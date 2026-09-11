package discover_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/state"
	"github.com/taihen/rosup/internal/transport"
)

type fakeClient struct {
	outputs map[string]string
	runs    []string
	closed  bool
}

func (f *fakeClient) Run(_ context.Context, command string) (string, error) {
	f.runs = append(f.runs, command)
	out, ok := f.outputs[command]
	if !ok {
		return "", fmt.Errorf("unexpected command %q", command)
	}
	return out, nil
}

func (f *fakeClient) Upload(context.Context, string, string) error {
	return errors.New("upload not allowed")
}

func (f *fakeClient) Download(context.Context, string, string) error {
	return errors.New("download not allowed")
}

func (f *fakeClient) Remove(context.Context, string) error {
	return errors.New("remove not allowed")
}

func (f *fakeClient) Close() error {
	f.closed = true
	return nil
}

func fixtureOutputs(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"/system resource print":    ros6Fixture(t, "resource-print.txt"),
		"/system package print":     ros6Fixture(t, "package-print.txt"),
		"/system routerboard print": ros6Fixture(t, "routerboard-print.txt"),
		"/system identity print":    ros6Fixture(t, "identity-print.txt"),
	}
}

func writeDiscoverEnv(t *testing.T, inventoryYAML string) (*config.Config, string) {
	t.Helper()
	root := t.TempDir()
	ops := filepath.Join(root, "ops")
	invPath := filepath.Join(ops, "inventory", "devices.yaml")
	if err := os.MkdirAll(filepath.Dir(invPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invPath, []byte(inventoryYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		StateDir: filepath.Join(root, "state"),
		Ops: config.OpsConfig{
			Path:          ops,
			InventoryFile: "inventory/devices.yaml",
		},
		SSH: config.SSHConfig{
			DefaultPort: 22,
		},
	}
	return cfg, invPath
}

func TestRunWritesFactsToStateNotInventory(t *testing.T) {
	cfg, invPath := writeDiscoverEnv(t, `
devices:
  - name: router-01
    address: 192.0.2.10
    role: ospf
    group: core-a
    order: 10
    validation_profile: ospf
    port: 60022
`)
	before, err := os.ReadFile(invPath)
	if err != nil {
		t.Fatal(err)
	}

	client := &fakeClient{outputs: fixtureOutputs(t)}
	var gotAddr string
	var gotPort int
	dial := func(_ context.Context, _ config.SSHConfig, address string, port int) (transport.Client, error) {
		gotAddr = address
		gotPort = port
		return client, nil
	}

	results, err := discover.Run(context.Background(), cfg, "", dial)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results %d", len(results))
	}
	facts := results[0].Facts
	if facts.ArchitectureName != "arm" || facts.Version != "6.49.18" {
		t.Fatalf("facts %+v", facts)
	}
	if gotAddr != "192.0.2.10" {
		t.Fatalf("address %q", gotAddr)
	}
	if gotPort != 60022 {
		t.Fatalf("port %d", gotPort)
	}
	if !client.closed {
		t.Fatal("client not closed")
	}
	wantCmds := []string{
		"/system resource print",
		"/system package print",
		"/system routerboard print",
		"/system identity print",
	}
	if strings.Join(client.runs, ",") != strings.Join(wantCmds, ",") {
		t.Fatalf("commands %v", client.runs)
	}

	job, err := state.Load(cfg.StateDir, "router-01")
	if err != nil {
		t.Fatal(err)
	}
	if job.Stage != "DISCOVER" {
		t.Fatalf("stage %q", job.Stage)
	}
	if job.Group != "core-a" {
		t.Fatalf("group %q", job.Group)
	}
	if job.Status == state.StatusComplete {
		t.Fatal("discover must not mark complete")
	}
	var stored discover.Facts
	if err := json.Unmarshal(job.Facts, &stored); err != nil {
		t.Fatalf("facts json: %v", err)
	}
	if stored.BoardName != "hAP ac^2" || stored.FreeHDDSpace != "4212.0KiB" {
		t.Fatalf("stored %+v", stored)
	}
	if stored.CurrentFirmware != "6.49.13" || stored.UpgradeFirmware != "6.49.18" {
		t.Fatalf("firmware %+v", stored)
	}

	after, err := os.ReadFile(invPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("inventory file changed")
	}
}

func TestRunFiltersByGroup(t *testing.T) {
	cfg, _ := writeDiscoverEnv(t, `
devices:
  - name: core-1
    address: 192.0.2.1
    role: ospf
    group: core-a
    order: 10
    validation_profile: ospf
  - name: edge-1
    address: 192.0.2.2
    role: access
    group: edge
    order: 20
    validation_profile: access
`)
	var dialed []string
	dial := func(_ context.Context, _ config.SSHConfig, address string, _ int) (transport.Client, error) {
		dialed = append(dialed, address)
		outs := fixtureOutputs(t)
		name := "edge-1"
		if address == "192.0.2.1" {
			name = "core-1"
		}
		outs["/system identity print"] = "  name: " + name + "\n"
		return &fakeClient{outputs: outs}, nil
	}

	results, err := discover.Run(context.Background(), cfg, "edge", dial)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Device.Name != "edge-1" {
		t.Fatalf("results %+v", results)
	}
	if len(dialed) != 1 || dialed[0] != "192.0.2.2" {
		t.Fatalf("dialed %v", dialed)
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, "core-1.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("core-1 state: %v", err)
	}
}

func TestRunAllDevicesWhenGroupUnset(t *testing.T) {
	cfg, _ := writeDiscoverEnv(t, `
devices:
  - name: core-1
    address: 192.0.2.1
    role: ospf
    group: core-a
    order: 10
    validation_profile: ospf
  - name: edge-1
    address: 192.0.2.2
    role: access
    group: edge
    order: 20
    validation_profile: access
`)
	var dialed []string
	dial := func(_ context.Context, _ config.SSHConfig, address string, _ int) (transport.Client, error) {
		dialed = append(dialed, address)
		outs := fixtureOutputs(t)
		name := "edge-1"
		if address == "192.0.2.1" {
			name = "core-1"
		}
		outs["/system identity print"] = "  name: " + name + "\n"
		return &fakeClient{outputs: outs}, nil
	}

	results, err := discover.Run(context.Background(), cfg, "", dial)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results %d", len(results))
	}
	if results[0].Device.Name != "core-1" || results[1].Device.Name != "edge-1" {
		t.Fatalf("order %q %q", results[0].Device.Name, results[1].Device.Name)
	}
	if len(dialed) != 2 {
		t.Fatalf("dialed %v", dialed)
	}
}

func TestRunUsesDefaultPortWhenPortZero(t *testing.T) {
	cfg, _ := writeDiscoverEnv(t, `
devices:
  - name: router-01
    address: 192.0.2.10
    role: switch
    group: access
    order: 1
    validation_profile: switch
`)
	var gotPort int
	dial := func(_ context.Context, _ config.SSHConfig, _ string, port int) (transport.Client, error) {
		gotPort = port
		return &fakeClient{outputs: fixtureOutputs(t)}, nil
	}

	if _, err := discover.Run(context.Background(), cfg, "", dial); err != nil {
		t.Fatal(err)
	}
	if gotPort != 22 {
		t.Fatalf("port %d", gotPort)
	}
}

func TestRunUnknownGroup(t *testing.T) {
	cfg, _ := writeDiscoverEnv(t, `
devices:
  - name: router-01
    address: 192.0.2.10
    role: ospf
    group: core-a
    order: 10
    validation_profile: ospf
`)
	dial := func(context.Context, config.SSHConfig, string, int) (transport.Client, error) {
		t.Fatal("dial should not run")
		return nil, nil
	}
	_, err := discover.Run(context.Background(), cfg, "missing", dial)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "group") {
		t.Fatalf("got %v", err)
	}
}

func TestRunPreservesCompleteStatus(t *testing.T) {
	cfg, _ := writeDiscoverEnv(t, `
devices:
  - name: router-01
    address: 192.0.2.10
    role: ospf
    group: core-a
    order: 10
    validation_profile: ospf
`)
	job := &state.DeviceJob{
		Device:  "router-01",
		Release: "6.49.18",
		Group:   "core-a",
		Stage:   "COMPLETE",
		Status:  state.StatusComplete,
	}
	if err := state.Save(cfg.StateDir, job); err != nil {
		t.Fatal(err)
	}

	dial := func(_ context.Context, _ config.SSHConfig, _ string, _ int) (transport.Client, error) {
		return &fakeClient{outputs: fixtureOutputs(t)}, nil
	}
	if _, err := discover.Run(context.Background(), cfg, "", dial); err != nil {
		t.Fatal(err)
	}

	got, err := state.Load(cfg.StateDir, "router-01")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusComplete {
		t.Fatalf("status %q", got.Status)
	}
	if got.Stage != "COMPLETE" {
		t.Fatalf("stage %q", got.Stage)
	}
	if got.Release != "6.49.18" {
		t.Fatalf("release %q", got.Release)
	}
	if len(got.Facts) == 0 {
		t.Fatal("facts missing")
	}
}

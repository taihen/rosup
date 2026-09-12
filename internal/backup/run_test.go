package backup_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/taihen/rosup/internal/backup"
	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/transport"
)

func TestExportAndBackupSetsTime(t *testing.T) {
	cfg := testConfig(t)
	before := time.Now().UTC().Add(-time.Second)
	got, err := backup.ExportAndBackup(context.Background(), cfg, "golem", &fakeClient{export: "#\n"})
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC().Add(time.Second)
	if got.Time.IsZero() {
		t.Fatal("Time not set")
	}
	if got.Time.Before(before) || got.Time.After(after) {
		t.Fatalf("Time %v outside [%v, %v]", got.Time, before, after)
	}
	stamp := got.Time.UTC().Format(backupTimeFmt)
	if !strings.Contains(filepath.Base(got.ExportPath), stamp) {
		t.Fatalf("ExportPath %q missing time %s", got.ExportPath, stamp)
	}
}

func TestRunContinuesOnDeviceFailureAndPushesSuccesses(t *testing.T) {
	cfg := writeFleetEnv(t, `
devices:
  - name: bad
    address: 192.0.2.1
    role: access
    group: edge
    order: 1
    validation_profile: access
  - name: good
    address: 192.0.2.2
    role: access
    group: edge
    order: 2
    validation_profile: access
`)

	dial := func(_ context.Context, _ config.SSHConfig, address string, _ int) (transport.Client, error) {
		if address == "192.0.2.1" {
			return nil, errors.New("dial refused")
		}
		return &fakeClient{export: "# good-export\n"}, nil
	}

	var pushed []backup.FleetItem
	push := func(_ context.Context, _ *config.Config, items []backup.FleetItem) error {
		pushed = append([]backup.FleetItem(nil), items...)
		return nil
	}

	outcomes, err := backup.Run(context.Background(), cfg, "", dial, push)
	if err == nil {
		t.Fatal("expected error for failed device")
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Fatalf("error %v", err)
	}
	if len(outcomes) != 3 {
		t.Fatalf("outcomes %d: %+v", len(outcomes), outcomes)
	}
	if outcomes[0].Device != "bad" || outcomes[0].Err == nil {
		t.Fatalf("outcome[0] %+v", outcomes[0])
	}
	if outcomes[1].Device != "good" || outcomes[1].Err != nil {
		t.Fatalf("outcome[1] %+v", outcomes[1])
	}
	if outcomes[2].Device != "git" || outcomes[2].Err != nil {
		t.Fatalf("outcome[2] %+v", outcomes[2])
	}
	if len(pushed) != 1 || pushed[0].Device != "good" {
		t.Fatalf("pushed %+v", pushed)
	}
	if pushed[0].Export != "# good-export\n" {
		t.Fatalf("export %q", pushed[0].Export)
	}
	if pushed[0].Time.IsZero() {
		t.Fatal("pushed time zero")
	}
}

func TestRunGroupFilter(t *testing.T) {
	cfg := writeFleetEnv(t, `
devices:
  - name: core-1
    address: 192.0.2.1
    role: ospf
    group: core
    order: 1
    validation_profile: ospf
  - name: edge-1
    address: 192.0.2.2
    role: access
    group: edge
    order: 1
    validation_profile: access
`)

	var dialed []string
	dial := func(_ context.Context, _ config.SSHConfig, address string, _ int) (transport.Client, error) {
		dialed = append(dialed, address)
		return &fakeClient{export: "#\n"}, nil
	}
	push := func(_ context.Context, _ *config.Config, items []backup.FleetItem) error {
		if len(items) != 1 || items[0].Device != "core-1" {
			t.Fatalf("items %+v", items)
		}
		return nil
	}

	outcomes, err := backup.Run(context.Background(), cfg, "core", dial, push)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes %+v", outcomes)
	}
	if outcomes[0].Device != "core-1" || outcomes[0].Err != nil {
		t.Fatalf("outcome[0] %+v", outcomes[0])
	}
	if outcomes[1].Device != "git" || outcomes[1].Err != nil {
		t.Fatalf("outcome[1] %+v", outcomes[1])
	}
	if len(dialed) != 1 || dialed[0] != "192.0.2.1" {
		t.Fatalf("dialed %v", dialed)
	}
}

func TestRunPushError(t *testing.T) {
	cfg := writeFleetEnv(t, `
devices:
  - name: golem
    address: 192.0.2.1
    role: access
    group: edge
    order: 1
    validation_profile: access
`)
	dial := func(_ context.Context, _ config.SSHConfig, _ string, _ int) (transport.Client, error) {
		return &fakeClient{export: "#\n"}, nil
	}
	push := func(_ context.Context, _ *config.Config, _ []backup.FleetItem) error {
		return errors.New("push failed")
	}

	outcomes, err := backup.Run(context.Background(), cfg, "", dial, push)
	if err == nil || !strings.Contains(err.Error(), "push failed") {
		t.Fatalf("got %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes %+v", outcomes)
	}
	if outcomes[0].Device != "golem" || outcomes[0].Err != nil {
		t.Fatalf("outcome[0] %+v", outcomes[0])
	}
	if outcomes[1].Device != "git" || outcomes[1].Err == nil {
		t.Fatalf("outcome[1] %+v", outcomes[1])
	}
}

func TestRunJoinsDeviceAndPushErrors(t *testing.T) {
	cfg := writeFleetEnv(t, `
devices:
  - name: bad
    address: 192.0.2.1
    role: access
    group: edge
    order: 1
    validation_profile: access
  - name: good
    address: 192.0.2.2
    role: access
    group: edge
    order: 2
    validation_profile: access
`)
	dial := func(_ context.Context, _ config.SSHConfig, address string, _ int) (transport.Client, error) {
		if address == "192.0.2.1" {
			return nil, errors.New("dial refused")
		}
		return &fakeClient{export: "#\n"}, nil
	}
	push := func(_ context.Context, _ *config.Config, _ []backup.FleetItem) error {
		return errors.New("push failed")
	}

	_, err := backup.Run(context.Background(), cfg, "", dial, push)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Fatalf("want device failure in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "push failed") {
		t.Fatalf("want push failure in error, got %v", err)
	}
}

func TestFormatOutcomes(t *testing.T) {
	var buf bytes.Buffer
	err := backup.FormatOutcomes(&buf, []backup.Outcome{
		{Device: "ok-dev"},
		{Device: "bad-dev", Err: errors.New("boom")},
		{Device: "git", Err: errors.New("push failed")},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "*  ok-dev  ok\n") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "x  bad-dev  boom\n") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "x  git  push failed\n") {
		t.Fatalf("got %q", got)
	}
}

func writeFleetEnv(t *testing.T, inventoryYAML string) *config.Config {
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
	return &config.Config{
		BackupDir:           filepath.Join(root, "backups"),
		BackupRetentionDays: 30,
		Ops: config.OpsConfig{
			Path:          ops,
			InventoryFile: "inventory/devices.yaml",
		},
		SSH: config.SSHConfig{
			DefaultPort: 22,
		},
	}
}

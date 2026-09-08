package inventory_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/inventory"
)

func writeInventory(t *testing.T, relPath, contents string) *config.Config {
	t.Helper()
	ops := t.TempDir()
	path := filepath.Join(ops, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return &config.Config{
		Ops: config.OpsConfig{
			Path:          ops,
			InventoryFile: relPath,
		},
	}
}

func TestLoadReadsDevice(t *testing.T) {
	cfg := writeInventory(t, "inventory/devices.yaml", `
devices:
  - name: router-01
    address: 192.0.2.1
    role: ospf
    group: core-a
    order: 10
    validation_profile: ospf
    port: 60022
    depends_on: []
`)
	devices, err := inventory.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("len %d", len(devices))
	}
	d := devices[0]
	if d.Name != "router-01" {
		t.Fatalf("name %q", d.Name)
	}
	if d.Address != "192.0.2.1" {
		t.Fatalf("address %q", d.Address)
	}
	if d.Role != "ospf" {
		t.Fatalf("role %q", d.Role)
	}
	if d.Group != "core-a" {
		t.Fatalf("group %q", d.Group)
	}
	if d.Order != 10 {
		t.Fatalf("order %d", d.Order)
	}
	if d.ValidationProfile != "ospf" {
		t.Fatalf("validation_profile %q", d.ValidationProfile)
	}
	if d.Port != 60022 {
		t.Fatalf("port %d", d.Port)
	}
	if len(d.DependsOn) != 0 {
		t.Fatalf("depends_on %v", d.DependsOn)
	}
}

func TestLoadSortsByGroupThenOrderThenName(t *testing.T) {
	cfg := writeInventory(t, "inventory/devices.yaml", `
devices:
  - name: zed
    address: 192.0.2.1
    role: access
    group: b
    order: 1
    validation_profile: access
  - name: cal
    address: 192.0.2.2
    role: switch
    group: a
    order: 10
    validation_profile: switch
  - name: amy
    address: 192.0.2.3
    role: radio
    group: a
    order: 20
    validation_profile: radio
  - name: bob
    address: 192.0.2.4
    role: pppoe
    group: a
    order: 10
    validation_profile: pppoe
`)
	devices, err := inventory.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(devices))
	for i, d := range devices {
		got[i] = d.Name
	}
	want := []string{"bob", "cal", "amy", "zed"}
	if len(got) != len(want) {
		t.Fatalf("names %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names %v want %v", got, want)
		}
	}
}

func TestLoadRejectsUnknownRole(t *testing.T) {
	cfg := writeInventory(t, "inventory/devices.yaml", `
devices:
  - name: router-01
    address: 192.0.2.1
    role: bgp
    group: core-a
    order: 10
    validation_profile: ospf
`)
	_, err := inventory.Load(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "role") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadRejectsUnknownValidationProfile(t *testing.T) {
	cfg := writeInventory(t, "inventory/devices.yaml", `
devices:
  - name: router-01
    address: 192.0.2.1
    role: ospf
    group: core-a
    order: 10
    validation_profile: custom
`)
	_, err := inventory.Load(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "validation_profile") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadRejectsEmptyName(t *testing.T) {
	cfg := writeInventory(t, "inventory/devices.yaml", `
devices:
  - name: ""
    address: 192.0.2.1
    role: ospf
    group: core-a
    order: 10
    validation_profile: ospf
`)
	_, err := inventory.Load(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadRejectsEmptyAddress(t *testing.T) {
	cfg := writeInventory(t, "inventory/devices.yaml", `
devices:
  - name: router-01
    address: ""
    role: ospf
    group: core-a
    order: 10
    validation_profile: ospf
`)
	_, err := inventory.Load(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "address") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadRejectsUnknownDependsOn(t *testing.T) {
	cfg := writeInventory(t, "inventory/devices.yaml", `
devices:
  - name: router-01
    address: 192.0.2.1
    role: ospf
    group: core-a
    order: 10
    validation_profile: ospf
    depends_on: [missing]
`)
	_, err := inventory.Load(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "depends_on") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadAcceptsDependsOnNamesInFile(t *testing.T) {
	cfg := writeInventory(t, "inventory/devices.yaml", `
devices:
  - name: leaf
    address: 192.0.2.2
    role: access
    group: edge
    order: 20
    validation_profile: access
    depends_on: [core]
  - name: core
    address: 192.0.2.1
    role: ospf
    group: core-a
    order: 10
    validation_profile: ospf
`)
	devices, err := inventory.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("len %d", len(devices))
	}
	if devices[0].Name != "core" || devices[1].Name != "leaf" {
		t.Fatalf("order %q %q", devices[0].Name, devices[1].Name)
	}
	if len(devices[1].DependsOn) != 1 || devices[1].DependsOn[0] != "core" {
		t.Fatalf("depends_on %v", devices[1].DependsOn)
	}
}

func TestLoadUsesConfiguredInventoryFile(t *testing.T) {
	cfg := writeInventory(t, "custom/hosts.yaml", `
devices:
  - name: router-01
    address: 192.0.2.1
    role: ospf
    group: core-a
    order: 10
    validation_profile: ospf
`)
	devices, err := inventory.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].Name != "router-01" {
		t.Fatalf("devices %+v", devices)
	}
}

func TestLoadRejectsInvalidYAML(t *testing.T) {
	cfg := writeInventory(t, "inventory/devices.yaml", "devices: [")
	_, err := inventory.Load(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadOmittedPortIsZero(t *testing.T) {
	cfg := writeInventory(t, "inventory/devices.yaml", `
devices:
  - name: router-01
    address: 192.0.2.1
    role: ospf
    group: core-a
    order: 10
    validation_profile: ospf
`)
	devices, err := inventory.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("len %d", len(devices))
	}
	if devices[0].Port != 0 {
		t.Fatalf("port %d", devices[0].Port)
	}
}

func TestLoadUsesDefaultInventoryFile(t *testing.T) {
	cfg := writeInventory(t, "inventory/devices.yaml", `
devices:
  - name: router-01
    address: 192.0.2.1
    role: ospf
    group: core-a
    order: 10
    validation_profile: ospf
`)
	cfg.Ops.InventoryFile = ""
	devices, err := inventory.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].Name != "router-01" {
		t.Fatalf("devices %+v", devices)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg := &config.Config{
		Ops: config.OpsConfig{
			Path:          t.TempDir(),
			InventoryFile: "inventory/devices.yaml",
		},
	}
	_, err := inventory.Load(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadDoesNotModifyFile(t *testing.T) {
	cfg := writeInventory(t, "inventory/devices.yaml", `
devices:
  - name: router-01
    address: 192.0.2.1
    role: ospf
    group: core-a
    order: 10
    validation_profile: ospf
`)
	path := filepath.Join(cfg.Ops.Path, cfg.Ops.InventoryFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inventory.Load(cfg); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("inventory file changed")
	}
}

func TestNoSaveFunction(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	src, err := os.ReadFile(filepath.Join(filepath.Dir(file), "inventory.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "func Save") {
		t.Fatal("inventory must not write; no Save function")
	}
}

func TestLoadAllowsAllRolesAndProfiles(t *testing.T) {
	cfg := writeInventory(t, "inventory/devices.yaml", `
devices:
  - name: ospf-1
    address: 192.0.2.1
    role: ospf
    group: g
    order: 1
    validation_profile: ospf
  - name: pppoe-1
    address: 192.0.2.2
    role: pppoe
    group: g
    order: 2
    validation_profile: pppoe
  - name: radio-1
    address: 198.51.100.1
    role: radio
    group: g
    order: 3
    validation_profile: radio
  - name: switch-1
    address: 198.51.100.2
    role: switch
    group: g
    order: 4
    validation_profile: switch
  - name: access-1
    address: 203.0.113.1
    role: access
    group: g
    order: 5
    validation_profile: access
`)
	devices, err := inventory.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 5 {
		t.Fatalf("len %d", len(devices))
	}
}

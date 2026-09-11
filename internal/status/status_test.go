package status_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/state"
	"github.com/taihen/rosup/internal/status"
)

func TestRunJoinsInventoryAndJobs(t *testing.T) {
	cfg := statusConfig(t, `
devices:
  - name: edge-1
    address: 192.0.2.10
    role: radio
    group: radio
    validation_profile: radio
    order: 10
  - name: core-1
    address: 192.0.2.1
    role: ospf
    group: core-a
    validation_profile: ospf
    order: 10
`)
	job := &state.DeviceJob{
		Device:    "edge-1",
		Release:   "6.49.21",
		Group:     "radio",
		Stage:     "VALIDATE_ROLE",
		Status:    state.StatusFailed,
		LastError: "ssh down",
		UpdatedAt: time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC),
	}
	if err := state.Save(cfg.StateDir, job); err != nil {
		t.Fatal(err)
	}

	report, err := status.Run(cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Devices) != 2 {
		t.Fatalf("devices %d", len(report.Devices))
	}
	if report.Devices[0].Name != "core-1" || report.Devices[0].Status != "" {
		t.Fatalf("core-1: %+v", report.Devices[0])
	}
	if report.Devices[1].Name != "edge-1" || report.Devices[1].Status != state.StatusFailed {
		t.Fatalf("edge-1: %+v", report.Devices[1])
	}
	if report.Devices[1].LastError != "ssh down" {
		t.Fatalf("last_error %q", report.Devices[1].LastError)
	}
}

func TestRunFiltersGroupAndRelease(t *testing.T) {
	cfg := statusConfig(t, `
devices:
  - name: edge-1
    address: 192.0.2.10
    role: radio
    group: radio
    validation_profile: radio
    order: 10
  - name: core-1
    address: 192.0.2.1
    role: ospf
    group: core-a
    validation_profile: ospf
    order: 10
`)
	for _, job := range []*state.DeviceJob{
		{Device: "edge-1", Release: "6.49.21", Group: "radio", Status: state.StatusFailed, Stage: "REBOOT"},
		{Device: "core-1", Release: "6.49.18", Group: "core-a", Status: state.StatusComplete, Stage: "COMPLETE"},
	} {
		if err := state.Save(cfg.StateDir, job); err != nil {
			t.Fatal(err)
		}
	}

	report, err := status.Run(cfg, "radio", "6.49.21")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Devices) != 1 || report.Devices[0].Name != "edge-1" {
		t.Fatalf("%+v", report.Devices)
	}

	report, err = status.Run(cfg, "", "6.49.21")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Devices) != 1 || report.Devices[0].Name != "edge-1" {
		t.Fatalf("release filter: %+v", report.Devices)
	}
}

func TestFormat(t *testing.T) {
	var buf bytes.Buffer
	err := status.Format(&buf, &status.Report{
		Devices: []status.Device{
			{Name: "core-1", Group: "core-a"},
			{
				Name:      "edge-1",
				Group:     "radio",
				Release:   "6.49.21",
				Stage:     "VALIDATE_ROLE",
				Status:    state.StatusFailed,
				LastError: "ssh down",
				UpdatedAt: time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{
		"devices: 2\n",
		"core-1\n  group: core-a\n  release: -\n  stage: -\n  status: none\n",
		"edge-1\n  group: radio\n  release: 6.49.21\n  stage: VALIDATE_ROLE\n  status: failed\n",
		"  updated_at: 2026-09-11T12:00:00Z\n",
		"  last_error: ssh down\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func statusConfig(t *testing.T, inv string) *config.Config {
	t.Helper()
	root := t.TempDir()
	ops := filepath.Join(root, "ops")
	path := filepath.Join(ops, "inventory", "devices.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(inv), 0o600); err != nil {
		t.Fatal(err)
	}
	return &config.Config{
		StateDir: filepath.Join(root, "state"),
		Ops: config.OpsConfig{
			Path:          ops,
			InventoryFile: "inventory/devices.yaml",
		},
	}
}

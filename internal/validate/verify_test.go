package validate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/validate"
)

func TestVerifyUsesLastBaselineOnDisk(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", ospfProfileYAML())
	client := newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt")).
		set(validate.CmdOSPFNeighbor, ros6Fixture(t, "ospf-neighbor-print.txt")).
		set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print.txt"))
	writeCapturedBaseline(t, cfg, client, "ospf")
	client.set(validate.CmdOSPFNeighbor, ros6Fixture(t, "ospf-neighbor-print-degraded.txt"))

	err := validate.Verify(context.Background(), validate.Request{
		Config:  cfg,
		Device:  testDeviceRole("ospf"),
		Dial:    dialClient(client),
		Clock:   newFakeClock(),
		Profile: "ospf",
	})
	if err == nil {
		t.Fatal("expected last baseline to fail on degraded adjacency")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "adjacency") {
		t.Fatalf("got %v", err)
	}
	for _, cmd := range client.runs {
		if cmd == "/system reboot" {
			t.Fatal("verify must not reboot")
		}
	}
}

func TestVerifySelfBaselineWhenMissing(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", ospfProfileYAML())
	client := newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt")).
		set(validate.CmdOSPFNeighbor, ros6Fixture(t, "ospf-neighbor-print.txt")).
		set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print.txt"))

	if err := validate.Verify(context.Background(), validate.Request{
		Config:  cfg,
		Device:  testDeviceRole("ospf"),
		Dial:    dialClient(client),
		Clock:   newFakeClock(),
		Profile: "ospf",
	}); err != nil {
		t.Fatal(err)
	}

	dir, err := validate.JobDir(cfg, "router-01", target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "baseline.json")); err != nil {
		t.Fatalf("expected self baseline: %v", err)
	}
	for _, cmd := range client.runs {
		if cmd == "/system reboot" {
			t.Fatal("verify must not reboot")
		}
	}
}

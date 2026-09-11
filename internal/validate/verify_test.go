package validate_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/progress"
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

func TestVerifySelfBaselineWhenSystemLogHasNoInstallLines(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", ospfProfileYAML())
	client := newFakeClient(t, "6.49.15", []string{"routeros-mipsbe", "wireless"}, ros6Fixture(t, "log-print-system-login-only.txt")).
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
}

func TestVerifyPlainProgressSavesBaselineThenChecks(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", strings.Replace(ospfProfileYAML(), "1ms", "5m", 1))
	client := newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt")).
		set(validate.CmdOSPFNeighbor, ros6Fixture(t, "ospf-neighbor-print.txt")).
		set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print.txt"))
	var buf bytes.Buffer

	if err := validate.Verify(context.Background(), validate.Request{
		Config:   cfg,
		Device:   testDeviceRole("ospf"),
		Dial:     dialClient(client),
		Clock:    newFakeClock(),
		Profile:  "ospf",
		Progress: progress.New(&buf, []string{"router-01"}),
	}); err != nil {
		t.Fatal(err)
	}
	want := "" +
		">  router-01  saving baseline\n" +
		"*  router-01  saving baseline\n" +
		">  router-01  waiting for ospf (5m)\n" +
		"*  router-01  waiting for ospf (5m)\n" +
		">  router-01  checking ospf\n" +
		"*  router-01  checking ospf\n"
	if buf.String() != want {
		t.Fatalf("got %q want %q", buf.String(), want)
	}
}

func TestVerifyPlainProgressOmitsBaselineWhenPresent(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", strings.Replace(ospfProfileYAML(), "1ms", "5m", 1))
	client := newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt")).
		set(validate.CmdOSPFNeighbor, ros6Fixture(t, "ospf-neighbor-print.txt")).
		set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print.txt"))
	writeCapturedBaseline(t, cfg, client, "ospf")
	var buf bytes.Buffer

	if err := validate.Verify(context.Background(), validate.Request{
		Config:   cfg,
		Device:   testDeviceRole("ospf"),
		Dial:     dialClient(client),
		Clock:    newFakeClock(),
		Profile:  "ospf",
		Progress: progress.New(&buf, []string{"router-01"}),
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "saving baseline") {
		t.Fatalf("should skip baseline: %q", buf.String())
	}
	want := "" +
		">  router-01  waiting for ospf (5m)\n" +
		"*  router-01  waiting for ospf (5m)\n" +
		">  router-01  checking ospf\n" +
		"*  router-01  checking ospf\n"
	if buf.String() != want {
		t.Fatalf("got %q want %q", buf.String(), want)
	}
}

package plan_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/inventory"
	"github.com/taihen/rosup/internal/plan"
	"github.com/taihen/rosup/internal/preflight"
	"github.com/taihen/rosup/internal/release"
	"github.com/taihen/rosup/internal/state"
)

func TestRunRequiresRelease(t *testing.T) {
	called := false
	_, err := plan.Run(context.Background(), &config.Config{PackageDir: t.TempDir()}, "", "core-a", func(context.Context, *config.Config, string) ([]discover.Result, error) {
		called = true
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--release") {
		t.Fatalf("got %v", err)
	}
	if called {
		t.Fatal("discover should not run")
	}
}

func TestRunMissingManifestDoesNotDiscover(t *testing.T) {
	cfg := testConfig(t)
	called := false
	_, err := plan.Run(context.Background(), cfg, "6.49.21", "", func(context.Context, *config.Config, string) ([]discover.Result, error) {
		called = true
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "manifest") && !strings.Contains(err.Error(), "6.49.21") {
		t.Fatalf("got %v", err)
	}
	if called {
		t.Fatal("discover should not run")
	}
}

func TestRunRejectsUnsafeRelease(t *testing.T) {
	cfg := testConfig(t)
	called := false
	_, err := plan.Run(context.Background(), cfg, "../etc", "", func(context.Context, *config.Config, string) ([]discover.Result, error) {
		called = true
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("got %v", err)
	}
	if called {
		t.Fatal("discover should not run")
	}
}

func TestRunPassesGroupToDiscover(t *testing.T) {
	cfg := testConfig(t)
	writeManifest(t, cfg, manifest(npk("routeros", "arm", 1000)))
	var gotGroup string
	_, err := plan.Run(context.Background(), cfg, "6.49.21", "core-a", func(_ context.Context, _ *config.Config, group string) ([]discover.Result, error) {
		gotGroup = group
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotGroup != "core-a" {
		t.Fatalf("group %q", gotGroup)
	}
}

func TestRunReportsReadyGroup(t *testing.T) {
	cfg := testConfig(t)
	writeManifest(t, cfg, manifest(
		npk("routeros", "arm", 1000),
		npk("wireless", "arm", 1000),
	))
	report, err := plan.Run(context.Background(), cfg, "6.49.21", "", fakeDiscover(
		result("router-01", "ospf", 10, armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Failure(report); err != nil {
		t.Fatal(err)
	}
	out := writeReport(t, report)
	if !strings.HasPrefix(out, "plan: OK      0 blocked / 1 ready / 1 total  release 6.49.21\n") {
		t.Fatalf("status line: %q", out)
	}
	if !strings.Contains(out, "summary\n  ready") {
		t.Fatalf("summary: %q", out)
	}
	if !strings.Contains(out, "router-01") || !strings.Contains(out, "READY") {
		t.Fatalf("got %q", out)
	}
	if !strings.Contains(out, "missing none") {
		t.Fatalf("got %q", out)
	}
}

func TestRunReportsReadyWhenDevicePrintsRouterOSArchName(t *testing.T) {
	cases := []struct {
		arch, device, role string
	}{
		{"arm", "boa", "access"},
		{"mipsbe", "SAUZA2", "radio"},
	}
	for _, tc := range cases {
		t.Run(tc.arch, func(t *testing.T) {
			cfg := testConfig(t)
			writeManifest(t, cfg, manifest(
				npk("routeros", tc.arch, 1000),
				npk("wireless", tc.arch, 1000),
			))
			report, err := plan.Run(context.Background(), cfg, "6.49.21", "", fakeDiscover(
				result(tc.device, tc.role, 0, archFacts(tc.arch, "6.49.15", "107.0MiB", "routeros-"+tc.arch, "wireless")),
			))
			if err != nil {
				t.Fatal(err)
			}
			if err := plan.Failure(report); err != nil {
				t.Fatal(err)
			}
			out := writeReport(t, report)
			if !strings.Contains(out, "missing none") {
				t.Fatalf("got %q", out)
			}
		})
	}
}

func TestRunMissingPackagesFailsPreflight(t *testing.T) {
	cfg := testConfig(t)
	writeManifest(t, cfg, manifest(
		npk("routeros", "arm", 1000),
	))
	report, err := plan.Run(context.Background(), cfg, "6.49.21", "edge", fakeDiscover(
		result("router-01", "ospf", 10, armFacts("6.49.18", "4212.0KiB", "routeros")),
		result("switch-01", "switch", 20, armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")),
	))
	if err != nil {
		t.Fatal(err)
	}
	fail := plan.Failure(report)
	if fail == nil {
		t.Fatal("expected preflight failure")
	}
	if fail.Error() != "plan: preflight failed (1 blocked / 1 ready)" {
		t.Fatalf("got %q", fail.Error())
	}
	out := writeReport(t, report)
	if !strings.HasPrefix(out, "plan: FAILED  1 blocked / 1 ready / 2 total  release 6.49.21\n") {
		t.Fatalf("status line: %q", out)
	}
	if !strings.Contains(out, "summary\n  missing packages") || !strings.Contains(out, "ready") {
		t.Fatalf("summary: %q", out)
	}
	if !strings.Contains(out, "switch-01") || !strings.Contains(out, "BLOCKED  missing packages") {
		t.Fatalf("got %q", out)
	}
	if !strings.Contains(out, "wireless") {
		t.Fatalf("got %q", out)
	}
	if !strings.Contains(out, "router-01") || !strings.Contains(out, "READY") {
		t.Fatalf("got %q", out)
	}
}

func TestRunDiskPreflightFailsWithNoMissing(t *testing.T) {
	cfg := testConfig(t)
	writeManifest(t, cfg, manifest(
		npk("routeros", "arm", 3<<20),
		npk("wireless", "arm", 2<<20),
	))
	report, err := plan.Run(context.Background(), cfg, "6.49.21", "", fakeDiscover(
		result("router-01", "ospf", 10, armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")),
	))
	if err != nil {
		t.Fatal(err)
	}
	fail := plan.Failure(report)
	if fail == nil {
		t.Fatal("expected preflight failure")
	}
	if fail.Error() != "plan: preflight failed (1 blocked / 0 ready)" {
		t.Fatalf("got %q", fail.Error())
	}
	out := writeReport(t, report)
	if !strings.HasPrefix(out, "plan: FAILED  1 blocked / 0 ready / 1 total  release 6.49.21\n") {
		t.Fatalf("status line: %q", out)
	}
	if !strings.Contains(out, "summary\n  disk") {
		t.Fatalf("summary: %q", out)
	}
	if !strings.Contains(out, "BLOCKED  disk") {
		t.Fatalf("got %q", out)
	}
	if !strings.Contains(out, "have 4.1 MiB") || !strings.Contains(out, "need 6 MiB") {
		t.Fatalf("human sizes missing: %q", out)
	}
	if strings.Contains(out, "bytes") {
		t.Fatalf("raw bytes in report: %q", out)
	}
}

func TestRunSkipsDiskPreflightWhenAlreadyOnRelease(t *testing.T) {
	cfg := testConfig(t)
	writeManifest(t, cfg, manifest(
		npk("routeros", "arm", 3<<20),
		npk("wireless", "arm", 2<<20),
		npk("routeros", "mipsbe", 3<<20),
		npk("wireless", "mipsbe", 2<<20),
	))
	report, err := plan.Run(context.Background(), cfg, "6.49.21", "", fakeDiscover(
		result("boa", "access", 0, armFacts("6.49.21", "1916.0KiB", "routeros-arm", "wireless")),
		result("SAUZA2", "radio", 0, archFacts("mipsbe", "6.49.15", "107.0MiB", "routeros-mipsbe", "wireless")),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Failure(report); err != nil {
		t.Fatal(err)
	}
	if report.Devices[0].Skip != "already 6.49.21" {
		t.Fatalf("boa Skip %q", report.Devices[0].Skip)
	}
	if report.Devices[1].Skip != "" {
		t.Fatalf("SAUZA2 Skip %q", report.Devices[1].Skip)
	}
	out := writeReport(t, report)
	if !strings.Contains(out, "boa") || !strings.Contains(out, "READY") {
		t.Fatalf("got %q", out)
	}
	if strings.Contains(out, "skip:") {
		t.Fatalf("skip must not appear in report: %q", out)
	}
}

func TestWriteReportAllReady(t *testing.T) {
	report := &plan.Report{
		Release: "6.49.19",
		Devices: []plan.Device{
			{Device: inventory.Device{Name: "a", Role: "core", Order: 10}},
			{Device: inventory.Device{Name: "b", Role: "edge", Order: 20}},
		},
	}
	out := writeReport(t, report)
	want := "" +
		"plan: OK      0 blocked / 2 ready / 2 total  release 6.49.19\n" +
		"summary\n" +
		"  ready ............. 2\n" +
		"hosts\n" +
		"  a  READY  order 10  role core  missing none\n" +
		"  b  READY  order 20  role edge  missing none\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
	if err := plan.Failure(report); err != nil {
		t.Fatal(err)
	}
}

func TestWriteReportMixed(t *testing.T) {
	report := &plan.Report{
		Release: "6.49.19",
		Devices: []plan.Device{
			{Device: inventory.Device{Name: "belmond", Role: "core", Order: 10}},
			{
				Device: inventory.Device{Name: "pilaf", Role: "access", Order: 20},
				Err:    &preflight.DiskError{Have: 2202009, Need: 26528972},
			},
			{
				Device: inventory.Device{Name: "marron", Role: "radio", Order: 30},
				Err:    &preflight.MissingPackagesError{Arch: "mipsbe", Packages: []string{"lte"}},
			},
		},
	}
	out := writeReport(t, report)
	want := "" +
		"plan: FAILED  2 blocked / 1 ready / 3 total  release 6.49.19\n" +
		"summary\n" +
		"  disk .............. 1\n" +
		"  missing packages .. 1\n" +
		"  ready ............. 1\n" +
		"hosts\n" +
		"  belmond  READY    order 10  role core    missing none\n" +
		"  pilaf    BLOCKED  disk              have 2.1 MiB  need 25.3 MiB\n" +
		"  marron   BLOCKED  missing packages  mipsbe: lte\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
	if strings.Contains(out, "bytes") {
		t.Fatalf("raw bytes leaked:\n%s", out)
	}
	fail := plan.Failure(report)
	if fail == nil {
		t.Fatal("expected failure")
	}
	if fail.Error() != "plan: preflight failed (2 blocked / 1 ready)" {
		t.Fatalf("got %q", fail.Error())
	}
	if strings.Contains(fail.Error(), "pilaf") || strings.Contains(fail.Error(), "hosts") {
		t.Fatalf("error must not embed table: %v", fail)
	}
}

func TestWriteReportUnsupportedAndGenericError(t *testing.T) {
	report := &plan.Report{
		Release: "6.49.19",
		Devices: []plan.Device{
			{Device: inventory.Device{Name: "x"}, Err: &preflight.UnsupportedError{Version: "7.11.2"}},
			{Device: inventory.Device{Name: "y"}, Err: errors.New("preflight: parse free-hdd-space \"nope\": invalid size")},
		},
	}
	out := writeReport(t, report)
	want := "" +
		"plan: FAILED  2 blocked / 0 ready / 2 total  release 6.49.19\n" +
		"summary\n" +
		"  unsupported ....... 1\n" +
		"  error ............. 1\n" +
		"  ready ............. 0\n" +
		"hosts\n" +
		"  x  BLOCKED  unsupported  RouterOS 7 (7.11.2)\n" +
		"  y  BLOCKED  error        parse free-hdd-space \"nope\": invalid size\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestRunEmptyGroupDiscoversAll(t *testing.T) {
	cfg := testConfig(t)
	writeManifest(t, cfg, manifest(npk("routeros", "arm", 1000)))
	var gotGroup string
	_, err := plan.Run(context.Background(), cfg, "6.49.21", "", func(_ context.Context, _ *config.Config, group string) ([]discover.Result, error) {
		gotGroup = group
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotGroup != "" {
		t.Fatalf("group %q", gotGroup)
	}
}

func TestRunDependsSatisfiedByCompleteJob(t *testing.T) {
	cfg := testConfig(t)
	writeReadyArmManifest(t, cfg)
	saveCompleteJob(t, cfg, "core-1", "6.49.21")
	report, err := plan.Run(context.Background(), cfg, "6.49.21", "edge", fakeDiscover(
		resultDeps("edge-1", "ospf", 10, []string{"core-1"}, armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Failure(report); err != nil {
		t.Fatal(err)
	}
	out := writeReport(t, report)
	if !strings.Contains(out, "edge-1") || !strings.Contains(out, "READY") {
		t.Fatalf("got %q", out)
	}
}

func TestRunDependsBlockedWhenDepMissingFromPlanAndState(t *testing.T) {
	cfg := testConfig(t)
	writeReadyArmManifest(t, cfg)
	report, err := plan.Run(context.Background(), cfg, "6.49.21", "edge", fakeDiscover(
		resultDeps("edge-1", "ospf", 10, []string{"core-1"}, armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")),
	))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Failure(report) == nil {
		t.Fatal("expected preflight failure")
	}
	out := writeReport(t, report)
	if !strings.Contains(out, "summary\n  depends") {
		t.Fatalf("summary: %q", out)
	}
	if !strings.Contains(out, "BLOCKED  depends") {
		t.Fatalf("got %q", out)
	}
	if !strings.Contains(out, "core-1:") || !strings.Contains(out, "no job for release 6.49.21") {
		t.Fatalf("got %q", out)
	}
}

func TestRunDependsSatisfiedWhenDepEarlierInSamePlan(t *testing.T) {
	cfg := testConfig(t)
	writeReadyArmManifest(t, cfg)
	report, err := plan.Run(context.Background(), cfg, "6.49.21", "", fakeDiscover(
		resultDeps("core-1", "ospf", 10, nil, armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")),
		resultDeps("edge-1", "ospf", 20, []string{"core-1"}, armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Failure(report); err != nil {
		t.Fatal(err)
	}
	out := writeReport(t, report)
	if !strings.Contains(out, "edge-1") || !strings.Contains(out, "READY") {
		t.Fatalf("got %q", out)
	}
	if strings.Contains(out, "depends") {
		t.Fatalf("unexpected depends block: %q", out)
	}
}

func TestRunDependsBlockedWhenDepLaterInSamePlan(t *testing.T) {
	cfg := testConfig(t)
	writeReadyArmManifest(t, cfg)
	report, err := plan.Run(context.Background(), cfg, "6.49.21", "", fakeDiscover(
		resultDeps("edge-1", "ospf", 10, []string{"core-1"}, armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")),
		resultDeps("core-1", "ospf", 20, nil, armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")),
	))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Failure(report) == nil {
		t.Fatal("expected preflight failure")
	}
	out := writeReport(t, report)
	if !strings.Contains(out, "edge-1") || !strings.Contains(out, "BLOCKED  depends") {
		t.Fatalf("got %q", out)
	}
	if !strings.Contains(out, "sorts after this host in plan") {
		t.Fatalf("got %q", out)
	}
}

func TestRunDependsSkippedWhenAlreadyOnRelease(t *testing.T) {
	cfg := testConfig(t)
	writeReadyArmManifest(t, cfg)
	report, err := plan.Run(context.Background(), cfg, "6.49.21", "edge", fakeDiscover(
		resultDeps("edge-1", "ospf", 10, []string{"core-1"}, armFacts("6.49.21", "1916.0KiB", "routeros", "wireless")),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Failure(report); err != nil {
		t.Fatal(err)
	}
	if report.Devices[0].Skip != "already 6.49.21" {
		t.Fatalf("Skip %q", report.Devices[0].Skip)
	}
	out := writeReport(t, report)
	if !strings.Contains(out, "READY") {
		t.Fatalf("got %q", out)
	}
	if strings.Contains(out, "depends") {
		t.Fatalf("unexpected depends: %q", out)
	}
}

func TestRunDependsDoesNotOverwriteDiskPreflight(t *testing.T) {
	cfg := testConfig(t)
	writeManifest(t, cfg, manifest(
		npk("routeros", "arm", 3<<20),
		npk("wireless", "arm", 2<<20),
	))
	report, err := plan.Run(context.Background(), cfg, "6.49.21", "edge", fakeDiscover(
		resultDeps("edge-1", "ospf", 10, []string{"core-1"}, armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")),
	))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Failure(report) == nil {
		t.Fatal("expected preflight failure")
	}
	out := writeReport(t, report)
	if !strings.Contains(out, "BLOCKED  disk") {
		t.Fatalf("disk must win: %q", out)
	}
	if strings.Contains(out, "depends") {
		t.Fatalf("depends must not overwrite disk: %q", out)
	}
	var disk *preflight.DiskError
	if !errors.As(report.Devices[0].Err, &disk) {
		t.Fatalf("want DiskError, got %T %v", report.Devices[0].Err, report.Devices[0].Err)
	}
}

func TestWriteReportDependsCause(t *testing.T) {
	report := &plan.Report{
		Release: "6.49.19",
		Devices: []plan.Device{
			{
				Device: inventory.Device{Name: "edge-1", Role: "ospf", Order: 10},
				Err:    &preflight.DependsError{Device: "edge-1", Dep: "core-1", Reason: "no job for release 6.49.19"},
			},
		},
	}
	out := writeReport(t, report)
	want := "" +
		"plan: FAILED  1 blocked / 0 ready / 1 total  release 6.49.19\n" +
		"summary\n" +
		"  depends ........... 1\n" +
		"  ready ............. 0\n" +
		"hosts\n" +
		"  edge-1  BLOCKED  depends  core-1: no job for release 6.49.19\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	return &config.Config{
		PackageDir: filepath.Join(root, "packages"),
		StateDir:   filepath.Join(root, "state"),
	}
}

func writeManifest(t *testing.T, cfg *config.Config, man release.Manifest) {
	t.Helper()
	dir := filepath.Join(cfg.PackageDir, man.Version)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeReadyArmManifest(t *testing.T, cfg *config.Config) {
	t.Helper()
	writeManifest(t, cfg, manifest(
		npk("routeros", "arm", 1000),
		npk("wireless", "arm", 1000),
	))
}

func fakeDiscover(results ...discover.Result) plan.DiscoverFunc {
	return func(context.Context, *config.Config, string) ([]discover.Result, error) {
		return results, nil
	}
}

func result(name, role string, order int, facts discover.Facts) discover.Result {
	return resultDeps(name, role, order, nil, facts)
}

func resultDeps(name, role string, order int, deps []string, facts discover.Facts) discover.Result {
	return discover.Result{
		Device: inventory.Device{Name: name, Role: role, Order: order, DependsOn: deps},
		Facts:  facts,
	}
}

func saveCompleteJob(t *testing.T, cfg *config.Config, device, version string) {
	t.Helper()
	if err := state.EnsureSecureDir(cfg.StateDir); err != nil {
		t.Fatal(err)
	}
	job := &state.DeviceJob{
		Device:  device,
		Release: version,
		Status:  state.StatusComplete,
		Stage:   "done",
	}
	if err := state.Save(cfg.StateDir, job); err != nil {
		t.Fatal(err)
	}
}

func armFacts(version, free string, pkgs ...string) discover.Facts {
	return archFacts("arm", version, free, pkgs...)
}

func archFacts(arch, version, free string, pkgs ...string) discover.Facts {
	packages := make([]discover.Package, len(pkgs))
	for i, name := range pkgs {
		packages[i] = discover.Package{Name: name, Version: version}
	}
	return discover.Facts{
		ArchitectureName: arch,
		BoardName:        "hAP ac^2",
		Version:          version,
		FreeHDDSpace:     free,
		Packages:         packages,
		CurrentFirmware:  "6.49.13",
		UpgradeFirmware:  "6.49.18",
	}
}

func npk(pkg, arch string, size int64) release.File {
	return release.File{
		Name:         pkg + "-6.49.21-" + arch + ".npk",
		Architecture: arch,
		Package:      pkg,
		SHA256:       "abc",
		Size:         size,
	}
}

func manifest(files ...release.File) release.Manifest {
	return release.Manifest{
		Version:       "6.49.21",
		Channel:       "long-term",
		Architectures: []string{"arm"},
		Files:         files,
	}
}

func writeReport(t *testing.T, report *plan.Report) string {
	t.Helper()
	var buf bytes.Buffer
	if err := plan.WriteReport(&buf, report); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

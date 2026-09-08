package plan_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/inventory"
	"github.com/taihen/rosup/internal/plan"
	"github.com/taihen/rosup/internal/release"
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
	out := format(t, report)
	want := "" +
		"release: 6.49.21\n" +
		"devices: 1\n" +
		"router-01\n" +
		"  order: 10\n" +
		"  role: ospf\n" +
		"  missing: none\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
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
	if !strings.Contains(fail.Error(), "switch-01") {
		t.Fatalf("got %v", fail)
	}
	if !strings.Contains(fail.Error(), "wireless") {
		t.Fatalf("got %v", fail)
	}
	out := format(t, report)
	want := "" +
		"release: 6.49.21\n" +
		"devices: 2\n" +
		"router-01\n" +
		"  order: 10\n" +
		"  role: ospf\n" +
		"  missing: none\n" +
		"switch-01\n" +
		"  order: 20\n" +
		"  role: switch\n" +
		"  missing: wireless\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
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
	if !strings.Contains(fail.Error(), "not enough free disk") {
		t.Fatalf("got %v", fail)
	}
	out := format(t, report)
	if !strings.Contains(out, "missing: none") {
		t.Fatalf("got %q", out)
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

func fakeDiscover(results ...discover.Result) plan.DiscoverFunc {
	return func(context.Context, *config.Config, string) ([]discover.Result, error) {
		return results, nil
	}
}

func result(name, role string, order int, facts discover.Facts) discover.Result {
	return discover.Result{
		Device: inventory.Device{Name: name, Role: role, Order: order},
		Facts:  facts,
	}
}

func armFacts(version, free string, pkgs ...string) discover.Facts {
	packages := make([]discover.Package, len(pkgs))
	for i, name := range pkgs {
		packages[i] = discover.Package{Name: name, Version: "6.49.18"}
	}
	return discover.Facts{
		ArchitectureName: "arm",
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

func format(t *testing.T, report *plan.Report) string {
	t.Helper()
	var buf bytes.Buffer
	if err := plan.Format(&buf, report); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

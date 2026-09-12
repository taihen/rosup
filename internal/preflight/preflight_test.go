package preflight_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/preflight"
	"github.com/taihen/rosup/internal/release"
)

func TestFormatSize(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{3 << 20, "3 MiB"},
		{2202009, "2.1 MiB"}, // 2.1 * 1024 * 1024 truncated
		{4212 * 1024, "4.1 MiB"},
		{1024, "1 KiB"},
		{512, "512 B"},
		{1024 * 1024 * 1024, "1 GiB"},
	}
	for _, tc := range cases {
		if got := preflight.FormatSize(tc.n); got != tc.want {
			t.Errorf("FormatSize(%d)=%q want %q", tc.n, got, tc.want)
		}
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
	archs := make([]string, 0)
	seen := map[string]struct{}{}
	for _, f := range files {
		if _, ok := seen[f.Architecture]; ok {
			continue
		}
		seen[f.Architecture] = struct{}{}
		archs = append(archs, f.Architecture)
	}
	return release.Manifest{
		Version:       "6.49.21",
		Channel:       "long-term",
		Architectures: archs,
		Files:         files,
	}
}

func TestCheckAcceptsRouterOSArchPackageName(t *testing.T) {
	facts := armFacts("6.49.18", "4212.0KiB", "routeros-arm", "wireless")
	man := manifest(
		npk("routeros", "arm", 1000),
		npk("wireless", "arm", 1000),
	)
	if err := preflight.Check(facts, man); err != nil {
		t.Fatal(err)
	}
}

func TestCheckMipsbeUsesRouterOSMipsbeName(t *testing.T) {
	facts := discover.Facts{
		ArchitectureName: "mipsbe",
		BoardName:        "SXT 5",
		Version:          "6.49.15",
		FreeHDDSpace:     "107.3MiB",
		Packages: []discover.Package{
			{Name: "routeros-mipsbe", Version: "6.49.15"},
			{Name: "system", Version: "6.49.15"},
		},
		CurrentFirmware: "6.49.15",
		UpgradeFirmware: "6.49.15",
	}
	man := manifest(
		npk("routeros", "mipsbe", 1000),
		npk("system", "mipsbe", 1000),
	)
	if err := preflight.Check(facts, man); err != nil {
		t.Fatal(err)
	}
}

func TestCheckMissingWireless(t *testing.T) {
	facts := armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")
	man := manifest(
		npk("routeros", "arm", 1000),
	)
	err := preflight.Check(facts, man)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "wireless") {
		t.Fatalf("got %v", err)
	}
}

func TestCheckExtraUnusedNPKOK(t *testing.T) {
	facts := armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")
	man := manifest(
		npk("routeros", "arm", 1000),
		npk("wireless", "arm", 1000),
		npk("dhcp", "arm", 100<<20),
	)
	if err := preflight.Check(facts, man); err != nil {
		t.Fatal(err)
	}
}

func TestCheckArmCannotUseMMIPS(t *testing.T) {
	facts := armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")
	man := manifest(
		npk("routeros", "arm", 1000),
		npk("wireless", "mmips", 1000),
	)
	err := preflight.Check(facts, man)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "wireless") {
		t.Fatalf("got %v", err)
	}
}

func TestCheckInsufficientFreeDisk(t *testing.T) {
	facts := armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")
	man := manifest(
		npk("routeros", "arm", 3<<20),
		npk("wireless", "arm", 2<<20),
	)
	err := preflight.Check(facts, man)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not enough free disk") {
		t.Fatalf("got %v", err)
	}
	// 4212.0KiB = 4212 * 1024 bytes
	if !strings.Contains(err.Error(), "4313088") {
		t.Fatalf("did not parse 4212.0KiB: %v", err)
	}
}

func TestCheckInsufficientFreeDiskTyped(t *testing.T) {
	facts := armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")
	man := manifest(
		npk("routeros", "arm", 3<<20),
		npk("wireless", "arm", 2<<20),
	)
	err := preflight.Check(facts, man)
	var disk *preflight.DiskError
	if !errors.As(err, &disk) {
		t.Fatalf("want DiskError, got %T %v", err, err)
	}
	if disk.Have != 4212*1024 {
		t.Fatalf("Have %d", disk.Have)
	}
	if disk.Need != (5<<20)+(1<<20) {
		t.Fatalf("Need %d", disk.Need)
	}
	msg := err.Error()
	if !strings.Contains(msg, "not enough free disk") {
		t.Fatalf("Error(): %v", err)
	}
	if !strings.Contains(msg, "4313088") || !strings.Contains(msg, "bytes") {
		t.Fatalf("Error() should keep byte counts for upgrade one-liners: %v", err)
	}
}

func TestCheckMissingPackagesTyped(t *testing.T) {
	facts := armFacts("6.49.18", "4212.0KiB", "routeros", "wireless")
	man := manifest(npk("routeros", "arm", 1000))
	err := preflight.Check(facts, man)
	var miss *preflight.MissingPackagesError
	if !errors.As(err, &miss) {
		t.Fatalf("want MissingPackagesError, got %T %v", err, err)
	}
	if miss.Arch != "arm" || strings.Join(miss.Packages, ",") != "wireless" {
		t.Fatalf("%+v", miss)
	}
}

func TestCheckRejectsROS7Typed(t *testing.T) {
	facts := armFacts("7.11.2", "4212.0KiB", "routeros", "wireless")
	man := manifest(npk("routeros", "arm", 1000), npk("wireless", "arm", 1000))
	err := preflight.Check(facts, man)
	var un *preflight.UnsupportedError
	if !errors.As(err, &un) {
		t.Fatalf("want UnsupportedError, got %T %v", err, err)
	}
	if un.Version != "7.11.2" {
		t.Fatalf("Version %q", un.Version)
	}
}

func TestCheckSkipsDiskWhenAlreadyOnRelease(t *testing.T) {
	facts := armFacts("6.49.21", "100B", "routeros", "wireless")
	for i := range facts.Packages {
		facts.Packages[i].Version = "6.49.21"
	}
	man := manifest(
		npk("routeros", "arm", 3<<20),
		npk("wireless", "arm", 2<<20),
	)
	if err := preflight.Check(facts, man); err != nil {
		t.Fatal(err)
	}
}

func TestCheckDiskWhenSystemMatchesButPackagesStale(t *testing.T) {
	facts := armFacts("6.49.21", "100B", "routeros", "wireless")
	man := manifest(
		npk("routeros", "arm", 3<<20),
		npk("wireless", "arm", 2<<20),
	)
	err := preflight.Check(facts, man)
	var disk *preflight.DiskError
	if !errors.As(err, &disk) {
		t.Fatalf("want DiskError, got %T %v", err, err)
	}
	if disk.Have != 100 {
		t.Fatalf("Have %d", disk.Have)
	}
	if disk.Need != (5<<20)+(1<<20) {
		t.Fatalf("Need %d", disk.Need)
	}
}

func TestCheckRejectsROS7(t *testing.T) {
	facts := armFacts("7.11.2", "4212.0KiB", "routeros", "wireless")
	man := manifest(
		npk("routeros", "arm", 1000),
		npk("wireless", "arm", 1000),
	)
	err := preflight.Check(facts, man)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "7.") {
		t.Fatalf("got %v", err)
	}
}

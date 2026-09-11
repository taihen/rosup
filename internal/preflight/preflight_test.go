package preflight_test

import (
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/preflight"
	"github.com/taihen/rosup/internal/release"
)

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

func TestCheckSkipsDiskWhenAlreadyOnRelease(t *testing.T) {
	facts := armFacts("6.49.21", "100B", "routeros", "wireless")
	man := manifest(
		npk("routeros", "arm", 3<<20),
		npk("wireless", "arm", 2<<20),
	)
	if err := preflight.Check(facts, man); err != nil {
		t.Fatal(err)
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

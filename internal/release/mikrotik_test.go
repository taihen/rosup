package release_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/release"
)

func mikrotikFixture(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join("..", "..", "testdata", "mikrotik", name)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseNewestLongTerm(t *testing.T) {
	got, err := release.ParseNewest(mikrotikFixture(t, "NEWEST6.long-term"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "6.49.21" {
		t.Fatalf("version %q", got)
	}
}

func TestParseListingNPKNames(t *testing.T) {
	names := release.ParseListing(mikrotikFixture(t, "listing-6.49.21.html"))
	want := []string{
		"routeros-arm-6.49.21.npk",
		"routeros-arm64-6.49.21.npk",
		"routeros-mmips-6.49.21.npk",
		"routeros-x86-6.49.21.npk",
		"wireless-6.49.21.npk",
		"wireless-6.49.21-arm.npk",
		"wireless-6.49.21-mmips.npk",
		"dhcp-6.49.21-arm.npk",
		"advanced-tools-6.49.21-arm.npk",
		"tr069-client-6.49.21-arm.npk",
		"iot-6.49.21-arm.npk",
		"lora-6.49.21-arm.npk",
		"calea-6.49.21-arm.npk",
		"dude-6.49.21-arm.npk",
		"lcd-6.49.21.npk",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("got %#v want %#v", names, want)
	}
	for _, name := range names {
		if !strings.HasSuffix(name, ".npk") {
			t.Fatalf("non-npk %q", name)
		}
	}
}

func TestFilterArchArmExcludesMmipsAndX86(t *testing.T) {
	names := release.ParseListing(mikrotikFixture(t, "listing-6.49.21.html"))
	got := release.FilterByArch(names, "arm", "6.49.21")
	want := []string{
		"routeros-arm-6.49.21.npk",
		"wireless-6.49.21-arm.npk",
		"dhcp-6.49.21-arm.npk",
		"advanced-tools-6.49.21-arm.npk",
		"tr069-client-6.49.21-arm.npk",
		"iot-6.49.21-arm.npk",
		"lora-6.49.21-arm.npk",
		"calea-6.49.21-arm.npk",
		"dude-6.49.21-arm.npk",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	for _, name := range got {
		if strings.Contains(name, "-mmips-") || strings.HasSuffix(name, "-mmips.npk") {
			t.Fatalf("arm filter kept mmips file %q", name)
		}
		if strings.Contains(name, "-arm64-") || strings.HasSuffix(name, "-arm64.npk") {
			t.Fatalf("arm filter kept arm64 file %q", name)
		}
	}
}

func TestFilterArchX86HasNoArchInfix(t *testing.T) {
	names := release.ParseListing(mikrotikFixture(t, "listing-6.49.21.html"))
	got := release.FilterByArch(names, "x86", "6.49.21")
	want := []string{
		"routeros-x86-6.49.21.npk",
		"wireless-6.49.21.npk",
		"lcd-6.49.21.npk",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestNewestURLIsLongTermOnly(t *testing.T) {
	u := release.NewestURL()
	if u != "https://upgrade.mikrotik.com/routeros/NEWEST6.long-term" {
		t.Fatalf("newest URL %q", u)
	}
	if strings.Contains(u, "stable") {
		t.Fatal("must not call NEWEST6.stable")
	}
}

func TestPackageURLs(t *testing.T) {
	arm := release.PackageURL("6.49.21", "arm", "routeros")
	if arm != "https://download.mikrotik.com/routeros/6.49.21/routeros-arm-6.49.21.npk" {
		t.Fatalf("arm url %q", arm)
	}
	x86 := release.PackageURL("6.49.21", "x86", "routeros")
	if x86 != "https://download.mikrotik.com/routeros/6.49.21/routeros-x86-6.49.21.npk" {
		t.Fatalf("x86 url %q", x86)
	}
	x86extra := release.PackageURL("6.49.21", "x86", "wireless")
	if x86extra != "https://download.mikrotik.com/routeros/6.49.21/wireless-6.49.21.npk" {
		t.Fatalf("x86 extra url %q", x86extra)
	}
	extra := release.PackageURL("6.49.21", "arm", "wireless")
	if extra != "https://download.mikrotik.com/routeros/6.49.21/wireless-6.49.21-arm.npk" {
		t.Fatalf("extra url %q", extra)
	}
	all := release.AllPackagesURL("6.49.21", "mipsbe")
	if all != "https://download.mikrotik.com/routeros/6.49.21/all_packages-mipsbe-6.49.21.zip" {
		t.Fatalf("all packages url %q", all)
	}
}

func TestParseNPKNameLivePatterns(t *testing.T) {
	cases := []struct {
		name, pkg, arch string
	}{
		{"routeros-arm-6.49.21.npk", "routeros", "arm"},
		{"routeros-x86-6.49.21.npk", "routeros", "x86"},
		{"routeros-6.49.21.npk", "routeros", "x86"},
		{"wireless-6.49.21-arm.npk", "wireless", "arm"},
		{"wireless-6.49.21-arm64.npk", "wireless", "arm64"},
		{"wireless-arm-6.49.21.npk", "wireless", "arm"},
		{"wireless-6.49.21.npk", "wireless", "x86"},
		{"tr069-client-6.49.21-arm.npk", "tr069-client", "arm"},
		{"iot-6.49.21-arm.npk", "iot", "arm"},
	}
	for _, tc := range cases {
		pkg, arch, ok := release.ParseNPKName(tc.name, "6.49.21")
		if !ok {
			t.Errorf("%s: parse failed", tc.name)
			continue
		}
		if pkg != tc.pkg || arch != tc.arch {
			t.Errorf("%s: got pkg=%q arch=%q want pkg=%q arch=%q", tc.name, pkg, arch, tc.pkg, tc.arch)
		}
	}
}

func TestIndexByInstalledNameAliasesRouterOSArch(t *testing.T) {
	files := []release.File{
		{Name: "routeros-arm-6.49.21.npk", Architecture: "arm", Package: "routeros"},
		{Name: "system-6.49.21-arm.npk", Architecture: "arm", Package: "system"},
		{Name: "routeros-mipsbe-6.49.21.npk", Architecture: "mipsbe", Package: "routeros"},
	}
	arm := release.IndexByInstalledName(files, "arm")
	if arm["routeros"].Name != "routeros-arm-6.49.21.npk" {
		t.Fatalf("routeros %+v", arm["routeros"])
	}
	if arm["routeros-arm"].Name != "routeros-arm-6.49.21.npk" {
		t.Fatalf("routeros-arm %+v", arm["routeros-arm"])
	}
	if _, ok := arm["routeros-mipsbe"]; ok {
		t.Fatal("arm index must not include mipsbe alias")
	}
	mipsbe := release.IndexByInstalledName(files, "mipsbe")
	if mipsbe["routeros-mipsbe"].Name != "routeros-mipsbe-6.49.21.npk" {
		t.Fatalf("routeros-mipsbe %+v", mipsbe["routeros-mipsbe"])
	}
}

func TestParseNewestRejectsPathLikeVersion(t *testing.T) {
	_, err := release.ParseNewest([]byte("6./../tmp extra\n"))
	if err == nil {
		t.Fatal("expected error for path-like version")
	}
}

func TestExtraPackagesIncludesLiveExtras(t *testing.T) {
	for _, pkg := range []string{"iot", "lora", "calea", "dude", "wireless", "routeros", "lcd"} {
		if !slices.Contains(release.ExtraPackages, pkg) {
			t.Errorf("ExtraPackages missing %s", pkg)
		}
	}
}

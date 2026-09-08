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
		"routeros-6.49.21.npk",
		"routeros-arm-6.49.21.npk",
		"routeros-arm64-6.49.21.npk",
		"routeros-mmips-6.49.21.npk",
		"wireless-6.49.21.npk",
		"wireless-arm-6.49.21.npk",
		"wireless-mmips-6.49.21.npk",
		"dhcp-arm-6.49.21.npk",
		"advanced-tools-arm-6.49.21.npk",
		"tr069-client-arm-6.49.21.npk",
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
		"wireless-arm-6.49.21.npk",
		"dhcp-arm-6.49.21.npk",
		"advanced-tools-arm-6.49.21.npk",
		"tr069-client-arm-6.49.21.npk",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	for _, name := range got {
		if strings.Contains(name, "-mmips-") {
			t.Fatalf("arm filter kept mmips file %q", name)
		}
		if strings.Contains(name, "-arm64-") {
			t.Fatalf("arm filter kept arm64 file %q", name)
		}
	}
}

func TestFilterArchX86HasNoArchInfix(t *testing.T) {
	names := release.ParseListing(mikrotikFixture(t, "listing-6.49.21.html"))
	got := release.FilterByArch(names, "x86", "6.49.21")
	want := []string{
		"routeros-6.49.21.npk",
		"wireless-6.49.21.npk",
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
	if x86 != "https://download.mikrotik.com/routeros/6.49.21/routeros-6.49.21.npk" {
		t.Fatalf("x86 url %q", x86)
	}
	x86extra := release.PackageURL("6.49.21", "x86", "wireless")
	if x86extra != "https://download.mikrotik.com/routeros/6.49.21/wireless-6.49.21.npk" {
		t.Fatalf("x86 extra url %q", x86extra)
	}
	extra := release.PackageURL("6.49.21", "arm", "wireless")
	if extra != "https://download.mikrotik.com/routeros/6.49.21/wireless-arm-6.49.21.npk" {
		t.Fatalf("extra url %q", extra)
	}
}

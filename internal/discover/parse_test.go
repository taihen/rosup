package discover_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/inventory"
)

func ros6Fixture(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "testdata", "ros6", name)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestParseRejectsMissingIdentity(t *testing.T) {
	_, err := discover.Parse(
		ros6Fixture(t, "resource-print.txt"),
		ros6Fixture(t, "package-print.txt"),
		ros6Fixture(t, "routerboard-print.txt"),
		"  name: \n",
	)
	if err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("got %v", err)
	}
}

func TestMatchInventory(t *testing.T) {
	facts := discover.Facts{Identity: "edge-1"}
	if err := discover.MatchInventory(facts, "edge-1"); err != nil {
		t.Fatal(err)
	}
	if err := discover.MatchInventory(facts, "core-1"); err == nil {
		t.Fatal("expected mismatch")
	}
}

func TestParseROS6Fixture(t *testing.T) {
	facts, err := discover.Parse(
		ros6Fixture(t, "resource-print.txt"),
		ros6Fixture(t, "package-print.txt"),
		ros6Fixture(t, "routerboard-print.txt"),
		ros6Fixture(t, "identity-print.txt"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if facts.ArchitectureName != "arm" {
		t.Fatalf("architecture-name %q", facts.ArchitectureName)
	}
	if facts.BoardName != "hAP ac^2" {
		t.Fatalf("board-name %q", facts.BoardName)
	}
	if facts.Version != "6.49.18" {
		t.Fatalf("version %q", facts.Version)
	}
	if facts.FreeHDDSpace != "4212.0KiB" {
		t.Fatalf("free-hdd-space %q", facts.FreeHDDSpace)
	}
	if facts.CurrentFirmware != "6.49.13" {
		t.Fatalf("current-firmware %q", facts.CurrentFirmware)
	}
	if facts.UpgradeFirmware != "6.49.18" {
		t.Fatalf("upgrade-firmware %q", facts.UpgradeFirmware)
	}
	if facts.Identity != "router-01" {
		t.Fatalf("identity %q", facts.Identity)
	}

	wantPkgs := []discover.Package{
		{Name: "routeros", Version: "6.49.18"},
		{Name: "wireless", Version: "6.49.18"},
		{Name: "dhcp", Version: "6.49.18"},
		{Name: "security", Version: "6.49.18"},
		{Name: "routing", Version: "6.49.18"},
		{Name: "ppp", Version: "6.49.18"},
		{Name: "ipv6", Version: "6.49.18"},
		{Name: "hotspot", Version: "6.49.18"},
	}
	if len(facts.Packages) != len(wantPkgs) {
		t.Fatalf("packages %#v", facts.Packages)
	}
	for i, want := range wantPkgs {
		got := facts.Packages[i]
		if got.Name != want.Name || got.Version != want.Version {
			t.Fatalf("package[%d] %+v want %+v", i, got, want)
		}
	}
}

func TestParseAcceptsNonRouterBoard(t *testing.T) {
	facts, err := discover.Parse(
		ros6Fixture(t, "resource-print.txt"),
		ros6Fixture(t, "package-print.txt"),
		"routerboard: no\n",
		ros6Fixture(t, "identity-print.txt"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if facts.RouterBoard {
		t.Fatal("expected non-RouterBOARD facts")
	}
	if facts.CurrentFirmware != "" || facts.UpgradeFirmware != "" {
		t.Fatalf("firmware %q / %q", facts.CurrentFirmware, facts.UpgradeFirmware)
	}
}

func TestParseRejectsMissingFirmwareOnRouterBoard(t *testing.T) {
	_, err := discover.Parse(
		ros6Fixture(t, "resource-print.txt"),
		ros6Fixture(t, "package-print.txt"),
		"routerboard: yes\nupgrade-firmware: 6.49.21\n",
		ros6Fixture(t, "identity-print.txt"),
	)
	if err == nil || !strings.Contains(err.Error(), "current-firmware") {
		t.Fatalf("got %v", err)
	}
}

func TestParseKeepsRouterOSArchPackageName(t *testing.T) {
	packages := "" +
		"Flags: X - disabled \n" +
		" #   NAME                    VERSION                    SCHEDULED              \n" +
		" 0   routeros-arm            6.49.21                                           \n" +
		" 1   system                  6.49.21                                           \n"
	facts, err := discover.Parse(
		ros6Fixture(t, "resource-print.txt"),
		packages,
		ros6Fixture(t, "routerboard-print.txt"),
		ros6Fixture(t, "identity-print.txt"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.Packages) != 2 {
		t.Fatalf("packages %#v", facts.Packages)
	}
	if facts.Packages[0].Name != "routeros-arm" || facts.Packages[0].Version != "6.49.21" {
		t.Fatalf("package[0] %+v", facts.Packages[0])
	}
	if facts.Packages[1].Name != "system" {
		t.Fatalf("package[1] %+v", facts.Packages[1])
	}
}

func TestParseRejectsROS7Version(t *testing.T) {
	resource := strings.ReplaceAll(
		ros6Fixture(t, "resource-print.txt"),
		"6.49.18 (long-term)",
		"7.11.2 (stable)",
	)
	_, err := discover.Parse(
		resource,
		ros6Fixture(t, "package-print.txt"),
		ros6Fixture(t, "routerboard-print.txt"),
		ros6Fixture(t, "identity-print.txt"),
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "7.") {
		t.Fatalf("got %v", err)
	}
}

func TestFormatPrintsParsedFields(t *testing.T) {
	facts, err := discover.Parse(
		ros6Fixture(t, "resource-print.txt"),
		ros6Fixture(t, "package-print.txt"),
		ros6Fixture(t, "routerboard-print.txt"),
		ros6Fixture(t, "identity-print.txt"),
	)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := discover.Format(&buf, []discover.Result{{
		Device: inventory.Device{Name: "router-01"},
		Facts:  facts,
	}}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"router-01",
		"architecture-name: arm",
		"board-name: hAP ac^2",
		"routerboard: yes",
		"version: 6.49.18",
		"free-hdd-space: 4212.0KiB",
		"current-firmware: 6.49.13",
		"upgrade-firmware: 6.49.18",
		"identity: router-01",
		"routeros 6.49.18",
		"wireless 6.49.18",
		"hotspot 6.49.18",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

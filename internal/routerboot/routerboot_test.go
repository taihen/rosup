package routerboot_test

import (
	"testing"

	"github.com/taihen/rosup/internal/routerboot"
)

func TestNewer(t *testing.T) {
	tests := []struct {
		upgrade string
		current string
		want    bool
	}{
		{upgrade: "6.49.18", current: "6.49.18", want: false},
		{upgrade: "6.49.18 (long-term)", current: "6.49.18", want: false},
		{upgrade: "6.49.21", current: "6.49.13", want: true},
		{upgrade: "6.49.18", current: "6.49.13", want: true},
		{upgrade: "6.49.13", current: "6.49.18", want: false},
		{upgrade: "6.49.18", current: "6.48.6", want: true},
		{upgrade: "7.1", current: "6.49.18", want: true},
		{upgrade: "", current: "6.49.18", want: false},
		{upgrade: "6.49.21", current: "", want: false},
		{upgrade: "not-a-version", current: "also-bad", want: false},
	}
	for _, tc := range tests {
		got := routerboot.Newer(tc.upgrade, tc.current)
		if got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.upgrade, tc.current, got, tc.want)
		}
	}
}

func TestParseFirmware(t *testing.T) {
	current, upgrade := routerboot.ParseFirmware(`        board-name: hAP ac^2
  current-firmware: 6.49.13
  upgrade-firmware: 6.49.21
`)
	if current != "6.49.13" {
		t.Fatalf("current %q", current)
	}
	if upgrade != "6.49.21" {
		t.Fatalf("upgrade %q", upgrade)
	}
}

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
		wantErr bool
	}{
		{upgrade: "6.49.18", current: "6.49.18", want: false},
		{upgrade: "6.49.18 (long-term)", current: "6.49.18", want: false},
		{upgrade: "6.49.21", current: "6.49.13", want: true},
		{upgrade: "6.49.18", current: "6.49.13", want: true},
		{upgrade: "6.49.13", current: "6.49.18", want: false},
		{upgrade: "6.49.18", current: "6.48.6", want: true},
		{upgrade: "7.1", current: "6.49.18", want: true},
		{upgrade: "", current: "6.49.18", wantErr: true},
		{upgrade: "6.49.21", current: "", wantErr: true},
		{upgrade: "not-a-version", current: "also-bad", wantErr: true},
	}
	for _, tc := range tests {
		got, err := routerboot.Compare(tc.upgrade, tc.current)
		if tc.wantErr {
			if err == nil {
				t.Errorf("Compare(%q, %q) want error", tc.upgrade, tc.current)
			}
			continue
		}
		if err != nil {
			t.Errorf("Compare(%q, %q) unexpected err %v", tc.upgrade, tc.current, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Compare(%q, %q) = %v, want %v", tc.upgrade, tc.current, got, tc.want)
		}
		if routerboot.Newer(tc.upgrade, tc.current) != tc.want {
			t.Errorf("Newer(%q, %q) mismatch", tc.upgrade, tc.current)
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

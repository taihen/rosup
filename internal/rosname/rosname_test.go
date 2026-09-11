package rosname

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestValid(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"edge-1", true},
		{"core_01", true},
		{"r.1", true},
		{"", false},
		{"x password=secret", false},
		{"a=b", false},
		{"has space", false},
		{"../escape", false},
	}
	for _, tc := range tests {
		if got := Valid(tc.in); got != tc.want {
			t.Errorf("Valid(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestIsExpectedDisconnect(t *testing.T) {
	if !IsExpectedDisconnect(nil) {
		t.Fatal("nil should be ok")
	}
	if !IsExpectedDisconnect(errors.New("connection reset by peer")) {
		t.Fatal("reset")
	}
	if !IsExpectedDisconnect(errors.New("read tcp: EOF")) {
		t.Fatal("eof")
	}
	if !IsExpectedDisconnect(&ssh.ExitMissingError{}) {
		t.Fatal("ExitMissingError")
	}
	err := errors.New("permission denied")
	if IsExpectedDisconnect(err) {
		t.Fatalf("permission denied should fail: %v", err)
	}
	if !strings.Contains(Check("device name", "bad name").Error(), "must match") {
		t.Fatal("Check message")
	}
}

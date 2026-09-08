package redact_test

import (
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/redact"
)

func TestRedactSecrets(t *testing.T) {
	in := "password=hunter2\ncommunity=public\nAuthorization: Bearer abcdef\n"
	out := redact.String(in)
	if strings.Contains(out, "hunter2") || strings.Contains(out, "abcdef") {
		t.Fatalf("leaked: %s", out)
	}
}

func TestRedactRouterOSKeys(t *testing.T) {
	in := strings.Join([]string{
		"password=hunter2",
		`passphrase="winter-is-coming"`,
		"secret=s3cret-value",
		"wpa2-pre-shared-key=wifi-pass",
		"authentication-password=eap-secret",
		"community=public",
	}, "\n")
	out := redact.String(in)
	leaks := []string{"hunter2", "winter-is-coming", "s3cret-value", "wifi-pass", "eap-secret"}
	for _, leak := range leaks {
		if strings.Contains(out, leak) {
			t.Fatalf("leaked %q: %s", leak, out)
		}
	}
	if !strings.Contains(out, "community=public") {
		t.Fatalf("community= was redacted: %s", out)
	}
}

func TestRedactWPAPreSharedKey(t *testing.T) {
	in := "wpa-pre-shared-key=wpa1-pass"
	out := redact.String(in)
	if strings.Contains(out, "wpa1-pass") {
		t.Fatalf("leaked: %s", out)
	}
}

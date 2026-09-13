package opsgit

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/taihen/rosup/internal/config"
	"golang.org/x/crypto/ssh"
)

func TestAuthEmptyKeyReturnsNil(t *testing.T) {
	auth, err := Auth(&config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if auth != nil {
		t.Fatalf("want nil auth, got %#v", auth)
	}
}

func TestAuthKeyWithoutKnownHostsErrors(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ops.SSHPrivateKeyPath = writeAuthTestPrivateKey(t)
	auth, err := Auth(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if auth != nil {
		t.Fatalf("want nil auth, got %#v", auth)
	}
	if !strings.Contains(err.Error(), "git_known_hosts_path") {
		t.Fatalf("want git_known_hosts_path in error, got %v", err)
	}
}

func TestAuthMissingKeyFileErrors(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ops.SSHPrivateKeyPath = filepath.Join(t.TempDir(), "missing-ops-key")
	cfg.Ops.GitKnownHostsPath = writeAuthTestKnownHosts(t)
	auth, err := Auth(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if auth != nil {
		t.Fatalf("want nil auth, got %#v", auth)
	}
}

func TestAuthValidKeyAndKnownHosts(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ops.SSHPrivateKeyPath = writeAuthTestPrivateKey(t)
	cfg.Ops.GitKnownHostsPath = writeAuthTestKnownHosts(t)

	auth, err := Auth(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if auth == nil {
		t.Fatal("want non-nil auth")
	}
	pk, ok := auth.(*gitssh.PublicKeys)
	if !ok {
		t.Fatalf("want *gitssh.PublicKeys, got %T", auth)
	}
	if pk.HostKeyCallback == nil {
		t.Fatal("HostKeyCallback is nil")
	}

	insecure := ssh.InsecureIgnoreHostKey()
	if reflect.ValueOf(pk.HostKeyCallback).Pointer() == reflect.ValueOf(insecure).Pointer() {
		t.Fatal("HostKeyCallback is ssh.InsecureIgnoreHostKey")
	}

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	if err := pk.HostKeyCallback("evil.example", &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 22}, sshPub); err == nil {
		t.Fatal("HostKeyCallback accepted an unknown host (InsecureIgnoreHostKey?)")
	}
}

func TestAuthUsesOpsGitKnownHostsNotRouterOSTOFU(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ops.SSHPrivateKeyPath = writeAuthTestPrivateKey(t)
	cfg.Ops.GitKnownHostsPath = writeAuthTestKnownHosts(t)

	tofuPath := filepath.Join(t.TempDir(), "routeros_known_hosts")
	if err := os.WriteFile(tofuPath, []byte("this-is-not-a-known-hosts-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.SSH.KnownHostsPath = tofuPath

	assertAuthOK := func(t *testing.T) {
		t.Helper()
		auth, err := Auth(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if auth == nil {
			t.Fatal("want non-nil auth")
		}
		if _, ok := auth.(*gitssh.PublicKeys); !ok {
			t.Fatalf("want *gitssh.PublicKeys, got %T", auth)
		}
	}

	assertAuthOK(t)
	if err := os.Remove(tofuPath); err != nil {
		t.Fatal(err)
	}
	assertAuthOK(t)
}

func TestNeedsSSHAuth(t *testing.T) {
	cases := []struct {
		remote string
		want   bool
	}{
		{"git@github.com:org/ops.git", true},
		{"ssh://git@github.com/org/ops.git", true},
		{"github.com:org/ops.git", true},
		{"user@host:path/repo.git", true},
		{"https://github.com/org/ops.git", false},
		{"file:///tmp/ops.git", false},
		{"/tmp/ops.git", false},
		{"relative/path.git", false},
	}
	for _, tc := range cases {
		if got := needsSSHAuth(tc.remote); got != tc.want {
			t.Fatalf("needsSSHAuth(%q)=%v, want %v", tc.remote, got, tc.want)
		}
	}
}

func writeAuthTestPrivateKey(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519_ops")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeAuthTestKnownHosts(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "git_known_hosts")
	line := "github.com " + string(ssh.MarshalAuthorizedKey(sshPub))
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

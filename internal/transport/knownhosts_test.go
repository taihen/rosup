package transport_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/transport"
)

func TestTOFUFirstConnectWritesKnownHosts0600(t *testing.T) {
	cfg, userKey := testSSHConfig(t, true)
	hostKey := generateSigner(t)
	srv := startTestSSHServer(t, userKey, hostKey)

	ctx := context.Background()
	client, err := transport.Dial(ctx, cfg, srv.host, srv.port)
	if err != nil {
		t.Fatalf("first connect: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	content, lines, mode, exists := knownHostsState(t, cfg.KnownHostsPath)
	if !exists {
		t.Fatal("known_hosts was not created")
	}
	if lines != 1 {
		t.Fatalf("known_hosts lines=%d content=%q", lines, content)
	}
	if mode != 0o600 {
		t.Fatalf("known_hosts mode %04o", mode)
	}

	parent := filepath.Dir(cfg.KnownHostsPath)
	fi, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("known_hosts parent mode %04o", perm)
	}
}

func TestTOFUSecondConnectSameKeyLeavesFileUnchanged(t *testing.T) {
	cfg, userKey := testSSHConfig(t, true)
	hostKey := generateSigner(t)
	srv := startTestSSHServer(t, userKey, hostKey)

	ctx := context.Background()
	client, err := transport.Dial(ctx, cfg, srv.host, srv.port)
	if err != nil {
		t.Fatalf("first connect: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	before, _, _, _ := knownHostsState(t, cfg.KnownHostsPath)

	client, err = transport.Dial(ctx, cfg, srv.host, srv.port)
	if err != nil {
		t.Fatalf("second connect: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	after, lines, mode, exists := knownHostsState(t, cfg.KnownHostsPath)
	if !exists {
		t.Fatal("known_hosts missing after second connect")
	}
	if after != before {
		t.Fatalf("known_hosts changed: before=%q after=%q", before, after)
	}
	if lines != 1 {
		t.Fatalf("known_hosts lines=%d", lines)
	}
	if mode != 0o600 {
		t.Fatalf("known_hosts mode %04o", mode)
	}
}

func TestTOFUSecondConnectDifferentKeyFailsAndLeavesFileUnchanged(t *testing.T) {
	cfg, userKey := testSSHConfig(t, true)
	hostKey := generateSigner(t)
	srv := startTestSSHServer(t, userKey, hostKey)

	ctx := context.Background()
	client, err := transport.Dial(ctx, cfg, srv.host, srv.port)
	if err != nil {
		t.Fatalf("first connect: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	before, _, _, _ := knownHostsState(t, cfg.KnownHostsPath)

	srv.SetHostKey(generateSigner(t))
	client, err = transport.Dial(ctx, cfg, srv.host, srv.port)
	if err == nil {
		_ = client.Close()
		t.Fatal("expected host key mismatch")
	}
	if !strings.Contains(err.Error(), "host key mismatch") {
		t.Fatalf("want host key mismatch, got %v", err)
	}

	after, lines, _, exists := knownHostsState(t, cfg.KnownHostsPath)
	if !exists {
		t.Fatal("known_hosts missing after mismatch")
	}
	if after != before {
		t.Fatalf("known_hosts changed: before=%q after=%q", before, after)
	}
	if lines != 1 {
		t.Fatalf("known_hosts lines=%d", lines)
	}
}

func TestTOFUDisabledEmptyKnownHostsDoesNotWrite(t *testing.T) {
	cfg, userKey := testSSHConfig(t, false)
	hostKey := generateSigner(t)
	srv := startTestSSHServer(t, userKey, hostKey)

	ctx := context.Background()
	client, err := transport.Dial(ctx, cfg, srv.host, srv.port)
	if err == nil {
		_ = client.Close()
		t.Fatal("expected error with TOFU disabled")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unknown host") &&
		!strings.Contains(strings.ToLower(err.Error()), "tofu") {
		t.Fatalf("want unknown host / tofu error, got %v", err)
	}

	_, _, _, exists := knownHostsState(t, cfg.KnownHostsPath)
	if exists {
		t.Fatal("known_hosts was written with TOFU disabled")
	}
}

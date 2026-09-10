package transport_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/transport"
	"golang.org/x/crypto/ssh"
)

func TestDialNegotiatesDHGroupExchangeSHA256(t *testing.T) {
	cfg, userKey := testSSHConfig(t, true)
	srv := startTestSSHServerKEX(t, userKey, generateSigner(t), []string{
		ssh.KeyExchangeDHGEXSHA256,
	})

	ctx := context.Background()
	client, err := transport.Dial(ctx, cfg, srv.host, srv.port)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	out, err := client.Run(ctx, "/system identity print")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "/system identity print") {
		t.Fatalf("stdout %q", out)
	}
}

func TestRunReturnsStdout(t *testing.T) {
	cfg, userKey := testSSHConfig(t, true)
	srv := startTestSSHServer(t, userKey, generateSigner(t))

	ctx := context.Background()
	client, err := transport.Dial(ctx, cfg, srv.host, srv.port)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	out, err := client.Run(ctx, "/system identity print")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "/system identity print") {
		t.Fatalf("stdout %q", out)
	}
}

func TestUploadDownloadRemove(t *testing.T) {
	cfg, userKey := testSSHConfig(t, true)
	srv := startTestSSHServer(t, userKey, generateSigner(t))

	ctx := context.Background()
	client, err := transport.Dial(ctx, cfg, srv.host, srv.port)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	dir := t.TempDir()
	local := filepath.Join(dir, "local.bin")
	payload := []byte("rosup-sftp-payload")
	if err := os.WriteFile(local, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(dir, "remote.bin")
	downloaded := filepath.Join(dir, "downloaded.bin")

	if err := client.Upload(ctx, local, remote); err != nil {
		t.Fatalf("upload: %v", err)
	}
	got, err := os.ReadFile(remote)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("remote content %q", got)
	}

	if err := client.Download(ctx, remote, downloaded); err != nil {
		t.Fatalf("download: %v", err)
	}
	got, err = os.ReadFile(downloaded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("downloaded content %q", got)
	}

	if err := client.Remove(ctx, remote); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(remote); !os.IsNotExist(err) {
		t.Fatalf("remote still exists: %v", err)
	}
}

func TestProductionCodeDoesNotShellOut(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no Go files")
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		if strings.Contains(src, "os/exec") {
			t.Errorf("%s imports os/exec", name)
		}
		for _, bin := range []string{`"ssh"`, `"scp"`, `"sftp"`} {
			if strings.Contains(src, "exec.Command("+bin) {
				t.Errorf("%s shells out to %s", name, bin)
			}
		}
	}
}

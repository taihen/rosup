package transport_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/taihen/rosup/internal/config"
	"golang.org/x/crypto/ssh"
)

type testSSHServer struct {
	mu           sync.Mutex
	hostKey      ssh.Signer
	userKey      ssh.PublicKey
	ln           net.Listener
	host         string
	port         int
	keyExchanges []string
}

func generateSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func writePrivateKey(t *testing.T, path string) ssh.PublicKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, block, 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}

func startTestSSHServer(t *testing.T, userKey ssh.PublicKey, hostKey ssh.Signer) *testSSHServer {
	t.Helper()
	return startTestSSHServerKEX(t, userKey, hostKey, nil)
}

func startTestSSHServerKEX(t *testing.T, userKey ssh.PublicKey, hostKey ssh.Signer, keyExchanges []string) *testSSHServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	srv := &testSSHServer{
		hostKey:      hostKey,
		userKey:      userKey,
		ln:           ln,
		host:         host,
		port:         port,
		keyExchanges: keyExchanges,
	}
	go srv.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return srv
}

func (s *testSSHServer) SetHostKey(key ssh.Signer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hostKey = key
}

func (s *testSSHServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *testSSHServer) handle(nConn net.Conn) {
	defer func() { _ = nConn.Close() }()

	s.mu.Lock()
	hostKey := s.hostKey
	userKey := s.userKey
	keyExchanges := s.keyExchanges
	s.mu.Unlock()

	cfg := &ssh.ServerConfig{
		Config: ssh.Config{KeyExchanges: keyExchanges},
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if conn.User() != "rosup" {
				return nil, fmt.Errorf("bad user %q", conn.User())
			}
			if !keysEqual(key, userKey) {
				return nil, fmt.Errorf("unauthorized key")
			}
			return nil, nil
		},
	}
	cfg.AddHostKey(hostKey)

	_, chans, reqs, err := ssh.NewServerConn(nConn, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for newChan := range chans {
		go handleChannel(newChan)
	}
}

func handleChannel(newChan ssh.NewChannel) {
	if newChan.ChannelType() != "session" {
		_ = newChan.Reject(ssh.UnknownChannelType, "unknown")
		return
	}
	channel, requests, err := newChan.Accept()
	if err != nil {
		return
	}
	defer func() { _ = channel.Close() }()

	for req := range requests {
		switch req.Type {
		case "exec":
			var msg struct {
				Command string
			}
			if err := ssh.Unmarshal(req.Payload, &msg); err != nil {
				if req.WantReply {
					_ = req.Reply(false, nil)
				}
				return
			}
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			_, _ = io.WriteString(channel, "stdout:"+msg.Command+"\n")
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(&struct{ Status uint32 }{0}))
			return
		case "subsystem":
			name := ""
			if len(req.Payload) >= 4 {
				name = string(req.Payload[4:])
			}
			if name == "sftp" {
				if req.WantReply {
					_ = req.Reply(true, nil)
				}
				serveSFTP(channel)
				return
			}
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

func serveSFTP(channel ssh.Channel) {
	server, err := sftp.NewServer(channel)
	if err != nil {
		return
	}
	_ = server.Serve()
	_ = server.Close()
}

func keysEqual(a, b ssh.PublicKey) bool {
	if a == nil || b == nil {
		return false
	}
	return a.Type() == b.Type() && string(a.Marshal()) == string(b.Marshal())
}

func testSSHConfig(t *testing.T, tofu bool) (config.SSHConfig, ssh.PublicKey) {
	t.Helper()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	pub := writePrivateKey(t, keyPath)
	return config.SSHConfig{
		Username:       "rosup",
		PrivateKeyPath: keyPath,
		KnownHostsPath: filepath.Join(dir, "ssh", "known_hosts"),
		TOFU:           tofu,
		Timeout:        5 * time.Second,
		DefaultPort:    22,
	}, pub
}

func knownHostsState(t *testing.T, path string) (content string, lines int, mode os.FileMode, exists bool) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0, 0, false
		}
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return string(data), n, fi.Mode().Perm(), true
}

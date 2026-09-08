package transport

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/taihen/rosup/internal/state"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var (
	ErrHostKeyMismatch = errors.New("ssh: host key mismatch")
	ErrUnknownHost     = errors.New("ssh: unknown host")
)

func hostKeyCallback(knownHostsPath string, tofu bool) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := checkKnownHost(knownHostsPath, hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) {
			return err
		}
		if len(keyErr.Want) > 0 {
			return fmt.Errorf("%w for %s", ErrHostKeyMismatch, hostname)
		}
		if !tofu {
			return fmt.Errorf("%w %s (tofu disabled)", ErrUnknownHost, hostname)
		}
		if writeErr := appendKnownHost(knownHostsPath, hostname, key); writeErr != nil {
			return writeErr
		}
		return nil
	}
}

func checkKnownHost(path, hostname string, remote net.Addr, key ssh.PublicKey) error {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &knownhosts.KeyError{}
		}
		return fmt.Errorf("transport: stat known_hosts %s: %w", path, err)
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		return fmt.Errorf("transport: load known_hosts %s: %w", path, err)
	}
	return cb(hostname, remote, key)
}

func appendKnownHost(path, hostname string, key ssh.PublicKey) error {
	if err := state.EnsureSecureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("transport: open known_hosts %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("transport: chmod known_hosts %s: %w", path, err)
	}
	line := knownhosts.Line([]string{hostname}, key) + "\n"
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("transport: write known_hosts %s: %w", path, err)
	}
	return nil
}

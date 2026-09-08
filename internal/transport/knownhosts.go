package transport

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var (
	ErrHostKeyMismatch = errors.New("ssh: host key mismatch")
	ErrUnknownHost     = errors.New("ssh: unknown host")
)

type hostKeyPin struct {
	path     string
	tofu     bool
	hostname string
	key      ssh.PublicKey
}

func (p *hostKeyPin) callback(hostname string, remote net.Addr, key ssh.PublicKey) error {
	err := checkKnownHost(p.path, hostname, remote, key)
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
	if !p.tofu {
		return fmt.Errorf("%w %s (tofu disabled)", ErrUnknownHost, hostname)
	}
	p.hostname = hostname
	p.key = key
	return nil
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
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("transport: mkdir %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("transport: chmod %s: %w", dir, err)
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

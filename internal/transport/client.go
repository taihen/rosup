package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/redact"
	"golang.org/x/crypto/ssh"
)

type Client interface {
	Run(ctx context.Context, command string) (stdout string, err error)
	Upload(ctx context.Context, local, remote string) error
	Download(ctx context.Context, remote, local string) error
	Remove(ctx context.Context, remote string) error
	Close() error
}

type sshClient struct {
	conn *ssh.Client
	mu   sync.Mutex
	sftp *sftp.Client
}

func Dial(ctx context.Context, cfg config.SSHConfig, address string, port int) (Client, error) {
	if ctx == nil {
		return nil, errors.New("transport: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if port <= 0 {
		port = cfg.DefaultPort
	}
	if port <= 0 {
		return nil, errors.New("transport: port is required")
	}

	keyBytes, err := os.ReadFile(cfg.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("transport: read private key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("transport: parse private key: %w", err)
	}

	addr := net.JoinHostPort(address, strconv.Itoa(port))
	dialer := net.Dialer{Timeout: cfg.Timeout}
	nConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("transport: dial %s: %w", addr, err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = nConn.SetDeadline(deadline)
	} else if cfg.Timeout > 0 {
		_ = nConn.SetDeadline(time.Now().Add(cfg.Timeout))
	}

	sshCfg := &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback(cfg.KnownHostsPath, cfg.TOFU),
		Timeout:         cfg.Timeout,
	}
	conn, chans, reqs, err := ssh.NewClientConn(nConn, addr, sshCfg)
	if err != nil {
		return nil, fmt.Errorf("transport: ssh handshake: %w", err)
	}
	_ = nConn.SetDeadline(time.Time{})

	return &sshClient{conn: ssh.NewClient(conn, chans, reqs)}, nil
}

func (c *sshClient) Run(ctx context.Context, command string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	session, err := c.conn.NewSession()
	if err != nil {
		return "", fmt.Errorf("transport: ssh session: %w", err)
	}
	defer func() { _ = session.Close() }()

	var stderr bytes.Buffer
	session.Stderr = &stderr

	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := session.Output(command)
		done <- result{out, err}
	}()

	select {
	case <-ctx.Done():
		_ = session.Close()
		return "", ctx.Err()
	case r := <-done:
		if r.err != nil {
			if stderr.Len() > 0 {
				return string(r.out), fmt.Errorf("transport: ssh run: %w: %s", r.err, redact.String(stderr.String()))
			}
			return string(r.out), fmt.Errorf("transport: ssh run: %w", r.err)
		}
		return string(r.out), nil
	}
}

func (c *sshClient) Upload(ctx context.Context, local, remote string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sc, err := c.sftpClient()
	if err != nil {
		return err
	}
	in, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("transport: open local %s: %w", local, err)
	}
	defer func() { _ = in.Close() }()

	out, err := sc.Create(remote)
	if err != nil {
		return fmt.Errorf("transport: sftp create %s: %w", remote, err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("transport: sftp upload %s: %w", remote, err)
	}
	return nil
}

func (c *sshClient) Download(ctx context.Context, remote, local string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sc, err := c.sftpClient()
	if err != nil {
		return err
	}
	in, err := sc.Open(remote)
	if err != nil {
		return fmt.Errorf("transport: sftp open %s: %w", remote, err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(local, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("transport: create local %s: %w", local, err)
	}
	defer func() { _ = out.Close() }()
	if err := os.Chmod(local, 0o600); err != nil {
		return fmt.Errorf("transport: chmod local %s: %w", local, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("transport: sftp download %s: %w", remote, err)
	}
	return nil
}

func (c *sshClient) Remove(ctx context.Context, remote string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sc, err := c.sftpClient()
	if err != nil {
		return err
	}
	if err := sc.Remove(remote); err != nil {
		return fmt.Errorf("transport: sftp remove %s: %w", remote, err)
	}
	return nil
}

func (c *sshClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var err error
	if c.sftp != nil {
		err = c.sftp.Close()
		c.sftp = nil
	}
	if c.conn != nil {
		if closeErr := c.conn.Close(); err == nil {
			err = closeErr
		}
		c.conn = nil
	}
	return err
}

func (c *sshClient) sftpClient() (*sftp.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sftp != nil {
		return c.sftp, nil
	}
	sc, err := sftp.NewClient(c.conn)
	if err != nil {
		return nil, fmt.Errorf("transport: sftp: %w", err)
	}
	c.sftp = sc
	return sc, nil
}

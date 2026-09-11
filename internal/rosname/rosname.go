package rosname

import (
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"

	"golang.org/x/crypto/ssh"
)

var safe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Valid reports whether s is safe to embed in a RouterOS property value
// without quoting (no spaces, equals, or other tokens).
func Valid(s string) bool {
	return s != "" && safe.MatchString(s)
}

func Check(kind, s string) error {
	if Valid(s) {
		return nil
	}
	return fmt.Errorf("%s %q must match %s", kind, s, safe.String())
}

// IsExpectedDisconnect reports SSH/session errors that are normal after
// /system reboot tears down the active connection.
func IsExpectedDisconnect(err error) bool {
	if err == nil {
		return true
	}
	var exitMissing *ssh.ExitMissingError
	if errors.As(err, &exitMissing) {
		return true
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	s := strings.ToLower(err.Error())
	for _, n := range []string{
		"connection reset",
		"broken pipe",
		"connection lost",
		"use of closed network connection",
		"eof",
		"session shutdown",
		"exited without exit status",
		"exited without exit signal",
	} {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

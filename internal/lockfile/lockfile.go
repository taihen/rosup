package lockfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

var ErrLocked = errors.New("lockfile: already locked")

var held sync.Map

func Acquire(path string) (func() error, error) {
	if path == "" {
		return nil, errors.New("lockfile: empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("lockfile: resolve %s: %w", path, err)
	}

	dir := filepath.Dir(abs)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("lockfile: mkdir %s: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return nil, fmt.Errorf("lockfile: chmod %s: %w", dir, err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("lockfile: stat %s: %w", dir, err)
	}

	if _, exists := held.LoadOrStore(abs, struct{}{}); exists {
		return nil, ErrLocked
	}

	f, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		held.Delete(abs)
		return nil, fmt.Errorf("lockfile: open %s: %w", abs, err)
	}
	if err := os.Chmod(abs, 0o600); err != nil {
		held.Delete(abs)
		_ = f.Close()
		return nil, fmt.Errorf("lockfile: chmod %s: %w", abs, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		held.Delete(abs)
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("lockfile: flock %s: %w", abs, err)
	}

	var once sync.Once
	var unlockErr error
	return func() error {
		once.Do(func() {
			unlockErr = unix.Flock(int(f.Fd()), unix.LOCK_UN)
			if closeErr := f.Close(); unlockErr == nil {
				unlockErr = closeErr
			}
			held.Delete(abs)
		})
		return unlockErr
	}, nil
}

package lockfile_test

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/taihen/rosup/internal/lockfile"
)

func TestMain(m *testing.M) {
	switch os.Getenv("ROSUP_LOCK_HELPER") {
	case "hold":
		os.Exit(lockHelperHold())
	case "die":
		os.Exit(lockHelperDie())
	}
	os.Exit(m.Run())
}

func lockHelperHold() int {
	unlock, err := lockfile.Acquire(os.Getenv("ROSUP_LOCK_PATH"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("locked")
	if _, err := bufio.NewReader(os.Stdin).ReadByte(); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := unlock(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func lockHelperDie() int {
	_, err := lockfile.Acquire(os.Getenv("ROSUP_LOCK_PATH"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func TestAcquireSecondFailsInSameProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rosup.lock")
	unlock, err := lockfile.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unlock() })

	_, err = lockfile.Acquire(path)
	if !errors.Is(err, lockfile.ErrLocked) {
		t.Fatalf("second acquire: %v", err)
	}
}

func TestAcquireSucceedsAfterUnlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rosup.lock")
	unlock, err := lockfile.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}

	unlock2, err := lockfile.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := unlock2(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireFailsWhileOtherProcessHolds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rosup.lock")
	cmd, stdin, stdout := startLockHelper(t, "hold", path)

	if got, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || got != "locked\n" {
		t.Fatalf("child ready: %q %v", got, err)
	}

	_, err := lockfile.Acquire(path)
	if !errors.Is(err, lockfile.ErrLocked) {
		t.Fatalf("acquire while held: %v", err)
	}

	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}

	unlock, err := lockfile.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireAfterProcessDeath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rosup.lock")
	cmd, _, _ := startLockHelper(t, "die", path)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}

	unlock, err := lockfile.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireCreatesParentDir0700(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "rosup.lock")
	unlock, err := lockfile.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unlock() })

	fi, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("parent perm %04o", perm)
	}

	fi, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("lock file perm %04o", perm)
	}
}

func TestAcquireTightensExistingParentDir0700(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rosup.lock")
	unlock, err := lockfile.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unlock() })

	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("parent perm %04o", perm)
	}
}

func startLockHelper(t *testing.T, mode, path string) (*exec.Cmd, io.WriteCloser, io.ReadCloser) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(),
		"ROSUP_LOCK_HELPER="+mode,
		"ROSUP_LOCK_PATH="+path,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd, stdin, stdout
}

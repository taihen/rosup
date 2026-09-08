package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/lockfile"
	"github.com/taihen/rosup/internal/release"
)

func execute(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := newRootCmd()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestVersion(t *testing.T) {
	out, _, err := execute(t, "version")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("rosup %s (%s) %s\n", version, commit, date)
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestVersionDoesNotRequireConfig(t *testing.T) {
	t.Setenv("ROSUP_CONFIG", "/no/such/rosup.yaml")
	_, _, err := execute(t, "version")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = execute(t, "--config", "/no/such/rosup.yaml", "version")
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoArgsExitsNonZero(t *testing.T) {
	_, _, err := execute(t)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUnknownCommandExitsNonZero(t *testing.T) {
	_, _, err := execute(t, "definitely-not-a-command")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStubCommandsNotImplemented(t *testing.T) {
	commands := [][]string{
		{"rollback"},
		{"backup", "restore"},
	}
	for _, args := range commands {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, _, err := execute(t, args...)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "not implemented") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestReleaseRequiresSubcommand(t *testing.T) {
	_, _, err := execute(t, "release")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBackupRequiresSubcommand(t *testing.T) {
	_, _, err := execute(t, "backup")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDiscoverMissingConfigFlag(t *testing.T) {
	_, _, err := execute(t, "--config", "/no/such/rosup.yaml", "discover")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("should fail on missing config before the stub, got %v", err)
	}
}

func TestDiscoverLoadsConfigAndNeedsInventory(t *testing.T) {
	configPath, _ := writeCLIConfig(t)
	_, _, err := execute(t, "--config", configPath, "discover")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("should load inventory, got %v", err)
	}
	if !strings.Contains(err.Error(), "inventory") {
		t.Fatalf("got %v", err)
	}
}

func TestDiscoverHeldLock(t *testing.T) {
	configPath, _ := writeCLIConfig(t)
	dir := filepath.Dir(configPath)
	lockPath := filepath.Join(dir, "state", "rosup.lock")
	unlock, err := lockfile.Acquire(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unlock() })

	_, _, err = execute(t, "--config", configPath, "discover")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, lockfile.ErrLocked) {
		t.Fatalf("got %v", err)
	}
}

func TestUpgradeRequiresRelease(t *testing.T) {
	_, _, err := execute(t, "upgrade")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--release") {
		t.Fatalf("got %v", err)
	}
}

func TestUpgradeHeldLock(t *testing.T) {
	configPath, _ := writeCLIConfig(t)
	dir := filepath.Dir(configPath)
	lockPath := filepath.Join(dir, "state", "rosup.lock")
	unlock, err := lockfile.Acquire(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unlock() })

	_, _, err = execute(t, "--config", configPath, "--release", "6.49.21", "upgrade")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, lockfile.ErrLocked) {
		t.Fatalf("got %v", err)
	}
}

func TestPlanRequiresRelease(t *testing.T) {
	_, _, err := execute(t, "plan")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--release") {
		t.Fatalf("got %v", err)
	}
}

func TestPlanHeldLock(t *testing.T) {
	configPath, _ := writeCLIConfig(t)
	dir := filepath.Dir(configPath)
	lockPath := filepath.Join(dir, "state", "rosup.lock")
	unlock, err := lockfile.Acquire(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unlock() })

	_, _, err = execute(t, "--config", configPath, "--release", "6.49.21", "plan")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, lockfile.ErrLocked) {
		t.Fatalf("got %v", err)
	}
}

func TestPlanMissingManifest(t *testing.T) {
	configPath, _ := writeCLIConfig(t)
	_, _, err := execute(t, "--config", configPath, "plan", "--release", "6.49.21")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("should load the local manifest, got %v", err)
	}
	if !strings.Contains(err.Error(), "6.49.21") && !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("got %v", err)
	}
}

func TestVerifyRequiresDevice(t *testing.T) {
	_, _, err := execute(t, "verify")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("verify should require a device, got %v", err)
	}
}

func TestVerifyHeldLock(t *testing.T) {
	configPath, _ := writeCLIConfig(t)
	dir := filepath.Dir(configPath)
	lockPath := filepath.Join(dir, "state", "rosup.lock")
	unlock, err := lockfile.Acquire(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unlock() })

	_, _, err = execute(t, "--config", configPath, "verify", "router-01")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, lockfile.ErrLocked) {
		t.Fatalf("got %v", err)
	}
}

func TestVerifyUnknownDevice(t *testing.T) {
	configPath, _ := writeCLIConfig(t)
	dir := filepath.Dir(configPath)
	inv := filepath.Join(dir, "ops", "inventory", "devices.yaml")
	if err := os.MkdirAll(filepath.Dir(inv), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `devices:
  - name: other
    address: 192.0.2.9
    role: access
    group: edge
    order: 1
    validation_profile: access
`
	if err := os.WriteFile(inv, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := execute(t, "--config", configPath, "verify", "router-01")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("should look up inventory, got %v", err)
	}
	if !strings.Contains(err.Error(), "router-01") {
		t.Fatalf("got %v", err)
	}
}

func TestPersistentFlagsRegistered(t *testing.T) {
	cmd := newRootCmd()
	for _, name := range []string{"config", "release", "group", "to-version", "file", "resume"} {
		if cmd.PersistentFlags().Lookup(name) == nil {
			t.Errorf("missing flag --%s", name)
		}
	}
}

func writeCLIConfig(t *testing.T) (configPath, packageDir string) {
	t.Helper()
	dir := t.TempDir()
	packageDir = filepath.Join(dir, "packages")
	yaml := fmt.Sprintf(`
data_dir: %s
state_dir: %s
package_dir: %s
backup_dir: %s
lock_path: %s
architectures: [arm]
channel: long-term
ops:
  path: %s
  remote: git@example.com:org/ops.git
ssh:
  private_key_path: %s
  known_hosts_path: %s
`,
		filepath.Join(dir, "data"),
		filepath.Join(dir, "state"),
		packageDir,
		filepath.Join(dir, "backups"),
		filepath.Join(dir, "state", "rosup.lock"),
		filepath.Join(dir, "ops"),
		filepath.Join(dir, "ssh", "id_ed25519"),
		filepath.Join(dir, "ssh", "known_hosts"),
	)
	configPath = filepath.Join(dir, "rosup.yaml")
	if err := os.WriteFile(configPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath, packageDir
}

func TestReleaseListMissingConfig(t *testing.T) {
	t.Setenv("ROSUP_CONFIG", "")
	_, _, err := execute(t, "release", "list")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("should load config, got %v", err)
	}
}

func TestReleaseSyncMissingConfig(t *testing.T) {
	t.Setenv("ROSUP_CONFIG", "")
	_, _, err := execute(t, "release", "sync")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("should load config, got %v", err)
	}
}

func TestReleaseListPrintsVersions(t *testing.T) {
	configPath, packageDir := writeCLIConfig(t)
	for _, v := range []string{"6.49.21", "6.48.7"} {
		sub := filepath.Join(packageDir, v)
		if err := os.MkdirAll(sub, 0o700); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(release.Manifest{Version: v, Channel: "long-term"})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "manifest.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out, _, err := execute(t, "--config", configPath, "release", "list")
	if err != nil {
		t.Fatal(err)
	}
	if out != "6.48.7\n6.49.21\n" {
		t.Fatalf("got %q", out)
	}
}

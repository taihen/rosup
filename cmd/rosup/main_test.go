package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		{"discover"},
		{"plan"},
		{"upgrade"},
		{"verify"},
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

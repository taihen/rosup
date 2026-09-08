package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/taihen/rosup/internal/config"
)

func writeYAML(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "rosup.yaml")
	if err := os.WriteFile(p, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const validYAML = `
data_dir: /var/lib/rosup
state_dir: /var/lib/rosup/state
package_dir: /var/lib/rosup/packages
backup_dir: /var/lib/rosup/backups
lock_path: /var/lib/rosup/state/rosup.lock
architectures: [arm, mmips]
channel: long-term
ops:
  path: /var/lib/rosup/ops
  remote: git@github.com:netrunnerlabs/rosup-ops.git
ssh:
  private_key_path: /var/lib/rosup/ssh/id_ed25519
  known_hosts_path: /var/lib/rosup/ssh/known_hosts
`

func TestLoadMissingFile(t *testing.T) {
	_, err := config.Load("/no/such/rosup.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadRejectsNonLongTerm(t *testing.T) {
	p := writeYAML(t, "channel: stable\narchitectures: [arm]\n")
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadDefaults(t *testing.T) {
	p := writeYAML(t, `
data_dir: /var/lib/rosup
state_dir: /var/lib/rosup/state
package_dir: /var/lib/rosup/packages
backup_dir: /var/lib/rosup/backups
lock_path: /var/lib/rosup/state/rosup.lock
architectures: [arm, mmips]
channel: long-term
ops:
  path: /var/lib/rosup/ops
  remote: git@github.com:netrunnerlabs/rosup-ops.git
ssh:
  private_key_path: /var/lib/rosup/ssh/id_ed25519
  known_hosts_path: /var/lib/rosup/ssh/known_hosts
`)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SSH.Username != "rosup" || cfg.SSH.DefaultPort != 22 || !cfg.SSH.TOFU {
		t.Fatalf("defaults: %+v", cfg.SSH)
	}
	if cfg.SSH.Timeout != 30*time.Second {
		t.Fatalf("ssh.timeout default %s", cfg.SSH.Timeout)
	}
	if cfg.Reconnect.Attempts != 3 || cfg.Reconnect.Timeout != 3*time.Minute {
		t.Fatalf("reconnect: %+v", cfg.Reconnect)
	}
	if cfg.BackupRetentionDays != 30 {
		t.Fatalf("retention %d", cfg.BackupRetentionDays)
	}
}

func TestLoadOpsDefaults(t *testing.T) {
	p := writeYAML(t, validYAML)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Ops.InventoryFile != "inventory/devices.yaml" {
		t.Fatalf("inventory file %q", cfg.Ops.InventoryFile)
	}
	if cfg.Ops.AuditDir != "audit" {
		t.Fatalf("audit dir %q", cfg.Ops.AuditDir)
	}
	if cfg.Ops.SSHPrivateKeyPath != "" {
		t.Fatalf("ops.ssh_private_key_path %q, want empty", cfg.Ops.SSHPrivateKeyPath)
	}
	if cfg.Ops.GitKnownHostsPath != "" {
		t.Fatalf("ops.git_known_hosts_path %q, want empty", cfg.Ops.GitKnownHostsPath)
	}
}

func TestLoadSetsConfigPath(t *testing.T) {
	p := writeYAML(t, validYAML)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigPath != abs {
		t.Fatalf("ConfigPath %q want %q", cfg.ConfigPath, abs)
	}
}

func TestLoadResolvesRelativePaths(t *testing.T) {
	p := writeYAML(t, `
data_dir: data
state_dir: state
package_dir: packages
backup_dir: backups
lock_path: state/rosup.lock
architectures: [arm]
channel: long-term
ops:
  path: ops
  remote: git@example.com:org/ops.git
ssh:
  private_key_path: ssh/id_ed25519
  known_hosts_path: ssh/known_hosts
`)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(p)
	checks := map[string]string{
		"data_dir":             filepath.Join(dir, "data"),
		"state_dir":            filepath.Join(dir, "state"),
		"package_dir":          filepath.Join(dir, "packages"),
		"backup_dir":           filepath.Join(dir, "backups"),
		"lock_path":            filepath.Join(dir, "state", "rosup.lock"),
		"ops.path":             filepath.Join(dir, "ops"),
		"ssh.private_key_path": filepath.Join(dir, "ssh", "id_ed25519"),
		"ssh.known_hosts_path": filepath.Join(dir, "ssh", "known_hosts"),
	}
	got := map[string]string{
		"data_dir":             cfg.DataDir,
		"state_dir":            cfg.StateDir,
		"package_dir":          cfg.PackageDir,
		"backup_dir":           cfg.BackupDir,
		"lock_path":            cfg.LockPath,
		"ops.path":             cfg.Ops.Path,
		"ssh.private_key_path": cfg.SSH.PrivateKeyPath,
		"ssh.known_hosts_path": cfg.SSH.KnownHostsPath,
	}
	for name, want := range checks {
		if got[name] != want {
			t.Errorf("%s: got %q want %q", name, got[name], want)
		}
	}
}

func TestLoadResolvesRelativeOpsGitSSHPaths(t *testing.T) {
	p := writeYAML(t, `
data_dir: data
state_dir: state
package_dir: packages
backup_dir: backups
lock_path: state/rosup.lock
architectures: [arm]
channel: long-term
ops:
  path: ops
  remote: git@example.com:org/ops.git
  ssh_private_key_path: ssh/id_ed25519_ops
  git_known_hosts_path: ssh/git_known_hosts
ssh:
  private_key_path: ssh/id_ed25519
  known_hosts_path: ssh/known_hosts
`)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(p)
	if cfg.Ops.SSHPrivateKeyPath != filepath.Join(dir, "ssh", "id_ed25519_ops") {
		t.Fatalf("ops.ssh_private_key_path %q", cfg.Ops.SSHPrivateKeyPath)
	}
	if cfg.Ops.GitKnownHostsPath != filepath.Join(dir, "ssh", "git_known_hosts") {
		t.Fatalf("ops.git_known_hosts_path %q", cfg.Ops.GitKnownHostsPath)
	}
}

func TestLoadKeepsAbsolutePaths(t *testing.T) {
	p := writeYAML(t, validYAML)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/var/lib/rosup" {
		t.Fatalf("data_dir %q", cfg.DataDir)
	}
	if cfg.Ops.Path != "/var/lib/rosup/ops" {
		t.Fatalf("ops.path %q", cfg.Ops.Path)
	}
}

func TestLoadTOFUExplicitFalse(t *testing.T) {
	p := writeYAML(t, `
data_dir: /var/lib/rosup
state_dir: /var/lib/rosup/state
package_dir: /var/lib/rosup/packages
backup_dir: /var/lib/rosup/backups
lock_path: /var/lib/rosup/state/rosup.lock
architectures: [arm, mmips]
channel: long-term
ops:
  path: /var/lib/rosup/ops
  remote: git@github.com:netrunnerlabs/rosup-ops.git
ssh:
  private_key_path: /var/lib/rosup/ssh/id_ed25519
  known_hosts_path: /var/lib/rosup/ssh/known_hosts
  tofu: false
`)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SSH.TOFU {
		t.Fatal("expected tofu false")
	}
}

func TestLoadRejectsEmptyArchitectures(t *testing.T) {
	p := writeYAML(t, `
data_dir: /var/lib/rosup
state_dir: /var/lib/rosup/state
package_dir: /var/lib/rosup/packages
backup_dir: /var/lib/rosup/backups
lock_path: /var/lib/rosup/state/rosup.lock
architectures: []
channel: long-term
ops:
  path: /var/lib/rosup/ops
  remote: git@github.com:netrunnerlabs/rosup-ops.git
ssh:
  private_key_path: /var/lib/rosup/ssh/id_ed25519
  known_hosts_path: /var/lib/rosup/ssh/known_hosts
`)
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadRejectsMissingRequired(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"missing architectures", `
data_dir: /var/lib/rosup
state_dir: /var/lib/rosup/state
package_dir: /var/lib/rosup/packages
backup_dir: /var/lib/rosup/backups
lock_path: /var/lib/rosup/state/rosup.lock
channel: long-term
ops:
  path: /var/lib/rosup/ops
  remote: git@github.com:netrunnerlabs/rosup-ops.git
ssh:
  private_key_path: /var/lib/rosup/ssh/id_ed25519
  known_hosts_path: /var/lib/rosup/ssh/known_hosts
`},
		{"missing data_dir", `
state_dir: /var/lib/rosup/state
package_dir: /var/lib/rosup/packages
backup_dir: /var/lib/rosup/backups
lock_path: /var/lib/rosup/state/rosup.lock
architectures: [arm]
channel: long-term
ops:
  path: /var/lib/rosup/ops
  remote: git@github.com:netrunnerlabs/rosup-ops.git
ssh:
  private_key_path: /var/lib/rosup/ssh/id_ed25519
  known_hosts_path: /var/lib/rosup/ssh/known_hosts
`},
		{"missing state_dir", `
data_dir: /var/lib/rosup
package_dir: /var/lib/rosup/packages
backup_dir: /var/lib/rosup/backups
lock_path: /var/lib/rosup/state/rosup.lock
architectures: [arm]
channel: long-term
ops:
  path: /var/lib/rosup/ops
  remote: git@github.com:netrunnerlabs/rosup-ops.git
ssh:
  private_key_path: /var/lib/rosup/ssh/id_ed25519
  known_hosts_path: /var/lib/rosup/ssh/known_hosts
`},
		{"missing package_dir", `
data_dir: /var/lib/rosup
state_dir: /var/lib/rosup/state
backup_dir: /var/lib/rosup/backups
lock_path: /var/lib/rosup/state/rosup.lock
architectures: [arm]
channel: long-term
ops:
  path: /var/lib/rosup/ops
  remote: git@github.com:netrunnerlabs/rosup-ops.git
ssh:
  private_key_path: /var/lib/rosup/ssh/id_ed25519
  known_hosts_path: /var/lib/rosup/ssh/known_hosts
`},
		{"missing backup_dir", `
data_dir: /var/lib/rosup
state_dir: /var/lib/rosup/state
package_dir: /var/lib/rosup/packages
lock_path: /var/lib/rosup/state/rosup.lock
architectures: [arm]
channel: long-term
ops:
  path: /var/lib/rosup/ops
  remote: git@github.com:netrunnerlabs/rosup-ops.git
ssh:
  private_key_path: /var/lib/rosup/ssh/id_ed25519
  known_hosts_path: /var/lib/rosup/ssh/known_hosts
`},
		{"missing lock_path", `
data_dir: /var/lib/rosup
state_dir: /var/lib/rosup/state
package_dir: /var/lib/rosup/packages
backup_dir: /var/lib/rosup/backups
architectures: [arm]
channel: long-term
ops:
  path: /var/lib/rosup/ops
  remote: git@github.com:netrunnerlabs/rosup-ops.git
ssh:
  private_key_path: /var/lib/rosup/ssh/id_ed25519
  known_hosts_path: /var/lib/rosup/ssh/known_hosts
`},
		{"missing ops.path", `
data_dir: /var/lib/rosup
state_dir: /var/lib/rosup/state
package_dir: /var/lib/rosup/packages
backup_dir: /var/lib/rosup/backups
lock_path: /var/lib/rosup/state/rosup.lock
architectures: [arm]
channel: long-term
ops:
  remote: git@github.com:netrunnerlabs/rosup-ops.git
ssh:
  private_key_path: /var/lib/rosup/ssh/id_ed25519
  known_hosts_path: /var/lib/rosup/ssh/known_hosts
`},
		{"missing ops.remote", `
data_dir: /var/lib/rosup
state_dir: /var/lib/rosup/state
package_dir: /var/lib/rosup/packages
backup_dir: /var/lib/rosup/backups
lock_path: /var/lib/rosup/state/rosup.lock
architectures: [arm]
channel: long-term
ops:
  path: /var/lib/rosup/ops
ssh:
  private_key_path: /var/lib/rosup/ssh/id_ed25519
  known_hosts_path: /var/lib/rosup/ssh/known_hosts
`},
		{"missing ssh.private_key_path", `
data_dir: /var/lib/rosup
state_dir: /var/lib/rosup/state
package_dir: /var/lib/rosup/packages
backup_dir: /var/lib/rosup/backups
lock_path: /var/lib/rosup/state/rosup.lock
architectures: [arm]
channel: long-term
ops:
  path: /var/lib/rosup/ops
  remote: git@github.com:netrunnerlabs/rosup-ops.git
ssh:
  known_hosts_path: /var/lib/rosup/ssh/known_hosts
`},
		{"missing ssh.known_hosts_path", `
data_dir: /var/lib/rosup
state_dir: /var/lib/rosup/state
package_dir: /var/lib/rosup/packages
backup_dir: /var/lib/rosup/backups
lock_path: /var/lib/rosup/state/rosup.lock
architectures: [arm]
channel: long-term
ops:
  path: /var/lib/rosup/ops
  remote: git@github.com:netrunnerlabs/rosup-ops.git
ssh:
  private_key_path: /var/lib/rosup/ssh/id_ed25519
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := writeYAML(t, tc.yaml)
			_, err := config.Load(p)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestResolvePathFlagWins(t *testing.T) {
	t.Setenv("ROSUP_CONFIG", "/from/env.yaml")
	got := config.ResolvePath("/from/flag.yaml")
	if got != "/from/flag.yaml" {
		t.Fatalf("got %q", got)
	}
}

func TestResolvePathEnv(t *testing.T) {
	t.Setenv("ROSUP_CONFIG", "/from/env.yaml")
	got := config.ResolvePath("")
	if got != "/from/env.yaml" {
		t.Fatalf("got %q", got)
	}
}

func TestResolvePathDefault(t *testing.T) {
	t.Setenv("ROSUP_CONFIG", "")
	got := config.ResolvePath("")
	if got != "./rosup.yaml" {
		t.Fatalf("got %q", got)
	}
}

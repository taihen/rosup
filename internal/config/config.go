package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultChannel             = "long-term"
	defaultBackupRetentionDays = 30
	defaultSSHUsername         = "rosup"
	defaultSSHPort             = 22
	defaultReconnectAttempts   = 3
	defaultOpsInventoryFile    = "inventory/devices.yaml"
	defaultOpsAuditDir         = "audit"
	defaultConfigPath          = "./rosup.yaml"
)

var defaultReconnectTimeout = 3 * time.Minute

type Config struct {
	ConfigPath string `yaml:"-"`

	DataDir             string        `yaml:"data_dir"`
	StateDir            string        `yaml:"state_dir"`
	PackageDir          string        `yaml:"package_dir"`
	BackupDir           string        `yaml:"backup_dir"`
	BackupRetentionDays int           `yaml:"backup_retention_days"`
	LockPath            string        `yaml:"lock_path"`
	Ops                 OpsConfig     `yaml:"ops"`
	SSH                 SSHConfig     `yaml:"ssh"`
	Reconnect           Reconnect     `yaml:"reconnect"`
	Architectures       []string      `yaml:"architectures"`
	Channel             string        `yaml:"channel"` // must be "long-term"
	UpgradeTimeout      time.Duration `yaml:"upgrade_timeout"`
}

type OpsConfig struct {
	Path          string `yaml:"path"`
	Remote        string `yaml:"remote"`
	InventoryFile string `yaml:"inventory_file"` // default "inventory/devices.yaml"
	AuditDir      string `yaml:"audit_dir"`      // default "audit"
}

type SSHConfig struct {
	Username       string        `yaml:"username"` // default "rosup"
	PrivateKeyPath string        `yaml:"private_key_path"`
	KnownHostsPath string        `yaml:"known_hosts_path"`
	TOFU           bool          `yaml:"tofu"` // default true
	Timeout        time.Duration `yaml:"timeout"`
	DefaultPort    int           `yaml:"default_port"` // default 22
}

type Reconnect struct {
	Timeout  time.Duration `yaml:"timeout"`  // default 3m
	Attempts int           `yaml:"attempts"` // default 3
}

type fileConfig struct {
	DataDir             string        `yaml:"data_dir"`
	StateDir            string        `yaml:"state_dir"`
	PackageDir          string        `yaml:"package_dir"`
	BackupDir           string        `yaml:"backup_dir"`
	BackupRetentionDays int           `yaml:"backup_retention_days"`
	LockPath            string        `yaml:"lock_path"`
	Ops                 OpsConfig     `yaml:"ops"`
	SSH                 fileSSHConfig `yaml:"ssh"`
	Reconnect           Reconnect     `yaml:"reconnect"`
	Architectures       []string      `yaml:"architectures"`
	Channel             string        `yaml:"channel"`
	UpgradeTimeout      time.Duration `yaml:"upgrade_timeout"`
}

type fileSSHConfig struct {
	Username       string        `yaml:"username"`
	PrivateKeyPath string        `yaml:"private_key_path"`
	KnownHostsPath string        `yaml:"known_hosts_path"`
	TOFU           *bool         `yaml:"tofu"`
	Timeout        time.Duration `yaml:"timeout"`
	DefaultPort    int           `yaml:"default_port"`
}

func ResolvePath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("ROSUP_CONFIG"); v != "" {
		return v
	}
	return defaultConfigPath
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var raw fileConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config path %s: %w", path, err)
	}

	cfg := Config{
		ConfigPath:          abs,
		DataDir:             raw.DataDir,
		StateDir:            raw.StateDir,
		PackageDir:          raw.PackageDir,
		BackupDir:           raw.BackupDir,
		BackupRetentionDays: raw.BackupRetentionDays,
		LockPath:            raw.LockPath,
		Ops:                 raw.Ops,
		SSH: SSHConfig{
			Username:       raw.SSH.Username,
			PrivateKeyPath: raw.SSH.PrivateKeyPath,
			KnownHostsPath: raw.SSH.KnownHostsPath,
			Timeout:        raw.SSH.Timeout,
			DefaultPort:    raw.SSH.DefaultPort,
		},
		Reconnect:      raw.Reconnect,
		Architectures:  raw.Architectures,
		Channel:        raw.Channel,
		UpgradeTimeout: raw.UpgradeTimeout,
	}
	if raw.SSH.TOFU != nil {
		cfg.SSH.TOFU = *raw.SSH.TOFU
	} else {
		cfg.SSH.TOFU = true
	}

	applyDefaults(&cfg)
	if err := resolvePaths(&cfg, filepath.Dir(abs)); err != nil {
		return nil, err
	}
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.BackupRetentionDays == 0 {
		cfg.BackupRetentionDays = defaultBackupRetentionDays
	}
	if cfg.SSH.Username == "" {
		cfg.SSH.Username = defaultSSHUsername
	}
	if cfg.SSH.DefaultPort == 0 {
		cfg.SSH.DefaultPort = defaultSSHPort
	}
	if cfg.Reconnect.Attempts == 0 {
		cfg.Reconnect.Attempts = defaultReconnectAttempts
	}
	if cfg.Reconnect.Timeout == 0 {
		cfg.Reconnect.Timeout = defaultReconnectTimeout
	}
	if cfg.Ops.InventoryFile == "" {
		cfg.Ops.InventoryFile = defaultOpsInventoryFile
	}
	if cfg.Ops.AuditDir == "" {
		cfg.Ops.AuditDir = defaultOpsAuditDir
	}
}

func resolvePaths(cfg *Config, base string) error {
	var err error
	if cfg.DataDir, err = resolvePath(base, cfg.DataDir); err != nil {
		return err
	}
	if cfg.StateDir, err = resolvePath(base, cfg.StateDir); err != nil {
		return err
	}
	if cfg.PackageDir, err = resolvePath(base, cfg.PackageDir); err != nil {
		return err
	}
	if cfg.BackupDir, err = resolvePath(base, cfg.BackupDir); err != nil {
		return err
	}
	if cfg.LockPath, err = resolvePath(base, cfg.LockPath); err != nil {
		return err
	}
	if cfg.Ops.Path, err = resolvePath(base, cfg.Ops.Path); err != nil {
		return err
	}
	if cfg.SSH.PrivateKeyPath, err = resolvePath(base, cfg.SSH.PrivateKeyPath); err != nil {
		return err
	}
	if cfg.SSH.KnownHostsPath, err = resolvePath(base, cfg.SSH.KnownHostsPath); err != nil {
		return err
	}
	return nil
}

func resolvePath(base, p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve path %s: %w", p, err)
	}
	return abs, nil
}

func validate(cfg *Config) error {
	if cfg.Channel != defaultChannel {
		return fmt.Errorf("channel must be %q, got %q", defaultChannel, cfg.Channel)
	}
	if len(cfg.Architectures) == 0 {
		return fmt.Errorf("architectures must be non-empty")
	}
	required := []struct {
		name  string
		value string
	}{
		{"data_dir", cfg.DataDir},
		{"state_dir", cfg.StateDir},
		{"package_dir", cfg.PackageDir},
		{"backup_dir", cfg.BackupDir},
		{"lock_path", cfg.LockPath},
		{"ops.path", cfg.Ops.Path},
		{"ops.remote", cfg.Ops.Remote},
		{"ssh.private_key_path", cfg.SSH.PrivateKeyPath},
		{"ssh.known_hosts_path", cfg.SSH.KnownHostsPath},
	}
	for _, field := range required {
		if field.value == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}
	return nil
}

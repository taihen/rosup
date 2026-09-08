package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/redact"
	"github.com/taihen/rosup/internal/state"
	"github.com/taihen/rosup/internal/transport"
)

const (
	cmdExport       = "/export hide-sensitive"
	backupTimeFmt   = "20060102T150405Z"
	remoteBackupExt = ".backup"
)

type Result struct {
	Export     string
	BackupPath string
}

func ExportAndBackup(ctx context.Context, cfg *config.Config, device string, client transport.Client) (Result, error) {
	if cfg == nil {
		return Result{}, errors.New("backup: nil config")
	}
	if client == nil {
		return Result{}, errors.New("backup: nil client")
	}

	dir, err := deviceBackupDir(cfg.BackupDir, device)
	if err != nil {
		return Result{}, err
	}

	out, err := client.Run(ctx, cmdExport)
	if err != nil {
		return Result{}, fmt.Errorf("backup: %s: %s: %w", device, cmdExport, err)
	}
	export := redact.String(out)

	if err := state.EnsureSecureDir(dir); err != nil {
		return Result{}, err
	}

	now := time.Now().UTC()
	name := "rosup-" + device + "-" + now.Format(backupTimeFmt)
	save := "/system backup save name=" + name + " dont-encrypt=yes"
	if _, err := client.Run(ctx, save); err != nil {
		return Result{}, fmt.Errorf("backup: %s: %s: %w", device, save, err)
	}

	remote := name + remoteBackupExt
	local := filepath.Join(dir, remote)
	if err := client.Download(ctx, remote, local); err != nil {
		_ = client.Remove(ctx, remote)
		return Result{}, fmt.Errorf("backup: %s: download %s: %w", device, remote, err)
	}
	if err := os.Chmod(local, 0o600); err != nil {
		_ = client.Remove(ctx, remote)
		return Result{}, fmt.Errorf("backup: %s: chmod %s: %w", device, local, err)
	}
	if err := client.Remove(ctx, remote); err != nil {
		return Result{}, fmt.Errorf("backup: %s: remove %s: %w", device, remote, err)
	}
	if err := prune(dir, device, now, cfg.BackupRetentionDays); err != nil {
		return Result{}, err
	}

	return Result{
		Export:     export,
		BackupPath: local,
	}, nil
}

func deviceBackupDir(backupDir, device string) (string, error) {
	if device == "" || device == "." || device == ".." || device != filepath.Base(device) {
		return "", fmt.Errorf("backup: invalid device name %q", device)
	}
	if backupDir == "" {
		return "", errors.New("backup: backup_dir is required")
	}
	base, err := filepath.Abs(backupDir)
	if err != nil {
		return "", fmt.Errorf("backup: resolve %s: %w", backupDir, err)
	}
	path := filepath.Join(base, device)
	if filepath.Dir(path) != base {
		return "", fmt.Errorf("backup: invalid device name %q", device)
	}
	return path, nil
}

func prune(dir, device string, now time.Time, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	cutoff := now.UTC().AddDate(0, 0, -retentionDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("backup: prune %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ts, ok := parseBackupTime(device, name)
		if !ok || !ts.Before(cutoff) {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("backup: prune %s: %w", path, err)
		}
	}
	return nil
}

func parseBackupTime(device, filename string) (time.Time, bool) {
	base := filepath.Base(filename)
	if filepath.Ext(base) != remoteBackupExt {
		return time.Time{}, false
	}
	stem := strings.TrimSuffix(base, remoteBackupExt)
	prefix := "rosup-" + device + "-"
	if !strings.HasPrefix(stem, prefix) {
		return time.Time{}, false
	}
	raw := strings.TrimPrefix(stem, prefix)
	if raw == "" {
		return time.Time{}, false
	}
	ts, err := time.Parse(backupTimeFmt, raw)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

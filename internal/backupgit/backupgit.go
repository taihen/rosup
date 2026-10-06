package backupgit

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/opsgit"
)

const (
	backupTimeFmt = "20060102T150405Z"
)

type Item struct {
	Device string
	Export string
	Time   time.Time
}

func Push(ctx context.Context, cfg *config.Config, items []Item) error {
	if cfg == nil {
		return errors.New("backupgit: nil config")
	}
	if len(items) == 0 {
		return nil
	}
	for _, item := range items {
		if err := validateName("device", item.Device); err != nil {
			return err
		}
		if item.Time.IsZero() {
			return fmt.Errorf("backupgit: item %s: time is required", item.Device)
		}
	}

	backupsDir := cfg.Ops.BackupsDir
	if backupsDir == "" {
		backupsDir = "backups"
	}
	if err := validateBackupsDir(backupsDir); err != nil {
		return err
	}
	commitItems := make([]opsgit.Item, 0, len(items))
	for _, item := range items {
		commitItems = append(commitItems, opsgit.Item{
			Rel:  filepath.ToSlash(relPath(backupsDir, item.Device, item.Time)),
			Body: item.Export,
		})
	}
	if err := opsgit.Commit(ctx, cfg, commitMessage(len(items)), commitItems); err != nil {
		return fmt.Errorf("backupgit: %w", err)
	}
	return nil
}

func relPath(backupsDir, device string, ts time.Time) string {
	t := ts.UTC()
	name := device + "-" + t.Format(backupTimeFmt) + ".rsc"
	return filepath.Join(backupsDir, device, t.Format("2006"), t.Format("01"), name)
}

func validateBackupsDir(backupsDir string) error {
	if backupsDir == "" || backupsDir == "." || backupsDir == ".." {
		return fmt.Errorf("backupgit: invalid backups_dir %q", backupsDir)
	}
	if filepath.IsAbs(backupsDir) {
		return fmt.Errorf("backupgit: backups_dir must be relative, got %q", backupsDir)
	}
	clean := filepath.Clean(backupsDir)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("backupgit: backups_dir escapes ops path: %q", backupsDir)
	}
	return nil
}

func commitMessage(n int) string {
	if n == 1 {
		return "backup: 1 device"
	}
	return fmt.Sprintf("backup: %d devices", n)
}

func validateName(kind, name string) error {
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) {
		return fmt.Errorf("backupgit: invalid %s %q", kind, name)
	}
	return nil
}

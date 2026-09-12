package backupgit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/redact"
)

const (
	authorName    = "rosup"
	authorEmail   = "rosup@localhost"
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

	auth, err := gitAuth(cfg)
	if err != nil {
		return err
	}

	repo, err := git.PlainOpen(cfg.Ops.Path)
	if err != nil {
		return fmt.Errorf("backupgit: open %s: %w", cfg.Ops.Path, err)
	}
	if err := checkRemote(repo, cfg.Ops.Remote); err != nil {
		return err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("backupgit: worktree: %w", err)
	}

	if err := wt.PullContext(ctx, &git.PullOptions{Auth: auth, RemoteName: "origin"}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("backupgit: pull: %w", err)
	}

	// Drop any pre-staged paths so Commit cannot piggyback inventory/ or other files.
	if err := wt.Reset(&git.ResetOptions{Mode: git.MixedReset}); err != nil {
		return fmt.Errorf("backupgit: reset index: %w", err)
	}

	backupsDir := cfg.Ops.BackupsDir
	if backupsDir == "" {
		backupsDir = "backups"
	}
	if err := validateBackupsDir(backupsDir); err != nil {
		return err
	}
	for _, item := range items {
		if err := stageItem(wt, cfg.Ops.Path, backupsDir, item); err != nil {
			return err
		}
	}

	sig := &object.Signature{
		Name:  authorName,
		Email: authorEmail,
		When:  time.Now().UTC(),
	}
	if _, err := wt.Commit(commitMessage(len(items)), &git.CommitOptions{
		Author:    sig,
		Committer: sig,
	}); err != nil && !errors.Is(err, git.ErrEmptyCommit) {
		return fmt.Errorf("backupgit: commit: %w", err)
	}

	if err := repo.PushContext(ctx, &git.PushOptions{Auth: auth, RemoteName: "origin"}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("backupgit: push: %w", err)
	}
	return nil
}

func stageItem(wt *git.Worktree, opsPath, backupsDir string, item Item) error {
	rel := relPath(backupsDir, item.Device, item.Time)
	path, err := containedPath(opsPath, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("backupgit: mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(redact.String(item.Export)), 0o644); err != nil {
		return fmt.Errorf("backupgit: write %s: %w", path, err)
	}
	addRel := filepath.ToSlash(rel)
	if _, err := wt.Add(addRel); err != nil {
		return fmt.Errorf("backupgit: add %s: %w", addRel, err)
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

func containedPath(opsPath, rel string) (string, error) {
	opsClean := filepath.Clean(opsPath)
	path := filepath.Clean(filepath.Join(opsClean, rel))
	relToOps, err := filepath.Rel(opsClean, path)
	if err != nil {
		return "", fmt.Errorf("backupgit: resolve path: %w", err)
	}
	if relToOps == ".." || strings.HasPrefix(relToOps, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("backupgit: path escapes ops: %s", rel)
	}
	return path, nil
}

func commitMessage(n int) string {
	if n == 1 {
		return "backup: 1 device"
	}
	return fmt.Sprintf("backup: %d devices", n)
}

func checkRemote(repo *git.Repository, want string) error {
	if want == "" {
		return errors.New("backupgit: ops.remote is required")
	}
	remote, err := repo.Remote("origin")
	if err != nil {
		return fmt.Errorf("backupgit: origin remote: %w", err)
	}
	for _, url := range remote.Config().URLs {
		if url == want {
			return nil
		}
	}
	return fmt.Errorf("backupgit: origin URLs %v do not match ops.remote %q", remote.Config().URLs, want)
}

func gitAuth(cfg *config.Config) (transport.AuthMethod, error) {
	keyPath := cfg.Ops.SSHPrivateKeyPath
	if needsSSHAuth(cfg.Ops.Remote) && keyPath == "" {
		return nil, errors.New("backupgit: ops.ssh_private_key_path is required for SSH remotes")
	}
	if keyPath == "" {
		return nil, nil
	}
	if cfg.Ops.GitKnownHostsPath == "" {
		return nil, errors.New("backupgit: ops.git_known_hosts_path is required when ops.ssh_private_key_path is set")
	}
	auth, err := gitssh.NewPublicKeysFromFile("git", keyPath, "")
	if err != nil {
		return nil, fmt.Errorf("backupgit: load ops git ssh key %s: %w", keyPath, err)
	}
	cb, err := gitssh.NewKnownHostsCallback(cfg.Ops.GitKnownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("backupgit: load ops git known_hosts %s: %w", cfg.Ops.GitKnownHostsPath, err)
	}
	auth.HostKeyCallback = cb
	return auth, nil
}

func needsSSHAuth(remote string) bool {
	if strings.HasPrefix(remote, "git@") || strings.HasPrefix(remote, "ssh://") {
		return true
	}
	if strings.Contains(remote, "://") {
		return false
	}
	at := strings.Index(remote, "@")
	colon := strings.LastIndex(remote, ":")
	return at > 0 && colon > at
}

func validateName(kind, name string) error {
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) {
		return fmt.Errorf("backupgit: invalid %s %q", kind, name)
	}
	return nil
}

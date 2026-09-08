package auditgit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/redact"
	"github.com/taihen/rosup/internal/state"
)

const (
	authorName  = "rosup"
	authorEmail = "rosup@localhost"
)

type Artifacts struct {
	Export string
	Result any
	Log    string
}

func Push(ctx context.Context, cfg *config.Config, job *state.DeviceJob, jobID string, artifacts Artifacts) error {
	if cfg == nil {
		return errors.New("auditgit: nil config")
	}
	if job == nil {
		return errors.New("auditgit: nil job")
	}
	if err := validateName("device", job.Device); err != nil {
		return err
	}
	if err := validateName("job id", jobID); err != nil {
		return err
	}

	repo, err := git.PlainOpen(cfg.Ops.Path)
	if err != nil {
		return fmt.Errorf("auditgit: open %s: %w", cfg.Ops.Path, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("auditgit: worktree: %w", err)
	}

	if err := wt.PullContext(ctx, &git.PullOptions{}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("auditgit: pull: %w", err)
	}

	auditDir := cfg.Ops.AuditDir
	if auditDir == "" {
		auditDir = "audit"
	}
	relDir := filepath.Join(auditDir, job.Device, jobID)
	dir := filepath.Join(cfg.Ops.Path, relDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("auditgit: mkdir %s: %w", dir, err)
	}

	resultJSON, err := json.MarshalIndent(artifacts.Result, "", "  ")
	if err != nil {
		return fmt.Errorf("auditgit: result.json: %w", err)
	}
	files := map[string]string{
		"export.rsc":  redact.String(artifacts.Export),
		"result.json": redact.String(string(resultJSON)) + "\n",
		"log.txt":     redact.String(artifacts.Log),
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return fmt.Errorf("auditgit: write %s: %w", path, err)
		}
		rel := filepath.ToSlash(filepath.Join(relDir, name))
		if _, err := wt.Add(rel); err != nil {
			return fmt.Errorf("auditgit: add %s: %w", rel, err)
		}
	}

	sig := &object.Signature{
		Name:  authorName,
		Email: authorEmail,
		When:  time.Now().UTC(),
	}
	if _, err := wt.Commit("audit: "+job.Device+" "+jobID, &git.CommitOptions{
		Author:    sig,
		Committer: sig,
	}); err != nil {
		return fmt.Errorf("auditgit: commit: %w", err)
	}

	if err := repo.PushContext(ctx, &git.PushOptions{}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("auditgit: push: %w", err)
	}
	return nil
}

func validateName(kind, name string) error {
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) {
		return fmt.Errorf("auditgit: invalid %s %q", kind, name)
	}
	return nil
}

package opsgit

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/redact"
)

// Item is a redacted file to write relative to cfg.Ops.Path.
type Item struct {
	Rel  string
	Body string
}

// Commit writes items, commits them, and pushes the resulting ops-git change.
// Errors are unprefixed so callers can wrap with their package name.
func Commit(ctx context.Context, cfg *config.Config, message string, items []Item) error {
	if cfg == nil {
		return errors.New("nil config")
	}

	paths := make([]string, len(items))
	rels := make([]string, len(items))
	for i, item := range items {
		path, rel, err := containedPath(cfg.Ops.Path, item.Rel)
		if err != nil {
			return err
		}
		paths[i] = path
		rels[i] = rel
	}

	auth, err := Auth(cfg)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	repo, err := Open(cfg)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	if err := CheckRemote(repo, cfg.Ops.Remote); err != nil {
		return fmt.Errorf("check remote: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}

	if err := wt.PullContext(ctx, &git.PullOptions{Auth: auth, RemoteName: "origin"}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("pull: %w", err)
	}

	// Reset the index before staging so an inventory or other pre-staged path
	// cannot piggyback on this transaction's commit.
	if err := wt.Reset(&git.ResetOptions{Mode: git.MixedReset}); err != nil {
		return fmt.Errorf("reset index: %w", err)
	}

	for i, item := range items {
		if err := os.MkdirAll(filepath.Dir(paths[i]), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", paths[i], err)
		}
		// Redact at the final write boundary so every caller gets the same
		// safety guarantee, including callers that construct Item directly.
		if err := os.WriteFile(paths[i], []byte(redact.String(item.Body)), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", paths[i], err)
		}
		if _, err := wt.Add(rels[i]); err != nil {
			return fmt.Errorf("add %s: %w", rels[i], err)
		}
	}

	sig := &object.Signature{
		Name:  "rosup",
		Email: "rosup@localhost",
		When:  time.Now().UTC(),
	}
	if _, err := wt.Commit(message, &git.CommitOptions{Author: sig, Committer: sig}); err != nil && !errors.Is(err, git.ErrEmptyCommit) {
		return fmt.Errorf("commit: %w", err)
	}

	if err := repo.PushContext(ctx, &git.PushOptions{Auth: auth, RemoteName: "origin"}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("push: %w", err)
	}
	return nil
}

func containedPath(opsPath, rel string) (string, string, error) {
	// Validate before joining and compare the cleaned result so item paths,
	// including encoded traversal attempts, cannot write outside the checkout.
	if err := validateRel(rel); err != nil {
		return "", "", err
	}

	opsClean := filepath.Clean(opsPath)
	path := filepath.Clean(filepath.Join(opsClean, filepath.FromSlash(rel)))
	relToOps, err := filepath.Rel(opsClean, path)
	if err != nil {
		return "", "", fmt.Errorf("resolve path: %w", err)
	}
	if relToOps == ".." || strings.HasPrefix(relToOps, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("path escapes ops: %s", rel)
	}
	return path, filepath.ToSlash(relToOps), nil
}

func validateRel(rel string) error {
	if rel == "" {
		return errors.New("invalid relative path: empty")
	}
	if strings.ContainsRune(rel, '\x00') {
		return fmt.Errorf("invalid relative path %q: NUL byte", rel)
	}
	if strings.Contains(rel, `\`) {
		return fmt.Errorf("invalid relative path %q: backslash is not allowed", rel)
	}
	if pathpkg.IsAbs(rel) || filepath.IsAbs(filepath.FromSlash(rel)) || filepath.VolumeName(filepath.FromSlash(rel)) != "" {
		return fmt.Errorf("invalid relative path %q: path must be relative", rel)
	}

	clean := pathpkg.Clean(rel)
	if clean != rel {
		return fmt.Errorf("invalid relative path %q: path must be cleaned", rel)
	}
	if clean == "." {
		return fmt.Errorf("invalid relative path %q: path must name a file", rel)
	}

	decoded, err := url.PathUnescape(rel)
	if err != nil {
		return fmt.Errorf("invalid relative path %q: bad escape: %w", rel, err)
	}
	if decoded != rel {
		if strings.ContainsAny(decoded, `/\`) || pathpkg.Clean(decoded) != decoded {
			return fmt.Errorf("invalid relative path %q: encoded traversal", rel)
		}
		for _, segment := range strings.Split(decoded, "/") {
			if segment == "." || segment == ".." {
				return fmt.Errorf("invalid relative path %q: encoded traversal", rel)
			}
		}
	}
	for _, segment := range strings.Split(rel, "/") {
		if segment == ".." {
			return fmt.Errorf("invalid relative path %q: path escapes ops", rel)
		}
	}
	return nil
}

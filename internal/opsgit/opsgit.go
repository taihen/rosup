package opsgit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"

	"github.com/taihen/rosup/internal/config"
)

// Auth returns SSH deploy-key auth for ops git when configured.
// Non-SSH remotes without a key return nil auth.
// Errors are unprefixed so callers can wrap with their package name.
func Auth(cfg *config.Config) (transport.AuthMethod, error) {
	if cfg == nil {
		return nil, errors.New("nil config")
	}
	keyPath := cfg.Ops.SSHPrivateKeyPath
	if needsSSHAuth(cfg.Ops.Remote) && keyPath == "" {
		return nil, errors.New("ops.ssh_private_key_path is required for SSH remotes")
	}
	if keyPath == "" {
		return nil, nil
	}
	if cfg.Ops.GitKnownHostsPath == "" {
		return nil, errors.New("ops.git_known_hosts_path is required when ops.ssh_private_key_path is set")
	}
	auth, err := gitssh.NewPublicKeysFromFile("git", keyPath, "")
	if err != nil {
		return nil, fmt.Errorf("load ops git ssh key %s: %w", keyPath, err)
	}
	cb, err := gitssh.NewKnownHostsCallback(cfg.Ops.GitKnownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("load ops git known_hosts %s: %w", cfg.Ops.GitKnownHostsPath, err)
	}
	auth.HostKeyCallback = cb
	return auth, nil
}

// Open opens the ops git repository at cfg.Ops.Path.
// Errors are unprefixed so callers can wrap with their package name.
func Open(cfg *config.Config) (*git.Repository, error) {
	if cfg == nil {
		return nil, errors.New("nil config")
	}
	repo, err := git.PlainOpen(cfg.Ops.Path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", cfg.Ops.Path, err)
	}
	return repo, nil
}

// CheckRemote ensures origin has exactly one URL and it equals ops.remote.
// Errors are unprefixed so callers can wrap with their package name.
func CheckRemote(repo *git.Repository, want string) error {
	if want == "" {
		return errors.New("ops.remote is required")
	}
	remote, err := repo.Remote("origin")
	if err != nil {
		return fmt.Errorf("origin remote: %w", err)
	}
	urls := remote.Config().URLs
	if len(urls) != 1 {
		return fmt.Errorf("origin must have exactly one URL matching ops.remote %q, got %v", want, urls)
	}
	if urls[0] != want {
		return fmt.Errorf("origin URL %q does not match ops.remote %q", urls[0], want)
	}
	return nil
}

func needsSSHAuth(remote string) bool {
	if strings.HasPrefix(remote, "git@") || strings.HasPrefix(remote, "ssh://") {
		return true
	}
	if strings.Contains(remote, "://") {
		return false
	}
	// SCP-like: [user@]host:path (no slash before the host/path colon)
	colon := strings.LastIndex(remote, ":")
	if colon <= 0 {
		return false
	}
	return !strings.Contains(remote[:colon], "/")
}

// PullResult is the outcome of a force-sync pull.
type PullResult struct {
	Hash            plumbing.Hash
	AlreadyUpToDate bool // true only if HEAD already equaled upstream and worktree had no local files outside the tip tree
}

// Pull fetches origin and hard-resets the checkout to the current branch's
// origin upstream tip. Remote always wins: local commits, dirty files, and
// untracked paths are discarded.
func Pull(ctx context.Context, cfg *config.Config) (PullResult, error) {
	if cfg == nil {
		return PullResult{}, errors.New("opsgit: nil config")
	}

	auth, err := Auth(cfg)
	if err != nil {
		return PullResult{}, fmt.Errorf("opsgit: %w", err)
	}
	repo, err := Open(cfg)
	if err != nil {
		return PullResult{}, fmt.Errorf("opsgit: %w", err)
	}
	if err := CheckRemote(repo, cfg.Ops.Remote); err != nil {
		return PullResult{}, fmt.Errorf("opsgit: %w", err)
	}

	if err := repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: "origin",
		RemoteURL:  cfg.Ops.Remote,
		Auth:       auth,
		Prune:      true,
	}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return PullResult{}, fmt.Errorf("opsgit: fetch: %w", err)
	}

	head, tip, err := resolveOriginUpstreamTip(repo)
	if err != nil {
		return PullResult{}, err
	}

	wt, err := repo.Worktree()
	if err != nil {
		return PullResult{}, fmt.Errorf("opsgit: worktree: %w", err)
	}
	status, err := wt.Status()
	if err != nil {
		return PullResult{}, fmt.Errorf("opsgit: status: %w", err)
	}
	already := head == tip && status.IsClean()
	if already {
		extra, err := hasFilesOutsideIndex(cfg.Ops.Path, repo)
		if err != nil {
			return PullResult{}, fmt.Errorf("opsgit: scan worktree: %w", err)
		}
		already = !extra
	}

	// Clean first so a clean failure does not leave HEAD already moved.
	if err := wt.Clean(&git.CleanOptions{Dir: true}); err != nil {
		return PullResult{}, fmt.Errorf("opsgit: clean: %w", err)
	}
	if err := wt.Reset(&git.ResetOptions{Commit: tip, Mode: git.HardReset}); err != nil {
		return PullResult{}, fmt.Errorf("opsgit: reset after clean: %w", err)
	}

	return PullResult{
		Hash:            tip,
		AlreadyUpToDate: already,
	}, nil
}

func resolveOriginUpstreamTip(repo *git.Repository) (plumbing.Hash, plumbing.Hash, error) {
	ref, err := repo.Head()
	if err != nil {
		return plumbing.ZeroHash, plumbing.ZeroHash, fmt.Errorf("opsgit: head: %w", err)
	}
	if !ref.Name().IsBranch() {
		return plumbing.ZeroHash, plumbing.ZeroHash, errors.New("opsgit: detached HEAD; checkout a branch that tracks origin")
	}
	branchName := ref.Name().Short()
	branch, err := repo.Branch(branchName)
	if err != nil {
		if errors.Is(err, git.ErrBranchNotFound) {
			return plumbing.ZeroHash, plumbing.ZeroHash, fmt.Errorf("opsgit: branch %s has no upstream configured", branchName)
		}
		return plumbing.ZeroHash, plumbing.ZeroHash, fmt.Errorf("opsgit: branch %s: %w", branchName, err)
	}
	if branch.Remote != "origin" {
		return plumbing.ZeroHash, plumbing.ZeroHash, fmt.Errorf("opsgit: branch %s tracks %q, want origin", branchName, branch.Remote)
	}
	if branch.Merge == "" {
		return plumbing.ZeroHash, plumbing.ZeroHash, fmt.Errorf("opsgit: branch %s has no upstream merge ref", branchName)
	}
	remoteRef := plumbing.NewRemoteReferenceName("origin", branch.Merge.Short())
	upstream, err := repo.Reference(remoteRef, true)
	if err != nil {
		return plumbing.ZeroHash, plumbing.ZeroHash, fmt.Errorf("opsgit: resolve %s: %w", remoteRef, err)
	}
	return ref.Hash(), upstream.Hash(), nil
}

func hasFilesOutsideIndex(opsPath string, repo *git.Repository) (bool, error) {
	idx, err := repo.Storer.Index()
	if err != nil {
		return false, err
	}
	indexed := make(map[string]struct{}, len(idx.Entries))
	for _, e := range idx.Entries {
		indexed[e.Name] = struct{}{}
	}

	var found bool
	err = filepath.WalkDir(opsPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(opsPath, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := indexed[rel]; ok {
			return nil
		}
		// Skip if the path vanished between listing and stat (race with other tools).
		if _, err := os.Lstat(path); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		found = true
		return fs.SkipAll
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return false, err
	}
	return found, nil
}

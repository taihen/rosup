package opsgit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	gitcfg "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/opsgit"
)

type gitEnv struct {
	bare string
	ops  string
	cfg  *config.Config
}

func TestPullAlreadyUpToDate(t *testing.T) {
	env := setupOpsRepo(t)
	before := headHash(t, env.ops)

	res, err := opsgit.Pull(context.Background(), env.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !res.AlreadyUpToDate {
		t.Fatal("want AlreadyUpToDate")
	}
	if res.Hash != before {
		t.Fatalf("hash %s, want %s", res.Hash, before)
	}
}

func TestPullDiscardsDirtyWorktree(t *testing.T) {
	env := setupOpsRepo(t)
	path := filepath.Join(env.ops, "inventory", "devices.yaml")
	if err := os.WriteFile(path, []byte("devices: [local-edit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := opsgit.Pull(context.Background(), env.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.AlreadyUpToDate {
		t.Fatal("dirty tree should not be AlreadyUpToDate")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "devices: []\n" {
		t.Fatalf("worktree content %q, want seed", got)
	}
}

func TestPullRemovesUntrackedFiles(t *testing.T) {
	env := setupOpsRepo(t)
	extra := filepath.Join(env.ops, "untracked.txt")
	if err := os.WriteFile(extra, []byte("nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := opsgit.Pull(context.Background(), env.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.AlreadyUpToDate {
		t.Fatal("untracked file should not be AlreadyUpToDate")
	}
	if _, err := os.Stat(extra); !os.IsNotExist(err) {
		t.Fatalf("untracked file still present: %v", err)
	}
}

func TestPullDiscardsUnpushedLocalCommit(t *testing.T) {
	env := setupOpsRepo(t)
	remoteTip := headHash(t, env.bare)
	writeCommit(t, env.ops, "inventory/devices.yaml", "devices: [local-only]\n", "local only")

	res, err := opsgit.Pull(context.Background(), env.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.AlreadyUpToDate {
		t.Fatal("want not AlreadyUpToDate after discarding local commit")
	}
	if res.Hash != remoteTip {
		t.Fatalf("hash %s, want remote tip %s", res.Hash, remoteTip)
	}
	got, err := os.ReadFile(filepath.Join(env.ops, "inventory", "devices.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "devices: []\n" {
		t.Fatalf("content %q after pull", got)
	}
	if headHash(t, env.ops) != remoteTip {
		t.Fatal("local HEAD should match remote tip")
	}
}

func TestPullFetchesRemoteCommits(t *testing.T) {
	env := setupOpsRepo(t)
	other := cloneWorktree(t, env.bare)
	writeCommit(t, other, "inventory/devices.yaml", "devices: [from-remote]\n", "remote update")
	push(t, other)

	res, err := opsgit.Pull(context.Background(), env.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.AlreadyUpToDate {
		t.Fatal("want pull of new remote commit")
	}
	got, err := os.ReadFile(filepath.Join(env.ops, "inventory", "devices.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "devices: [from-remote]\n" {
		t.Fatalf("content %q", got)
	}
	if res.Hash != headHash(t, env.bare) {
		t.Fatalf("hash %s, want bare %s", res.Hash, headHash(t, env.bare))
	}
}

func TestPullWrongRemoteErrors(t *testing.T) {
	env := setupOpsRepo(t)
	env.cfg.Ops.Remote = "/no/such/remote.git"
	_, err := opsgit.Pull(context.Background(), env.cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ops.remote") {
		t.Fatalf("want ops.remote in error, got %v", err)
	}
}

func TestPullNilConfigErrors(t *testing.T) {
	_, err := opsgit.Pull(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "opsgit:") {
		t.Fatalf("want opsgit wrap, got %v", err)
	}
}

func TestPullDetachedHEADErrors(t *testing.T) {
	env := setupOpsRepo(t)
	repo, err := git.PlainOpen(env.ops)
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.Checkout(&git.CheckoutOptions{Hash: head.Hash()}); err != nil {
		t.Fatal(err)
	}

	_, err = opsgit.Pull(context.Background(), env.cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "detached HEAD") {
		t.Fatalf("want detached HEAD error, got %v", err)
	}
}

func TestPullRejectsNonOriginUpstream(t *testing.T) {
	env := setupOpsRepo(t)
	repo, err := git.PlainOpen(env.ops)
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	name := head.Name().Short()
	if err := repo.DeleteBranch(name); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateBranch(&gitcfg.Branch{
		Name:   name,
		Remote: "upstream",
		Merge:  plumbing.NewBranchReferenceName(name),
	}); err != nil {
		t.Fatal(err)
	}

	_, err = opsgit.Pull(context.Background(), env.cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "want origin") {
		t.Fatalf("want origin tracking error, got %v", err)
	}
}

func TestPullMissingUpstreamErrors(t *testing.T) {
	env := setupOpsRepo(t)
	repo, err := git.PlainOpen(env.ops)
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteBranch(head.Name().Short()); err != nil {
		t.Fatal(err)
	}

	_, err = opsgit.Pull(context.Background(), env.cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no upstream") {
		t.Fatalf("want no upstream error, got %v", err)
	}
}

func TestPullRejectsMultiURLOrigin(t *testing.T) {
	env := setupOpsRepo(t)
	repo, err := git.PlainOpen(env.ops)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Remotes["origin"].URLs = append(cfg.Remotes["origin"].URLs, filepath.Join(t.TempDir(), "other.git"))
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}

	_, err = opsgit.Pull(context.Background(), env.cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "exactly one URL") {
		t.Fatalf("want multi-URL error, got %v", err)
	}
}

func TestPullFetchFailureLeavesHEAD(t *testing.T) {
	env := setupOpsRepo(t)
	before := headHash(t, env.ops)
	if err := os.RemoveAll(env.bare); err != nil {
		t.Fatal(err)
	}

	_, err := opsgit.Pull(context.Background(), env.cfg)
	if err == nil {
		t.Fatal("expected fetch error")
	}
	if !strings.Contains(err.Error(), "fetch") {
		t.Fatalf("want fetch error, got %v", err)
	}
	if headHash(t, env.ops) != before {
		t.Fatal("HEAD moved after failed fetch")
	}
}

func TestPullIgnoredFileNotAlreadyUpToDate(t *testing.T) {
	env := setupOpsRepo(t)
	if err := os.WriteFile(filepath.Join(env.ops, ".gitignore"), []byte("secret.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := git.PlainOpen(env.ops)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(".gitignore"); err != nil {
		t.Fatal(err)
	}
	sig := testSig()
	if _, err := wt.Commit("ignore", &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(&git.PushOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.ops, "secret.log"), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := opsgit.Pull(context.Background(), env.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.AlreadyUpToDate {
		t.Fatal("ignored local file should not be AlreadyUpToDate")
	}
	if _, err := os.Stat(filepath.Join(env.ops, "secret.log")); !os.IsNotExist(err) {
		t.Fatalf("ignored file should be discarded by hard-reset, stat=%v", err)
	}
}

func setupOpsRepo(t *testing.T) gitEnv {
	t.Helper()
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	bare := filepath.Join(root, "remote.git")
	ops := filepath.Join(root, "ops")

	initSeed(t, seed)
	if _, err := git.PlainClone(bare, true, &git.CloneOptions{URL: seed}); err != nil {
		t.Fatalf("bare clone: %v", err)
	}
	if _, err := git.PlainClone(ops, false, &git.CloneOptions{URL: bare}); err != nil {
		t.Fatalf("ops clone: %v", err)
	}

	return gitEnv{
		bare: bare,
		ops:  ops,
		cfg: &config.Config{
			Ops: config.OpsConfig{
				Path:   ops,
				Remote: bare,
			},
		},
	}
}

func initSeed(t *testing.T, dir string) {
	t.Helper()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "inventory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "inventory", "devices.yaml"), []byte("devices: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("inventory/devices.yaml"); err != nil {
		t.Fatal(err)
	}
	sig := testSig()
	if _, err := wt.Commit("seed", &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatal(err)
	}
}

func cloneWorktree(t *testing.T, bare string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clone")
	if _, err := git.PlainClone(dir, false, &git.CloneOptions{URL: bare}); err != nil {
		t.Fatalf("clone: %v", err)
	}
	return dir
}

func writeCommit(t *testing.T, worktree, relPath, contents, msg string) {
	t.Helper()
	path := filepath.Join(worktree, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := git.PlainOpen(worktree)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(filepath.ToSlash(relPath)); err != nil {
		t.Fatal(err)
	}
	sig := testSig()
	if _, err := wt.Commit(msg, &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatal(err)
	}
}

func push(t *testing.T, worktree string) {
	t.Helper()
	repo, err := git.PlainOpen(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(&git.PushOptions{}); err != nil {
		t.Fatal(err)
	}
}

func headHash(t *testing.T, path string) plumbing.Hash {
	t.Helper()
	repo, err := git.PlainOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	return ref.Hash()
}

func testSig() *object.Signature {
	return &object.Signature{
		Name:  "test",
		Email: "test@example.com",
		When:  time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC),
	}
}

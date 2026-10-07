package opsgit_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/opsgit"
	"golang.org/x/crypto/ssh"
)

func TestCommitWritesRedactedFilesAndUsesRosupIdentity(t *testing.T) {
	env := setupOpsRepo(t)

	err := opsgit.Commit(context.Background(), env.cfg, "audit: test job", []opsgit.Item{
		{Rel: "audit/golem/job-1/export.rsc", Body: "password=hunter2\n"},
		{Rel: "audit/golem/job-1/result.json", Body: "{\"status\":\"complete\"}\n"},
		{Rel: "audit/golem/job-1/log.txt", Body: "Authorization: Bearer abcdef\n"},
	})
	if err != nil {
		t.Fatal(err)
	}

	commit := headCommit(t, env.bare)
	if commit.Author.Name != "rosup" || commit.Author.Email != "rosup@localhost" {
		t.Fatalf("author %s <%s>", commit.Author.Name, commit.Author.Email)
	}
	if got := readCommitFile(t, commit, "audit/golem/job-1/export.rsc"); got != "password=***\n" {
		t.Fatalf("export %q", got)
	}
	if got := readCommitFile(t, commit, "audit/golem/job-1/log.txt"); got != "Authorization: Bearer ***\n" {
		t.Fatalf("log %q", got)
	}
	if got := readCommitFile(t, commit, "audit/golem/job-1/result.json"); got != "{\"status\":\"complete\"}\n" {
		t.Fatalf("result %q", got)
	}
}

func TestCommitEmptyItemsNoOp(t *testing.T) {
	env := setupOpsRepo(t)
	before := headCommit(t, env.bare).Hash

	if err := opsgit.Commit(context.Background(), env.cfg, "", nil); err != nil {
		t.Fatal(err)
	}
	if got := headCommit(t, env.bare).Hash; got != before {
		t.Fatalf("empty commit changed HEAD from %s to %s", before, got)
	}
}

func TestCommitUsesConfiguredSSHAuthOnFileRemote(t *testing.T) {
	env := setupOpsRepo(t)
	env.cfg.Ops.SSHPrivateKeyPath = writeOpsGitPrivateKey(t)
	env.cfg.Ops.GitKnownHostsPath = writeDummyKnownHosts(t)

	if err := opsgit.Commit(context.Background(), env.cfg, "audit: auth", []opsgit.Item{{
		Rel:  "audit/auth.txt",
		Body: "with configured auth\n",
	}}); err != nil {
		t.Fatal(err)
	}
	if got := readCommitFile(t, headCommit(t, env.bare), "audit/auth.txt"); got != "with configured auth\n" {
		t.Fatalf("auth transaction file %q", got)
	}
}

func TestCommitDropsPreStagedAndUnrelatedDirtyFiles(t *testing.T) {
	env := setupOpsRepo(t)
	inventory := filepath.Join(env.ops, "inventory", "devices.yaml")
	if err := os.WriteFile(inventory, []byte("devices: [{name: dirty}]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(env.ops, "unrelated.txt")
	if err := os.WriteFile(other, []byte("leave me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := openRepo(t, env.ops)
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("inventory/devices.yaml"); err != nil {
		t.Fatal(err)
	}

	if err := opsgit.Commit(context.Background(), env.cfg, "audit: only", []opsgit.Item{{
		Rel:  "audit/golem/job-1/export.rsc",
		Body: "# export\n",
	}}); err != nil {
		t.Fatal(err)
	}

	commit := headCommit(t, env.bare)
	for _, path := range commitPaths(t, commit) {
		if strings.HasPrefix(path, "inventory/") || path == "unrelated.txt" {
			t.Fatalf("committed unrelated path %q", path)
		}
		if path != "audit/golem/job-1/export.rsc" {
			t.Fatalf("committed unexpected path %q", path)
		}
	}
	got, err := os.ReadFile(inventory)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "devices: [{name: dirty}]\n" {
		t.Fatalf("inventory worktree overwritten: %q", got)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("unrelated worktree file missing: %v", err)
	}
}

func TestCommitPullsBeforeCommit(t *testing.T) {
	env := setupOpsRepo(t)
	other := cloneWorktree(t, env.bare)
	writeCommit(t, other, "audit/remote.txt", "from-remote\n", "remote audit")
	pushRepo(t, other)

	if err := opsgit.Commit(context.Background(), env.cfg, "audit: local", []opsgit.Item{{
		Rel:  "audit/local.txt",
		Body: "from-local\n",
	}}); err != nil {
		t.Fatal(err)
	}

	commit := headCommit(t, env.bare)
	if got := readCommitFile(t, commit, "audit/remote.txt"); got != "from-remote\n" {
		t.Fatalf("remote file %q", got)
	}
	if got := readCommitFile(t, commit, "audit/local.txt"); got != "from-local\n" {
		t.Fatalf("local file %q", got)
	}
}

func TestCommitNonFastForwardLeavesLocalCommit(t *testing.T) {
	env := setupOpsRepo(t)
	other := cloneWorktree(t, env.bare)
	writeCommit(t, other, "audit/remote.txt", "theirs\n", "remote diverged")
	pushRepo(t, other)
	writeCommit(t, env.ops, "audit/local.txt", "ours\n", "local diverged")

	before := headHash(t, env.ops)
	err := opsgit.Commit(context.Background(), env.cfg, "audit: failed", []opsgit.Item{{
		Rel:  "audit/job-1/export.rsc",
		Body: "# should not be written\n",
	}})
	if err == nil {
		t.Fatal("expected non-fast-forward error")
	}
	if !errors.Is(err, git.ErrNonFastForwardUpdate) {
		t.Fatalf("want ErrNonFastForwardUpdate, got %v", err)
	}
	if got := headHash(t, env.ops); got != before {
		t.Fatalf("local HEAD changed from %s to %s", before, got)
	}
	if got := readCommitFile(t, headCommit(t, env.ops), "audit/local.txt"); got != "ours\n" {
		t.Fatalf("local commit was rewritten: %q", got)
	}
}

func TestCommitContainedPathEnforcement(t *testing.T) {
	env := setupOpsRepo(t)
	for _, rel := range []string{"../x", "a/../../x", "/absolute", "..%2f"} {
		t.Run(rel, func(t *testing.T) {
			err := opsgit.Commit(context.Background(), env.cfg, "bad", []opsgit.Item{{Rel: rel, Body: "nope"}})
			if err == nil {
				t.Fatal("expected path validation error")
			}
			if !strings.Contains(err.Error(), "relative path") && !strings.Contains(err.Error(), "escapes") {
				t.Fatalf("want clear path error, got %v", err)
			}
		})
	}
}

func TestCommitSourceDoesNotExecGit(t *testing.T) {
	src, err := os.ReadFile("commit.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "os/exec") {
		t.Fatal("opsgit commit must not use os/exec")
	}
}

type gitEnv struct {
	bare string
	ops  string
	cfg  *config.Config
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
		cfg: &config.Config{Ops: config.OpsConfig{
			Path:   ops,
			Remote: bare,
		}},
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
	if _, err := wt.Commit("seed", &git.CommitOptions{Author: testSig(), Committer: testSig()}); err != nil {
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
	repo := openRepo(t, worktree)
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(filepath.ToSlash(relPath)); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit(msg, &git.CommitOptions{Author: testSig(), Committer: testSig()}); err != nil {
		t.Fatal(err)
	}
}

func pushRepo(t *testing.T, worktree string) {
	t.Helper()
	if err := openRepo(t, worktree).Push(&git.PushOptions{}); err != nil {
		t.Fatalf("push: %v", err)
	}
}

func openRepo(t *testing.T, path string) *git.Repository {
	t.Helper()
	repo, err := git.PlainOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func headHash(t *testing.T, path string) plumbing.Hash {
	t.Helper()
	ref, err := openRepo(t, path).Head()
	if err != nil {
		t.Fatal(err)
	}
	return ref.Hash()
}

func headCommit(t *testing.T, repoPath string) *object.Commit {
	t.Helper()
	commit, err := openRepo(t, repoPath).CommitObject(headHash(t, repoPath))
	if err != nil {
		t.Fatal(err)
	}
	return commit
}

func readCommitFile(t *testing.T, commit *object.Commit, path string) string {
	t.Helper()
	f, err := commit.File(path)
	if err != nil {
		t.Fatalf("file %s: %v", path, err)
	}
	got, err := f.Contents()
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func commitPaths(t *testing.T, commit *object.Commit) []string {
	t.Helper()
	parent, err := commit.Parent(0)
	if err != nil {
		t.Fatal(err)
	}
	patch, err := parent.Patch(commit)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	seen := map[string]struct{}{}
	add := func(path string) {
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	for _, fp := range patch.FilePatches() {
		from, to := fp.Files()
		if from != nil {
			add(from.Path())
		}
		if to != nil {
			add(to.Path())
		}
	}
	return paths
}

func testSig() *object.Signature {
	return &object.Signature{
		Name:  "test",
		Email: "test@example.com",
		When:  time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC),
	}
}

func writeOpsGitPrivateKey(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519_ops")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeDummyKnownHosts(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "git_known_hosts")
	line := "github.com " + string(ssh.MarshalAuthorizedKey(sshPub))
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

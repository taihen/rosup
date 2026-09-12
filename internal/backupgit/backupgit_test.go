package backupgit_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/taihen/rosup/internal/backupgit"
	"github.com/taihen/rosup/internal/config"
	"golang.org/x/crypto/ssh"
)

type gitEnv struct {
	bare string
	ops  string
	cfg  *config.Config
}

func TestPushWritesDatedBackupPathAndRedacts(t *testing.T) {
	env := setupOpsRepo(t)
	ts := time.Date(2026, 9, 12, 10, 30, 0, 0, time.UTC)

	err := backupgit.Push(context.Background(), env.cfg, []backupgit.Item{{
		Device: "golem",
		Export: "password=hunter2\n/ip address add address=192.0.2.1/24\n",
		Time:   ts,
	}})
	if err != nil {
		t.Fatal(err)
	}

	commit := headCommit(t, env.bare)
	if commit.Author.Name != "rosup" || commit.Author.Email != "rosup@localhost" {
		t.Fatalf("author %s <%s>", commit.Author.Name, commit.Author.Email)
	}
	if commit.Message != "backup: 1 device" {
		t.Fatalf("message %q", commit.Message)
	}

	wantPath := "backups/golem/2026/09/golem-20260912T103000Z.rsc"
	export := readCommitFile(t, commit, wantPath)
	if strings.Contains(export, "hunter2") {
		t.Fatalf("export leaked secret: %s", export)
	}
	if !strings.Contains(export, "password=***") {
		t.Fatalf("export not redacted: %s", export)
	}
}

func TestPushEmptyItemsNoOp(t *testing.T) {
	env := setupOpsRepo(t)
	before := headCommit(t, env.bare).Hash

	if err := backupgit.Push(context.Background(), env.cfg, nil); err != nil {
		t.Fatal(err)
	}
	if headCommit(t, env.bare).Hash != before {
		t.Fatal("empty push created a commit")
	}
}

func TestPushCommitsBackupsOnlyWhenInventoryDirty(t *testing.T) {
	env := setupOpsRepo(t)
	inv := filepath.Join(env.ops, "inventory", "devices.yaml")
	if err := os.WriteFile(inv, []byte("devices: [{name: dirty}]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ts := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	if err := backupgit.Push(context.Background(), env.cfg, []backupgit.Item{{
		Device: "golem",
		Export: "# export\n",
		Time:   ts,
	}}); err != nil {
		t.Fatal(err)
	}

	commit := headCommit(t, env.bare)
	for _, path := range commitPaths(t, commit) {
		if strings.HasPrefix(path, "inventory/") {
			t.Fatalf("committed inventory path %q", path)
		}
		if strings.HasPrefix(path, "audit/") {
			t.Fatalf("committed audit path %q", path)
		}
		if !strings.HasPrefix(path, "backups/") {
			t.Fatalf("committed non-backups path %q", path)
		}
	}
}

func TestPushIgnoresPreStagedInventory(t *testing.T) {
	env := setupOpsRepo(t)
	inv := filepath.Join(env.ops, "inventory", "devices.yaml")
	if err := os.WriteFile(inv, []byte("devices: [{name: staged}]\n"), 0o644); err != nil {
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

	ts := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	if err := backupgit.Push(context.Background(), env.cfg, []backupgit.Item{{
		Device: "golem",
		Export: "# export\n",
		Time:   ts,
	}}); err != nil {
		t.Fatal(err)
	}

	commit := headCommit(t, env.bare)
	for _, path := range commitPaths(t, commit) {
		if strings.HasPrefix(path, "inventory/") {
			t.Fatalf("committed staged inventory path %q", path)
		}
		if !strings.HasPrefix(path, "backups/") {
			t.Fatalf("committed non-backups path %q", path)
		}
	}
	got, err := os.ReadFile(inv)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "devices: [{name: staged}]\n" {
		t.Fatalf("inventory worktree overwritten: %q", got)
	}
}

func TestPushRejectsEscapingBackupsDir(t *testing.T) {
	env := setupOpsRepo(t)
	env.cfg.Ops.BackupsDir = "../outside"
	ts := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	err := backupgit.Push(context.Background(), env.cfg, []backupgit.Item{{
		Device: "golem",
		Export: "#\n",
		Time:   ts,
	}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "backups_dir") && !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("got %v", err)
	}
}

func TestPushErrorsWhenGitSSHKeyMissing(t *testing.T) {
	env := setupOpsRepo(t)
	keyPath := filepath.Join(t.TempDir(), "missing-ops-key")
	env.cfg.Ops.SSHPrivateKeyPath = keyPath
	env.cfg.Ops.GitKnownHostsPath = writeDummyKnownHosts(t)

	err := backupgit.Push(context.Background(), env.cfg, []backupgit.Item{{
		Device: "golem",
		Export: "#\n",
		Time:   time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC),
	}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "backupgit:") {
		t.Fatalf("want backupgit wrap, got %v", err)
	}
	if !strings.Contains(err.Error(), keyPath) {
		t.Fatalf("want key path in error, got %v", err)
	}
}

func TestPushSucceedsWithGitSSHAuthOnFileRemote(t *testing.T) {
	env := setupOpsRepo(t)
	env.cfg.Ops.SSHPrivateKeyPath = writeOpsGitPrivateKey(t)
	env.cfg.Ops.GitKnownHostsPath = writeDummyKnownHosts(t)

	ts := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	if err := backupgit.Push(context.Background(), env.cfg, []backupgit.Item{{
		Device: "golem",
		Export: "# with-auth\n",
		Time:   ts,
	}}); err != nil {
		t.Fatal(err)
	}

	commit := headCommit(t, env.bare)
	if readCommitFile(t, commit, "backups/golem/2026/09/golem-20260912T100000Z.rsc") != "# with-auth\n" {
		t.Fatal("missing backup after file-protocol push with ssh auth configured")
	}
}

func TestPushErrorsWhenGitKnownHostsPathEmpty(t *testing.T) {
	env := setupOpsRepo(t)
	env.cfg.Ops.SSHPrivateKeyPath = writeOpsGitPrivateKey(t)
	env.cfg.Ops.GitKnownHostsPath = ""

	err := backupgit.Push(context.Background(), env.cfg, []backupgit.Item{{
		Device: "golem",
		Export: "#\n",
		Time:   time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC),
	}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "backupgit:") {
		t.Fatalf("want backupgit wrap, got %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "known_hosts") {
		t.Fatalf("want known_hosts in error, got %v", err)
	}
}

func TestPushRejectsUnsafeDeviceNames(t *testing.T) {
	env := setupOpsRepo(t)
	ts := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	for _, name := range []string{"", ".", "..", "a/b"} {
		err := backupgit.Push(context.Background(), env.cfg, []backupgit.Item{{
			Device: name,
			Export: "#\n",
			Time:   ts,
		}})
		if err == nil {
			t.Fatalf("expected error for %q", name)
		}
		if !strings.Contains(err.Error(), "backupgit:") {
			t.Fatalf("want backupgit wrap, got %v", err)
		}
	}
}

func TestPushRequiresTime(t *testing.T) {
	env := setupOpsRepo(t)
	err := backupgit.Push(context.Background(), env.cfg, []backupgit.Item{{
		Device: "golem",
		Export: "#\n",
	}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "time") {
		t.Fatalf("got %v", err)
	}
}

func TestPushPullsBeforeCommit(t *testing.T) {
	env := setupOpsRepo(t)
	other := cloneWorktree(t, env.bare)
	writeCommit(t, other, "backups/other-device/note.txt", "from-remote\n", "remote backup")
	pushRepo(t, other)

	ts := time.Date(2026, 9, 12, 11, 0, 0, 0, time.UTC)
	if err := backupgit.Push(context.Background(), env.cfg, []backupgit.Item{{
		Device: "golem",
		Export: "# local\n",
		Time:   ts,
	}}); err != nil {
		t.Fatal(err)
	}

	commit := headCommit(t, env.bare)
	if readCommitFile(t, commit, "backups/other-device/note.txt") != "from-remote\n" {
		t.Fatal("missing remote file after pull")
	}
	if readCommitFile(t, commit, "backups/golem/2026/09/golem-20260912T110000Z.rsc") != "# local\n" {
		t.Fatal("missing local backup after ff pull")
	}
}

func TestPushMustNotUseOsExec(t *testing.T) {
	src, err := os.ReadFile("backupgit.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "os/exec") {
		t.Fatal("backupgit must not use os/exec")
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
				Path:       ops,
				Remote:     bare,
				BackupsDir: "backups",
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
	if err := os.MkdirAll(filepath.Join(dir, "backups"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "inventory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "backups", ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "inventory", "devices.yaml"), []byte("devices: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("backups/.gitkeep"); err != nil {
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
	repo := openRepo(t, worktree)
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(relPath); err != nil {
		t.Fatal(err)
	}
	sig := testSig()
	if _, err := wt.Commit(msg, &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatal(err)
	}
}

func pushRepo(t *testing.T, worktree string) {
	t.Helper()
	repo := openRepo(t, worktree)
	if err := repo.Push(&git.PushOptions{}); err != nil {
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

func headCommit(t *testing.T, repoPath string) *object.Commit {
	t.Helper()
	repo := openRepo(t, repoPath)
	ref, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	commit, err := repo.CommitObject(ref.Hash())
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
		Name:  "tester",
		Email: "tester@localhost",
		When:  time.Date(2026, 9, 8, 11, 0, 0, 0, time.UTC),
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

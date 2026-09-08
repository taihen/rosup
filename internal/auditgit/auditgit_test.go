package auditgit_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/taihen/rosup/internal/auditgit"
	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/state"
	"golang.org/x/crypto/ssh"
)

type gitEnv struct {
	bare     string
	ops      string
	stateDir string
	cfg      *config.Config
}

func TestPushWritesRedactedAuditAndPushes(t *testing.T) {
	env := setupOpsRepo(t)
	job := completeJob()
	if err := state.Save(env.stateDir, job); err != nil {
		t.Fatal(err)
	}

	err := auditgit.Push(context.Background(), env.cfg, job, "job-1", auditgit.Artifacts{
		Export: "password=hunter2\n/ip address add address=192.0.2.1/24\n",
		Result: map[string]string{"device": "golem", "status": "complete"},
		Log:    "Authorization: Bearer abcdef\nupgraded ok\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	commit := headCommit(t, env.bare)
	if commit.Author.Name != "rosup" || commit.Author.Email != "rosup@localhost" {
		t.Fatalf("author %s <%s>", commit.Author.Name, commit.Author.Email)
	}

	export := readCommitFile(t, commit, "audit/golem/job-1/export.rsc")
	if strings.Contains(export, "hunter2") {
		t.Fatalf("export leaked secret: %s", export)
	}
	if !strings.Contains(export, "password=***") {
		t.Fatalf("export not redacted: %s", export)
	}

	logTxt := readCommitFile(t, commit, "audit/golem/job-1/log.txt")
	if strings.Contains(logTxt, "abcdef") {
		t.Fatalf("log leaked secret: %s", logTxt)
	}
	if !strings.Contains(logTxt, "Authorization: Bearer ***") {
		t.Fatalf("log not redacted: %s", logTxt)
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(readCommitFile(t, commit, "audit/golem/job-1/result.json")), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["device"] != "golem" || parsed["status"] != "complete" {
		t.Fatalf("result.json %+v", parsed)
	}

	loaded, err := state.Load(env.stateDir, job.Device)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != state.StatusComplete {
		t.Fatalf("status %q", loaded.Status)
	}
}

func TestPushCommitsAuditOnlyWhenInventoryDirty(t *testing.T) {
	env := setupOpsRepo(t)
	inv := filepath.Join(env.ops, "inventory", "devices.yaml")
	if err := os.WriteFile(inv, []byte("devices: [{name: dirty}]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	job := completeJob()
	if err := auditgit.Push(context.Background(), env.cfg, job, "job-1", auditgit.Artifacts{
		Export: "# export\n",
		Result: map[string]string{"ok": "true"},
		Log:    "done\n",
	}); err != nil {
		t.Fatal(err)
	}

	commit := headCommit(t, env.bare)
	for _, path := range commitPaths(t, commit) {
		if strings.HasPrefix(path, "inventory/") {
			t.Fatalf("committed inventory path %q", path)
		}
		if !strings.HasPrefix(path, "audit/") {
			t.Fatalf("committed non-audit path %q", path)
		}
	}

	got, err := os.ReadFile(inv)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "devices: [{name: dirty}]\n" {
		t.Fatalf("inventory worktree overwritten: %q", got)
	}

	parent, err := commit.Parent(0)
	if err != nil {
		t.Fatal(err)
	}
	if readCommitFile(t, parent, "inventory/devices.yaml") != "devices: []\n" {
		t.Fatal("HEAD inventory content changed")
	}
}

func TestPushDoesNotAddBackupFileInOpsPath(t *testing.T) {
	env := setupOpsRepo(t)
	backup := filepath.Join(env.ops, "foo.backup")
	if err := os.WriteFile(backup, []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}

	job := completeJob()
	if err := auditgit.Push(context.Background(), env.cfg, job, "job-1", auditgit.Artifacts{
		Export: "#\n",
		Result: map[string]string{"ok": "true"},
		Log:    "ok\n",
	}); err != nil {
		t.Fatal(err)
	}

	commit := headCommit(t, env.bare)
	for _, path := range commitPaths(t, commit) {
		if strings.HasSuffix(path, ".backup") {
			t.Fatalf("committed backup %q", path)
		}
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("ops foo.backup missing: %v", err)
	}
}

func TestPushFastForwardPullsRemoteAhead(t *testing.T) {
	env := setupOpsRepo(t)
	other := cloneWorktree(t, env.bare)
	writeCommit(t, other, "audit/other-device/note.txt", "from-remote\n", "remote audit")
	pushRepo(t, other)

	job := completeJob()
	if err := auditgit.Push(context.Background(), env.cfg, job, "job-1", auditgit.Artifacts{
		Export: "# local\n",
		Result: map[string]string{"ok": "true"},
		Log:    "ok\n",
	}); err != nil {
		t.Fatal(err)
	}

	commit := headCommit(t, env.bare)
	if readCommitFile(t, commit, "audit/other-device/note.txt") != "from-remote\n" {
		t.Fatal("missing fast-forwarded remote file")
	}
	if readCommitFile(t, commit, "audit/golem/job-1/export.rsc") != "# local\n" {
		t.Fatal("missing local audit after ff pull")
	}
}

func TestPushNonFastForwardLeavesCompleteJob(t *testing.T) {
	env := setupOpsRepo(t)
	job := completeJob()
	if err := state.Save(env.stateDir, job); err != nil {
		t.Fatal(err)
	}

	other := cloneWorktree(t, env.bare)
	writeCommit(t, other, "audit/remote.txt", "theirs\n", "remote diverged")
	pushRepo(t, other)
	writeCommit(t, env.ops, "audit/local.txt", "ours\n", "local diverged")

	err := auditgit.Push(context.Background(), env.cfg, job, "job-1", auditgit.Artifacts{
		Export: "#\n",
		Result: map[string]string{"ok": "true"},
		Log:    "ok\n",
	})
	if err == nil {
		t.Fatal("expected non-fast-forward pull error")
	}
	if !errors.Is(err, git.ErrNonFastForwardUpdate) {
		t.Fatalf("want ErrNonFastForwardUpdate, got %v", err)
	}

	if job.Status != state.StatusComplete {
		t.Fatalf("in-memory status %q", job.Status)
	}
	loaded, err := state.Load(env.stateDir, job.Device)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != state.StatusComplete {
		t.Fatalf("on-disk status %q", loaded.Status)
	}

	repo := openRepo(t, env.ops)
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if readCommitFile(t, commit, "audit/local.txt") != "ours\n" {
		t.Fatal("local diverged commit was rewritten")
	}
}

func TestPushRejectsUnsafeNames(t *testing.T) {
	env := setupOpsRepo(t)
	job := completeJob()

	for _, name := range []string{"../x", "foo/bar", "", ".", ".."} {
		caseName := name
		if caseName == "" {
			caseName = "empty"
		}
		t.Run("device/"+caseName, func(t *testing.T) {
			bad := *job
			bad.Device = name
			err := auditgit.Push(context.Background(), env.cfg, &bad, "job-1", auditgit.Artifacts{})
			if err == nil {
				t.Fatal("expected error")
			}
		})
		t.Run("job/"+caseName, func(t *testing.T) {
			err := auditgit.Push(context.Background(), env.cfg, job, name, auditgit.Artifacts{})
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSourceDoesNotExecGit(t *testing.T) {
	src, err := os.ReadFile("auditgit.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "os/exec") {
		t.Fatal("auditgit must not use os/exec")
	}
}

func TestPushErrorsWhenGitSSHKeyMissing(t *testing.T) {
	env := setupOpsRepo(t)
	keyPath := filepath.Join(t.TempDir(), "missing-ops-key")
	env.cfg.Ops.SSHPrivateKeyPath = keyPath
	env.cfg.Ops.GitKnownHostsPath = writeDummyKnownHosts(t)

	err := auditgit.Push(context.Background(), env.cfg, completeJob(), "job-1", auditgit.Artifacts{
		Export: "#\n",
		Result: map[string]string{"ok": "true"},
		Log:    "ok\n",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "auditgit:") {
		t.Fatalf("want auditgit wrap, got %v", err)
	}
	if !strings.Contains(err.Error(), keyPath) {
		t.Fatalf("want key path in error, got %v", err)
	}
}

func TestPushSucceedsWithGitSSHAuthOnFileRemote(t *testing.T) {
	env := setupOpsRepo(t)
	env.cfg.Ops.SSHPrivateKeyPath = writeOpsGitPrivateKey(t)
	env.cfg.Ops.GitKnownHostsPath = writeDummyKnownHosts(t)

	if err := auditgit.Push(context.Background(), env.cfg, completeJob(), "job-1", auditgit.Artifacts{
		Export: "# with-auth\n",
		Result: map[string]string{"ok": "true"},
		Log:    "ok\n",
	}); err != nil {
		t.Fatal(err)
	}

	commit := headCommit(t, env.bare)
	if readCommitFile(t, commit, "audit/golem/job-1/export.rsc") != "# with-auth\n" {
		t.Fatal("missing audit after file-protocol push with ssh auth configured")
	}
}

func TestPushErrorsWhenGitKnownHostsPathEmpty(t *testing.T) {
	env := setupOpsRepo(t)
	env.cfg.Ops.SSHPrivateKeyPath = writeOpsGitPrivateKey(t)
	env.cfg.Ops.GitKnownHostsPath = ""

	err := auditgit.Push(context.Background(), env.cfg, completeJob(), "job-1", auditgit.Artifacts{
		Export: "#\n",
		Result: map[string]string{"ok": "true"},
		Log:    "ok\n",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "auditgit:") {
		t.Fatalf("want auditgit wrap, got %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "known_hosts") {
		t.Fatalf("want known_hosts in error, got %v", err)
	}
}

func completeJob() *state.DeviceJob {
	return &state.DeviceJob{
		Device:    "golem",
		Release:   "6.49.21",
		Group:     "core-a",
		Stage:     "BACKUP",
		Status:    state.StatusComplete,
		UpdatedAt: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
	}
}

func setupOpsRepo(t *testing.T) gitEnv {
	t.Helper()
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	bare := filepath.Join(root, "remote.git")
	ops := filepath.Join(root, "ops")
	stateDir := filepath.Join(root, "state")

	initSeed(t, seed)
	if _, err := git.PlainClone(bare, true, &git.CloneOptions{URL: seed}); err != nil {
		t.Fatalf("bare clone: %v", err)
	}
	if _, err := git.PlainClone(ops, false, &git.CloneOptions{URL: bare}); err != nil {
		t.Fatalf("ops clone: %v", err)
	}

	return gitEnv{
		bare:     bare,
		ops:      ops,
		stateDir: stateDir,
		cfg: &config.Config{
			StateDir: stateDir,
			Ops: config.OpsConfig{
				Path:     ops,
				Remote:   bare,
				AuditDir: "audit",
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
	if err := os.MkdirAll(filepath.Join(dir, "audit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "inventory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audit", ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "inventory", "devices.yaml"), []byte("devices: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("audit/.gitkeep"); err != nil {
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

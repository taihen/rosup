package state_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/taihen/rosup/internal/state"
)

func sampleJob(status string) *state.DeviceJob {
	return &state.DeviceJob{
		Device:    "golem",
		Release:   "6.49.21",
		Group:     "core-a",
		Stage:     "DISCOVER",
		Status:    status,
		UpdatedAt: time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC),
		LastError: "boom",
		Attempt:   2,
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := sampleJob(state.StatusPending)
	if err := state.Save(dir, want); err != nil {
		t.Fatal(err)
	}

	got, err := state.Load(dir, want.Device)
	if err != nil {
		t.Fatal(err)
	}
	if got.Device != want.Device || got.Release != want.Release || got.Group != want.Group {
		t.Fatalf("identity: %+v", got)
	}
	if got.Stage != want.Stage || got.Status != want.Status || got.Attempt != want.Attempt {
		t.Fatalf("progress: %+v", got)
	}
	if got.LastError != want.LastError {
		t.Fatalf("LastError %q", got.LastError)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("UpdatedAt %v want %v", got.UpdatedAt, want.UpdatedAt)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := state.Load(t.TempDir(), "golem")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want os.ErrNotExist, got %v", err)
	}
}

func TestLoadCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "golem.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()

	_, err := state.Load(dir, "golem")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMarkStartedSkipsComplete(t *testing.T) {
	dir := t.TempDir()
	job := sampleJob(state.StatusComplete)
	job.Stage = "COMPLETE"
	if err := state.Save(dir, job); err != nil {
		t.Fatal(err)
	}

	if err := state.MarkStarted(dir, job); err != nil {
		t.Fatal(err)
	}
	if job.Status != state.StatusComplete {
		t.Fatalf("in-memory status %q", job.Status)
	}

	got, err := state.Load(dir, job.Device)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusComplete {
		t.Fatalf("on-disk status %q", got.Status)
	}
	if got.Stage != "COMPLETE" {
		t.Fatalf("stage %q", got.Stage)
	}
	if !got.UpdatedAt.Equal(job.UpdatedAt) {
		t.Fatalf("UpdatedAt changed to %v", got.UpdatedAt)
	}
	if got.Attempt != 2 {
		t.Fatalf("attempt %d", got.Attempt)
	}
}

func TestMarkStartedSetsInProgress(t *testing.T) {
	dir := t.TempDir()
	job := sampleJob(state.StatusPending)
	if err := state.Save(dir, job); err != nil {
		t.Fatal(err)
	}

	if err := state.MarkStarted(dir, job); err != nil {
		t.Fatal(err)
	}
	if job.Status != state.StatusInProgress {
		t.Fatalf("in-memory status %q", job.Status)
	}

	got, err := state.Load(dir, job.Device)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusInProgress {
		t.Fatalf("on-disk status %q", got.Status)
	}
}

func TestAdvanceWritesAtomically(t *testing.T) {
	dir := t.TempDir()
	job := sampleJob(state.StatusInProgress)
	if err := state.Save(dir, job); err != nil {
		t.Fatal(err)
	}

	if err := state.Advance(dir, job, "PREFLIGHT"); err != nil {
		t.Fatal(err)
	}
	if job.Stage != "PREFLIGHT" {
		t.Fatalf("in-memory stage %q", job.Stage)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "golem.json" {
		t.Fatalf("dir after advance: %v", names(entries))
	}

	data, err := os.ReadFile(filepath.Join(dir, "golem.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed state.DeviceJob
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("partial write: %v", err)
	}
	if parsed.Stage != "PREFLIGHT" {
		t.Fatalf("stage %q", parsed.Stage)
	}
}

func TestSaveCreatesStateDir0700AndFile0600(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "state")
	job := sampleJob(state.StatusPending)
	if err := state.Save(dir, job); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("state dir perm %04o", perm)
	}

	fi, err = os.Stat(filepath.Join(dir, "golem.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("state file perm %04o", perm)
	}
}

func TestEnsureSecureDir0700(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"state", "backup", "ssh"} {
		dir := filepath.Join(root, name)
		if err := state.EnsureSecureDir(dir); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != 0o700 {
			t.Fatalf("%s perm %04o", name, perm)
		}
	}
}

func TestEnsureSecureDirTightensExisting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "backup")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := state.EnsureSecureDir(dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("perm %04o", perm)
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

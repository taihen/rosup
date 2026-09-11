package state_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	if job.LastError != "" {
		t.Fatalf("in-memory LastError %q", job.LastError)
	}

	got, err := state.Load(dir, job.Device)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusInProgress {
		t.Fatalf("on-disk status %q", got.Status)
	}
	if got.LastError != "" {
		t.Fatalf("on-disk LastError %q", got.LastError)
	}
}

func TestMarkStartedClearsLastError(t *testing.T) {
	dir := t.TempDir()
	job := sampleJob(state.StatusFailed)
	job.LastError = "ssh down"
	if err := state.Save(dir, job); err != nil {
		t.Fatal(err)
	}

	if err := state.MarkStarted(dir, job); err != nil {
		t.Fatal(err)
	}
	if job.LastError != "" {
		t.Fatalf("in-memory LastError %q", job.LastError)
	}

	got, err := state.Load(dir, job.Device)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusInProgress {
		t.Fatalf("status %q", got.Status)
	}
	if got.LastError != "" {
		t.Fatalf("LastError %q", got.LastError)
	}
}

func TestMarkStartedDoesNotOverwriteCompleteOnDisk(t *testing.T) {
	dir := t.TempDir()
	complete := sampleJob(state.StatusComplete)
	complete.Stage = "COMPLETE"
	if err := state.Save(dir, complete); err != nil {
		t.Fatal(err)
	}

	pending := &state.DeviceJob{Device: "golem", Status: state.StatusPending}
	err := state.MarkStarted(dir, pending)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, state.ErrJobComplete) {
		t.Fatalf("want ErrJobComplete, got %v", err)
	}

	got, err := state.Load(dir, "golem")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusComplete {
		t.Fatalf("on-disk status %q", got.Status)
	}
	if got.Stage != "COMPLETE" {
		t.Fatalf("stage %q", got.Stage)
	}
}

func TestSaveRejectsCompleteToInProgress(t *testing.T) {
	dir := t.TempDir()
	complete := sampleJob(state.StatusComplete)
	if err := state.Save(dir, complete); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "golem.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	next := sampleJob(state.StatusInProgress)
	err = state.Save(dir, next)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, state.ErrJobComplete) {
		t.Fatalf("want ErrJobComplete, got %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("disk changed:\nbefore %s\nafter %s", before, after)
	}
}

func TestSaveAllowsTransitionToComplete(t *testing.T) {
	dir := t.TempDir()
	job := sampleJob(state.StatusInProgress)
	if err := state.Save(dir, job); err != nil {
		t.Fatal(err)
	}

	job.Status = state.StatusComplete
	job.Stage = "COMPLETE"
	if err := state.Save(dir, job); err != nil {
		t.Fatal(err)
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
}

func TestSaveAllowsNewReleaseAfterComplete(t *testing.T) {
	dir := t.TempDir()
	complete := sampleJob(state.StatusComplete)
	complete.Release = "6.49.18"
	complete.Stage = "COMPLETE"
	if err := state.Save(dir, complete); err != nil {
		t.Fatal(err)
	}

	next := sampleJob(state.StatusInProgress)
	next.Release = "6.49.21"
	next.Stage = "DISCOVER"
	if err := state.Save(dir, next); err != nil {
		t.Fatal(err)
	}

	got, err := state.Load(dir, "golem")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusInProgress || got.Release != "6.49.21" {
		t.Fatalf("got %+v", got)
	}
}

func TestListSkipsCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	ok := sampleJob(state.StatusComplete)
	if err := state.Save(dir, ok); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := state.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	var sawOK, sawCorrupt bool
	for _, j := range got {
		switch j.Device {
		case "golem":
			sawOK = true
		case "broken":
			sawCorrupt = j.Status == "corrupt"
		}
	}
	if !sawOK || !sawCorrupt {
		t.Fatalf("%+v", got)
	}
}

func TestLoadRejectsDeviceIdentityMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "golem.json")
	body := []byte(`{"device":"boa","release":"6.49.21","status":"pending"}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := state.Load(dir, "golem")
	if err == nil {
		t.Fatal("expected error")
	}

	if _, statErr := os.Stat(filepath.Join(dir, "boa.json")); statErr == nil {
		t.Fatal("boa.json was written")
	}
}

func TestSaveRejectsUnsafeDeviceNames(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "state")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"../x", "foo/bar", "", ".", ".."} {
		t.Run(deviceCaseName(name), func(t *testing.T) {
			job := sampleJob(state.StatusPending)
			job.Device = name
			err := state.Save(dir, job)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "invalid device") {
				t.Fatalf("got %v", err)
			}

			escaped := filepath.Join(root, "x.json")
			if _, statErr := os.Stat(escaped); statErr == nil {
				t.Fatal("wrote outside state dir")
			}
			nested := filepath.Join(dir, "foo")
			if _, statErr := os.Stat(nested); statErr == nil {
				t.Fatal("created nested path")
			}
		})
	}
}

func TestLoadRejectsUnsafeDeviceNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"../x", "foo/bar", "", ".", ".."} {
		t.Run(deviceCaseName(name), func(t *testing.T) {
			_, err := state.Load(dir, name)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "invalid device") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestAdvanceSkipsComplete(t *testing.T) {
	dir := t.TempDir()
	job := sampleJob(state.StatusComplete)
	job.Stage = "COMPLETE"
	if err := state.Save(dir, job); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "golem.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantStage := job.Stage
	wantUpdated := job.UpdatedAt

	if err := state.Advance(dir, job, "PREFLIGHT"); err != nil {
		t.Fatal(err)
	}
	if job.Stage != wantStage {
		t.Fatalf("in-memory stage %q", job.Stage)
	}
	if !job.UpdatedAt.Equal(wantUpdated) {
		t.Fatalf("in-memory UpdatedAt changed to %v", job.UpdatedAt)
	}
	if job.Status != state.StatusComplete {
		t.Fatalf("in-memory status %q", job.Status)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("disk changed:\nbefore %s\nafter %s", before, after)
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

func TestListLoadsJobsSorted(t *testing.T) {
	dir := t.TempDir()
	jobs := []*state.DeviceJob{
		{
			Device:  "zebra",
			Group:   "b",
			Release: "6.49.21",
			Status:  state.StatusPending,
		},
		{
			Device:  "alpha",
			Group:   "a",
			Release: "6.49.21",
			Status:  state.StatusFailed,
			Stage:   "PREFLIGHT",
		},
		{
			Device:  "beta",
			Group:   "a",
			Release: "6.49.18",
			Status:  state.StatusComplete,
			Stage:   "COMPLETE",
		},
	}
	for _, job := range jobs {
		if err := state.Save(dir, job); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "rosup.lock"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := state.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len %d", len(got))
	}
	want := []string{"alpha", "beta", "zebra"}
	for i, name := range want {
		if got[i].Device != name {
			t.Fatalf("got[%d]=%q want %q", i, got[i].Device, name)
		}
	}
}

func TestListMissingDir(t *testing.T) {
	got, err := state.List(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d", len(got))
	}
}

func deviceCaseName(name string) string {
	if name == "" {
		return "empty"
	}
	return name
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

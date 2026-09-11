package backup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/taihen/rosup/internal/backup"
	"github.com/taihen/rosup/internal/config"
)

type fakeClient struct {
	export    string
	exportErr error
	runs      []string
	files     map[string][]byte
	download  [][2]string
	removed   []string
	uploads   [][2]string
}

func (f *fakeClient) Run(_ context.Context, command string) (string, error) {
	f.runs = append(f.runs, command)
	if command == "/export hide-sensitive" {
		if f.exportErr != nil {
			return "", f.exportErr
		}
		return f.export, nil
	}
	if strings.HasPrefix(command, "/system backup save name=") {
		name, ok := backupNameFromSave(command)
		if !ok {
			return "", errors.New("malformed backup save")
		}
		if f.files == nil {
			f.files = map[string][]byte{}
		}
		f.files[name+".backup"] = []byte("binary-backup")
		return "", nil
	}
	if strings.HasPrefix(command, "/system backup load name=") {
		return "", nil
	}
	return "", errors.New("unexpected command " + command)
}

func (f *fakeClient) Upload(_ context.Context, local, remote string) error {
	data, err := os.ReadFile(local)
	if err != nil {
		return err
	}
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	f.files[remote] = data
	f.uploads = append(f.uploads, [2]string{local, remote})
	return nil
}

func (f *fakeClient) Download(_ context.Context, remote, local string) error {
	f.download = append(f.download, [2]string{remote, local})
	data, ok := f.files[remote]
	if !ok {
		return errors.New("missing remote " + remote)
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o700); err != nil {
		return err
	}
	return os.WriteFile(local, data, 0o600)
}

func (f *fakeClient) Remove(_ context.Context, remote string) error {
	f.removed = append(f.removed, remote)
	delete(f.files, remote)
	return nil
}

func (f *fakeClient) Close() error { return nil }

func backupNameFromSave(command string) (string, bool) {
	const prefix = "/system backup save name="
	if !strings.HasPrefix(command, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(command, prefix)
	name, _, ok := strings.Cut(rest, " ")
	if !ok || name == "" {
		return "", false
	}
	return name, true
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	ops := filepath.Join(root, "ops")
	if err := os.MkdirAll(ops, 0o700); err != nil {
		t.Fatal(err)
	}
	return &config.Config{
		BackupDir:           filepath.Join(root, "backups"),
		BackupRetentionDays: 30,
		Ops: config.OpsConfig{
			Path: ops,
		},
	}
}

func TestExportAndBackupRedactsSensitiveExport(t *testing.T) {
	cfg := testConfig(t)
	client := &fakeClient{
		export: "password=hunter2\n/ip address add address=192.0.2.1/24\n",
	}

	got, err := backup.ExportAndBackup(context.Background(), cfg, "golem", client)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Export, "hunter2") {
		t.Fatalf("export leaked secret: %s", got.Export)
	}
	if !strings.Contains(got.Export, "password=***") {
		t.Fatalf("export not redacted: %s", got.Export)
	}
	if len(client.runs) == 0 || client.runs[0] != "/export hide-sensitive" {
		t.Fatalf("commands %v", client.runs)
	}
}

func TestExportAndBackupWritesBinaryUnderDeviceDir(t *testing.T) {
	cfg := testConfig(t)
	client := &fakeClient{export: "/ip address print\n"}

	got, err := backup.ExportAndBackup(context.Background(), cfg, "golem", client)
	if err != nil {
		t.Fatal(err)
	}

	wantDir := filepath.Join(cfg.BackupDir, "golem")
	fi, err := os.Stat(wantDir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("device backup dir perm %04o", perm)
	}

	if got.BackupPath == "" {
		t.Fatal("empty BackupPath")
	}
	if filepath.Dir(got.BackupPath) != wantDir {
		t.Fatalf("backup path %q not under %q", got.BackupPath, wantDir)
	}
	if filepath.Ext(got.BackupPath) != ".backup" {
		t.Fatalf("backup path %q", got.BackupPath)
	}
	base := filepath.Base(got.BackupPath)
	if !strings.HasPrefix(base, "rosup-golem-") || !strings.HasSuffix(base, ".backup") {
		t.Fatalf("backup name %q", base)
	}

	fi, err = os.Stat(got.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("backup file perm %04o", perm)
	}

	data, err := os.ReadFile(got.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "binary-backup" {
		t.Fatalf("backup content %q", data)
	}
	if got.ExportPath == "" {
		t.Fatal("empty ExportPath")
	}
	if filepath.Dir(got.ExportPath) != wantDir {
		t.Fatalf("export path %q not under %q", got.ExportPath, wantDir)
	}
	if filepath.Ext(got.ExportPath) != ".rsc" {
		t.Fatalf("export path %q", got.ExportPath)
	}
	if strings.TrimSuffix(filepath.Base(got.BackupPath), ".backup") != strings.TrimSuffix(filepath.Base(got.ExportPath), ".rsc") {
		t.Fatalf("export stem mismatch: backup %q export %q", got.BackupPath, got.ExportPath)
	}
	exportData, err := os.ReadFile(got.ExportPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(exportData) != got.Export {
		t.Fatalf("export file %q want %q", exportData, got.Export)
	}
	fi, err = os.Stat(got.ExportPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("export file perm %04o", perm)
	}

	var save string
	for _, c := range client.runs {
		if strings.HasPrefix(c, "/system backup save ") {
			save = c
			break
		}
	}
	wantSave := "/system backup save name=" + strings.TrimSuffix(base, ".backup") + " dont-encrypt=yes"
	if save != wantSave {
		t.Fatalf("save command %q want %q", save, wantSave)
	}
}

func TestExportAndBackupDownloadsAndDeletesRemoteBackup(t *testing.T) {
	cfg := testConfig(t)
	client := &fakeClient{export: "# empty\n"}

	got, err := backup.ExportAndBackup(context.Background(), cfg, "golem", client)
	if err != nil {
		t.Fatal(err)
	}

	remote := filepath.Base(got.BackupPath)
	if len(client.download) != 1 {
		t.Fatalf("downloads %v", client.download)
	}
	if client.download[0][0] != remote {
		t.Fatalf("download remote %q", client.download[0][0])
	}
	if client.download[0][1] != got.BackupPath {
		t.Fatalf("download local %q want %q", client.download[0][1], got.BackupPath)
	}
	if len(client.removed) != 1 || client.removed[0] != remote {
		t.Fatalf("removed %v want %q", client.removed, remote)
	}
	if _, ok := client.files[remote]; ok {
		t.Fatal("remote backup still present")
	}
}

func TestExportAndBackupDoesNotWriteOpsPathBackup(t *testing.T) {
	cfg := testConfig(t)
	opsBackup := filepath.Join(cfg.Ops.Path, "foo.backup")
	if err := os.WriteFile(opsBackup, []byte("leave-me"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := backup.ExportAndBackup(context.Background(), cfg, "golem", &fakeClient{export: "#\n"})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(opsBackup)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "leave-me" {
		t.Fatalf("ops foo.backup changed: %q", data)
	}

	if rel, err := filepath.Rel(cfg.Ops.Path, got.BackupPath); err == nil && !strings.HasPrefix(rel, "..") {
		t.Fatalf("backup written under ops.path: %s", got.BackupPath)
	}

	var extras []string
	err = filepath.Walk(cfg.Ops.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if path != opsBackup {
			extras = append(extras, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(extras) != 0 {
		t.Fatalf("unexpected files under ops.path: %v", extras)
	}

	src, err := os.ReadFile("backup.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "Ops") || strings.Contains(string(src), "ops.path") {
		t.Fatal("backup package must not use ops.path as a write path")
	}
}

const backupTimeFmt = "20060102T150405Z"

func TestExportAndBackupPrunesOldBackupsByNameTimestamp(t *testing.T) {
	cfg := testConfig(t)
	cfg.BackupRetentionDays = 30
	dir := filepath.Join(cfg.BackupDir, "golem")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	oldName := "rosup-golem-20200101T000000Z.backup"
	oldPath := filepath.Join(dir, oldName)
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	recentName := "rosup-golem-" + time.Now().UTC().Add(-24*time.Hour).Format(backupTimeFmt) + ".backup"
	recentPath := filepath.Join(dir, recentName)
	if err := os.WriteFile(recentPath, []byte("recent"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := backup.ExportAndBackup(context.Background(), cfg, "golem", &fakeClient{export: "#\n"}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old backup still present: %v", err)
	}
	if _, err := os.Stat(recentPath); err != nil {
		t.Fatalf("recent backup removed: %v", err)
	}
}

func TestExportAndBackupParsesTimestampUsingDevicePrefix(t *testing.T) {
	cfg := testConfig(t)
	cfg.BackupRetentionDays = 30
	dir := filepath.Join(cfg.BackupDir, "router-01")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	ours := filepath.Join(dir, "rosup-router-01-20200101T000000Z.backup")
	if err := os.WriteFile(ours, []byte("ours-old"), 0o600); err != nil {
		t.Fatal(err)
	}
	otherPrefix := filepath.Join(dir, "rosup-router-20200101T000000Z.backup")
	if err := os.WriteFile(otherPrefix, []byte("not-ours"), 0o600); err != nil {
		t.Fatal(err)
	}
	badTS := filepath.Join(dir, "rosup-router-01-not-a-time.backup")
	if err := os.WriteFile(badTS, []byte("bad-ts"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := backup.ExportAndBackup(context.Background(), cfg, "router-01", &fakeClient{export: "#\n"})
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(got.BackupPath)
	if !strings.HasPrefix(base, "rosup-router-01-") {
		t.Fatalf("backup name %q", base)
	}

	if _, err := os.Stat(ours); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hyphenated-device old backup still present: %v", err)
	}
	if _, err := os.Stat(otherPrefix); err != nil {
		t.Fatalf("non-matching prefix backup removed: %v", err)
	}
	if _, err := os.Stat(badTS); err != nil {
		t.Fatalf("unparseable backup removed: %v", err)
	}
}

func TestExportAndBackupRejectsUnsafeDeviceNames(t *testing.T) {
	cfg := testConfig(t)
	root := filepath.Dir(cfg.BackupDir)
	client := &fakeClient{export: "#\n"}

	for _, name := range []string{"../x", "foo/bar", "", ".", ".."} {
		caseName := name
		if caseName == "" {
			caseName = "empty"
		}
		t.Run(caseName, func(t *testing.T) {
			_, err := backup.ExportAndBackup(context.Background(), cfg, name, client)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "invalid device") && !strings.Contains(err.Error(), "must match") {
				t.Fatalf("got %v", err)
			}
			escaped := filepath.Join(root, "x.backup")
			if _, statErr := os.Stat(escaped); statErr == nil {
				t.Fatal("wrote outside backup dir")
			}
			if len(client.runs) != 0 {
				t.Fatalf("ran commands for invalid device: %v", client.runs)
			}
		})
	}
}

func TestExportAndBackupExportErrorCreatesNoLocalBackup(t *testing.T) {
	cfg := testConfig(t)
	client := &fakeClient{exportErr: errors.New("ssh down")}

	_, err := backup.ExportAndBackup(context.Background(), cfg, "golem", client)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ssh down") {
		t.Fatalf("got %v", err)
	}
	if len(client.runs) != 1 || client.runs[0] != "/export hide-sensitive" {
		t.Fatalf("commands %v", client.runs)
	}
	if _, statErr := os.Stat(filepath.Join(cfg.BackupDir, "golem")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("created backup dir after export failure")
	}
}

func TestRestoreUploadsBackupAndLoads(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "golem.backup")
	if err := os.WriteFile(local, []byte("binary-backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{}

	if err := backup.Restore(context.Background(), "golem", client, local); err != nil {
		t.Fatal(err)
	}
	if len(client.uploads) != 1 || client.uploads[0][1] != "golem.backup" {
		t.Fatalf("uploads %v", client.uploads)
	}
	if string(client.files["golem.backup"]) != "binary-backup" {
		t.Fatalf("remote file %q", client.files["golem.backup"])
	}
	if len(client.runs) != 1 || client.runs[0] != "/system backup load name=golem" {
		t.Fatalf("commands %v", client.runs)
	}
}

func TestRestoreRequiresBackupSuffix(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "golem.txt")
	if err := os.WriteFile(local, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{}
	err := backup.Restore(context.Background(), "golem", client, local)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), ".backup") {
		t.Fatalf("got %v", err)
	}
	if len(client.uploads) != 0 {
		t.Fatalf("uploaded %v", client.uploads)
	}
	if len(client.runs) != 0 {
		t.Fatalf("commands %v", client.runs)
	}
}

func TestRestoreMissingFile(t *testing.T) {
	client := &fakeClient{}
	err := backup.Restore(context.Background(), "golem", client, filepath.Join(t.TempDir(), "missing.backup"))
	if err == nil {
		t.Fatal("expected error")
	}
	if len(client.uploads) != 0 {
		t.Fatalf("uploaded %v", client.uploads)
	}
}

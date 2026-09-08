package release_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/release"
)

type fakeResp struct {
	body   []byte
	status int
	err    error
}

type fakeHTTP struct {
	t       *testing.T
	muGets  []string
	byURL   map[string]fakeResp
	missing int
}

func newFakeHTTP(t *testing.T) *fakeHTTP {
	t.Helper()
	return &fakeHTTP{t: t, byURL: make(map[string]fakeResp), missing: 404}
}

func (f *fakeHTTP) set(url string, status int, body []byte) {
	f.byURL[url] = fakeResp{body: body, status: status}
}

func (f *fakeHTTP) Get(_ context.Context, url string) ([]byte, int, error) {
	f.muGets = append(f.muGets, url)
	r, ok := f.byURL[url]
	if !ok {
		return nil, f.missing, nil
	}
	return r.body, r.status, r.err
}

func (f *fakeHTTP) gotStable() bool {
	for _, u := range f.muGets {
		if strings.Contains(u, "NEWEST6.stable") {
			return true
		}
	}
	return false
}

func testConfig(t *testing.T, archs ...string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	return &config.Config{
		PackageDir:    filepath.Join(dir, "packages"),
		Architectures: archs,
		Channel:       "long-term",
		LockPath:      filepath.Join(dir, "rosup.lock"),
	}
}

func listingHTTP(t *testing.T, bodyFor func(name string) []byte, archs ...string) *fakeHTTP {
	t.Helper()
	f := newFakeHTTP(t)
	f.set(release.NewestURL(), 200, mikrotikFixture(t, "NEWEST6.long-term"))
	listing := mikrotikFixture(t, "listing-6.49.21.html")
	f.set(release.DirectoryURL("6.49.21"), 200, listing)
	names := release.ParseListing(listing)
	for _, arch := range archs {
		for _, name := range release.FilterByArch(names, arch, "6.49.21") {
			f.set(fileURL("6.49.21", name), 200, bodyFor(name))
		}
	}
	return f
}

func fileURL(version, name string) string {
	dir := strings.TrimRight(release.DirectoryURL(version), "/")
	return dir + "/" + name
}

func TestSyncSHA256MismatchFails(t *testing.T) {
	cfg := testConfig(t, "arm")
	bodyFor := func(name string) []byte { return []byte("body:" + name) }
	f := listingHTTP(t, bodyFor, "arm")
	ctx := context.Background()
	if _, err := release.Sync(ctx, cfg, f); err != nil {
		t.Fatal(err)
	}

	tampered := fileURL("6.49.21", "routeros-arm-6.49.21.npk")
	f.set(tampered, 200, []byte("tampered-body"))
	_, err := release.Sync(ctx, cfg, f)
	if err == nil {
		t.Fatal("expected sha256 mismatch")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "sha256") {
		t.Fatalf("error %v", err)
	}

	got, err := os.ReadFile(filepath.Join(cfg.PackageDir, "6.49.21", "routeros-arm-6.49.21.npk"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "body:routeros-arm-6.49.21.npk" {
		t.Fatalf("immutable store mutated: %q", got)
	}
}

func TestSyncSameVersionDoesNotRewrite(t *testing.T) {
	cfg := testConfig(t, "arm")
	bodyFor := func(name string) []byte { return []byte("body:" + name) }
	f := listingHTTP(t, bodyFor, "arm")
	ctx := context.Background()
	if _, err := release.Sync(ctx, cfg, f); err != nil {
		t.Fatal(err)
	}

	npk := filepath.Join(cfg.PackageDir, "6.49.21", "routeros-arm-6.49.21.npk")
	old := time.Now().Add(-time.Hour).UTC()
	if err := os.Chtimes(npk, old, old); err != nil {
		t.Fatal(err)
	}

	if _, err := release.Sync(ctx, cfg, f); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(npk)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Fatalf("rewrote %s: mtime %s want %s", npk, info.ModTime(), old)
	}
}

func TestSyncZeroFilesForArchFails(t *testing.T) {
	cfg := testConfig(t, "arm", "mmips")
	html := []byte(`<a href="routeros-arm-6.49.21.npk">routeros-arm-6.49.21.npk</a>`)
	f := newFakeHTTP(t)
	f.set(release.NewestURL(), 200, mikrotikFixture(t, "NEWEST6.long-term"))
	f.set(release.DirectoryURL("6.49.21"), 200, html)
	f.set(fileURL("6.49.21", "routeros-arm-6.49.21.npk"), 200, []byte("arm-pkg"))

	_, err := release.Sync(context.Background(), cfg, f)
	if err == nil {
		t.Fatal("expected error for arch with zero files")
	}
	if !strings.Contains(err.Error(), "mmips") {
		t.Fatalf("error should mention mmips, got %v", err)
	}
}

func TestSyncDoesNotFetchStableChannel(t *testing.T) {
	cfg := testConfig(t, "arm")
	f := listingHTTP(t, func(name string) []byte { return []byte(name) }, "arm")
	if _, err := release.Sync(context.Background(), cfg, f); err != nil {
		t.Fatal(err)
	}
	if f.gotStable() {
		t.Fatalf("requested NEWEST6.stable: %v", f.muGets)
	}
	if len(f.muGets) == 0 || f.muGets[0] != release.NewestURL() {
		t.Fatalf("first request %v", f.muGets)
	}
}

func TestSyncWritesManifestChecksums(t *testing.T) {
	cfg := testConfig(t, "arm")
	bodyFor := func(name string) []byte { return []byte("body:" + name) }
	f := listingHTTP(t, bodyFor, "arm")
	man, err := release.Sync(context.Background(), cfg, f)
	if err != nil {
		t.Fatal(err)
	}
	if man.Version != "6.49.21" || man.Channel != "long-term" {
		t.Fatalf("manifest %+v", man)
	}
	if len(man.Files) == 0 {
		t.Fatal("expected files")
	}

	raw, err := os.ReadFile(filepath.Join(cfg.PackageDir, "6.49.21", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk release.Manifest
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	byName := map[string]release.File{}
	for _, file := range onDisk.Files {
		byName[file.Name] = file
	}
	wantName := "routeros-arm-6.49.21.npk"
	got, ok := byName[wantName]
	if !ok {
		t.Fatalf("missing %s in %#v", wantName, byName)
	}
	if got.Architecture != "arm" || got.Package != "routeros" {
		t.Fatalf("file %+v", got)
	}
	sum := sha256.Sum256(bodyFor(wantName))
	if got.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 %q", got.SHA256)
	}
	if got.Size != int64(len(bodyFor(wantName))) {
		t.Fatalf("size %d", got.Size)
	}
}

func TestSyncEmptyListingProbesExtraPackages(t *testing.T) {
	cfg := testConfig(t, "arm")
	f := newFakeHTTP(t)
	f.set(release.NewestURL(), 200, mikrotikFixture(t, "NEWEST6.long-term"))
	f.set(release.DirectoryURL("6.49.21"), 200, []byte("<html>empty</html>"))
	f.set(release.PackageURL("6.49.21", "arm", "routeros"), 200, []byte("ros"))
	f.set(release.PackageURL("6.49.21", "arm", "wireless"), 200, []byte("wifi"))

	man, err := release.Sync(context.Background(), cfg, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(man.Files) != 2 {
		t.Fatalf("files %#v", man.Files)
	}
	for _, u := range f.muGets {
		if strings.Contains(u, "NEWEST6.stable") {
			t.Fatal(u)
		}
	}
}

func TestSyncDirectoryListing500FailsWithoutProbe(t *testing.T) {
	cfg := testConfig(t, "arm")
	f := newFakeHTTP(t)
	f.set(release.NewestURL(), 200, mikrotikFixture(t, "NEWEST6.long-term"))
	f.set(release.DirectoryURL("6.49.21"), 500, []byte("internal error"))
	f.set(release.PackageURL("6.49.21", "arm", "routeros"), 200, []byte("ros"))
	f.set(release.PackageURL("6.49.21", "arm", "wireless"), 200, []byte("wifi"))

	_, err := release.Sync(context.Background(), cfg, f)
	if err == nil {
		t.Fatal("expected error when directory listing returns 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error should mention status 500, got %v", err)
	}
	for _, u := range f.muGets {
		if strings.Contains(u, "/routeros-arm-") || strings.Contains(u, "/wireless-arm-") {
			t.Fatalf("probed extra package after 500 listing: %s", u)
		}
	}
}

func TestSyncProbeZeroFilesForArchFails(t *testing.T) {
	cfg := testConfig(t, "arm")
	f := newFakeHTTP(t)
	f.set(release.NewestURL(), 200, mikrotikFixture(t, "NEWEST6.long-term"))
	f.set(release.DirectoryURL("6.49.21"), 404, nil)

	_, err := release.Sync(context.Background(), cfg, f)
	if err == nil {
		t.Fatal("expected error for arch with zero files")
	}
	if !strings.Contains(err.Error(), "arm") {
		t.Fatalf("error should mention arm, got %v", err)
	}
}

func TestListVersionsFromDisk(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"6.49.21", "6.48.7"} {
		sub := filepath.Join(dir, v)
		if err := os.MkdirAll(sub, 0o700); err != nil {
			t.Fatal(err)
		}
		man := release.Manifest{Version: v, Channel: "long-term"}
		raw, err := json.Marshal(man)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "manifest.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "not-a-release"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := release.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "6.48.7" || got[1] != "6.49.21" {
		t.Fatalf("got %#v", got)
	}
}

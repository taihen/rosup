package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/taihen/rosup/internal/config"
)

const (
	HTTPTimeout = 2 * time.Minute
	maxMetaBody = 1 << 20
	maxNPKBody  = 512 << 20
)

type HTTPGet interface {
	Get(ctx context.Context, url string) (body []byte, status int, err error)
}

type HTTPClient struct {
	client *http.Client
}

func NewHTTPClient() *HTTPClient {
	return &HTTPClient{client: &http.Client{Timeout: HTTPTimeout}}
}

func (c *HTTPClient) Get(ctx context.Context, rawURL string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "rosup")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := readLimited(resp.Body, bodyLimit(rawURL))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func bodyLimit(rawURL string) int64 {
	u := rawURL
	if i := strings.Index(u, "?"); i >= 0 {
		u = u[:i]
	}
	if strings.HasSuffix(strings.ToLower(u), ".npk") {
		return maxNPKBody
	}
	return maxMetaBody
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("release: response body exceeds %d bytes", max)
	}
	return body, nil
}

func fileURL(version, name string) string {
	return strings.TrimRight(DirectoryURL(version), "/") + "/" + path.Base(name)
}

func Sync(ctx context.Context, cfg *config.Config, client HTTPGet) (*Manifest, error) {
	if cfg == nil {
		return nil, errors.New("release: nil config")
	}
	if client == nil {
		return nil, errors.New("release: nil HTTP client")
	}
	if len(cfg.Architectures) == 0 {
		return nil, errors.New("release: architectures must be non-empty")
	}

	newestBody, status, err := client.Get(ctx, NewestURL())
	if err != nil {
		return nil, fmt.Errorf("release: fetch NEWEST: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("release: NEWEST status %d", status)
	}
	version, err := ParseNewest(newestBody)
	if err != nil {
		return nil, err
	}

	dirURL := DirectoryURL(version)
	listBody, listStatus, err := client.Get(ctx, dirURL)
	if err != nil {
		return nil, fmt.Errorf("release: list %s: %w", dirURL, err)
	}

	var names []string
	probe := false
	switch listStatus {
	case http.StatusOK:
		names = ParseListing(listBody)
		probe = len(names) == 0
	case http.StatusNotFound, http.StatusForbidden:
		probe = true
	default:
		return nil, fmt.Errorf("release: list %s: status %d", dirURL, listStatus)
	}

	destDir := filepath.Join(cfg.PackageDir, version)
	var files []File
	if probe {
		files, err = syncFromProbe(ctx, client, destDir, version, cfg.Architectures)
	} else {
		files, err = syncFromListing(ctx, client, destDir, version, cfg.Architectures, names)
	}
	if err != nil {
		return nil, err
	}

	channel := cfg.Channel
	if channel == "" {
		channel = "long-term"
	}
	man := Manifest{
		Version:       version,
		Channel:       channel,
		Architectures: append([]string(nil), cfg.Architectures...),
		Files:         files,
		SyncedAt:      time.Now().UTC(),
	}
	if err := writeManifest(destDir, &man); err != nil {
		return nil, err
	}
	return &man, nil
}

type plannedFile struct {
	name string
	arch string
	pkg  string
	url  string
}

func syncFromListing(ctx context.Context, client HTTPGet, destDir, version string, archs, names []string) ([]File, error) {
	var planned []plannedFile
	for _, arch := range archs {
		matched := FilterByArch(names, arch, version)
		if len(matched) == 0 {
			return nil, fmt.Errorf("release: zero files for architecture %s", arch)
		}
		for _, name := range matched {
			pkg, fileArch, ok := ParseNPKName(name, version)
			if !ok {
				continue
			}
			planned = append(planned, plannedFile{
				name: name,
				arch: fileArch,
				pkg:  pkg,
				url:  fileURL(version, name),
			})
		}
	}
	return downloadAll(ctx, client, destDir, planned, true)
}

func syncFromProbe(ctx context.Context, client HTTPGet, destDir, version string, archs []string) ([]File, error) {
	var planned []plannedFile
	for _, arch := range archs {
		for _, pkg := range ExtraPackages {
			if pkg == "lcd" && arch != "x86" {
				continue
			}
			for _, name := range packageFileNames(version, arch, pkg) {
				planned = append(planned, plannedFile{
					name: name,
					arch: arch,
					pkg:  pkg,
					url:  fileURL(version, name),
				})
			}
		}
	}
	files, err := downloadAll(ctx, client, destDir, planned, false)
	if err != nil {
		return nil, err
	}
	gotROS := map[string]bool{}
	for _, f := range files {
		if f.Package == "routeros" {
			gotROS[f.Architecture] = true
		}
	}
	for _, arch := range archs {
		if !gotROS[arch] {
			return nil, fmt.Errorf("release: missing routeros package for architecture %s", arch)
		}
	}
	return files, nil
}

func downloadAll(ctx context.Context, client HTTPGet, destDir string, planned []plannedFile, requireOK bool) ([]File, error) {
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return nil, fmt.Errorf("release: mkdir %s: %w", destDir, err)
	}
	if err := os.Chmod(destDir, 0o700); err != nil {
		return nil, fmt.Errorf("release: chmod %s: %w", destDir, err)
	}
	out := make([]File, 0, len(planned))
	for _, p := range planned {
		body, status, err := client.Get(ctx, p.url)
		if err != nil {
			return nil, fmt.Errorf("release: get %s: %w", p.url, err)
		}
		if status != http.StatusOK {
			if requireOK {
				return nil, fmt.Errorf("release: get %s: status %d", p.url, status)
			}
			continue
		}
		sum := sha256.Sum256(body)
		hexSum := hex.EncodeToString(sum[:])
		dest := filepath.Join(destDir, p.name)
		if err := storeImmutable(dest, body, hexSum); err != nil {
			return nil, err
		}
		out = append(out, File{
			Name:         p.name,
			Architecture: p.arch,
			Package:      p.pkg,
			SHA256:       hexSum,
			Size:         int64(len(body)),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func storeImmutable(dest string, body []byte, wantHex string) error {
	existing, err := os.ReadFile(dest)
	if err == nil {
		sum := sha256.Sum256(existing)
		if hex.EncodeToString(sum[:]) != wantHex {
			return fmt.Errorf("release: sha256 mismatch for %s", filepath.Base(dest))
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("release: read %s: %w", dest, err)
	}
	return writeFile(dest, body)
}

func writeFile(path string, body []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("release: write %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("release: chmod %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("release: rename %s: %w", tmp, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("release: chmod %s: %w", path, err)
	}
	return nil
}

func writeManifest(dir string, man *Manifest) error {
	data, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return fmt.Errorf("release: marshal manifest: %w", err)
	}
	data = append(data, '\n')
	return writeFile(filepath.Join(dir, "manifest.json"), data)
}

func Load(packageDir, version string) (Manifest, error) {
	if version == "" || version != filepath.Base(version) {
		return Manifest{}, fmt.Errorf("release: invalid version %q", version)
	}
	path := filepath.Join(packageDir, version, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("release: read %s: %w", path, err)
	}
	var man Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return Manifest{}, fmt.Errorf("release: parse %s: %w", path, err)
	}
	if man.Version != "" && man.Version != version {
		return Manifest{}, fmt.Errorf("release: manifest version %q does not match %q", man.Version, version)
	}
	return man, nil
}

func List(packageDir string) ([]string, error) {
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("release: list %s: %w", packageDir, err)
	}
	var versions []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		manPath := filepath.Join(packageDir, e.Name(), "manifest.json")
		if _, err := os.Stat(manPath); err != nil {
			continue
		}
		versions = append(versions, e.Name())
	}
	sort.Strings(versions)
	return versions, nil
}

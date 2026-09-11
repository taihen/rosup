package release

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Manifest struct {
	Version       string    `json:"version"`
	Channel       string    `json:"channel"`
	Architectures []string  `json:"architectures"`
	Files         []File    `json:"files"`
	SyncedAt      time.Time `json:"synced_at"`
}

type File struct {
	Name         string `json:"name"`
	Architecture string `json:"architecture"`
	Package      string `json:"package"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
}

// VerifyLocalFile checks the on-disk NPK against the manifest SHA-256.
func VerifyLocalFile(packageDir string, f File) error {
	if f.Name == "" || f.Name != filepath.Base(f.Name) {
		return fmt.Errorf("release: invalid package name %q", f.Name)
	}
	if f.SHA256 == "" {
		return fmt.Errorf("release: missing sha256 for %s", f.Name)
	}
	path := filepath.Join(packageDir, f.Name)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("release: read %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != f.SHA256 {
		return fmt.Errorf("release: sha256 mismatch for %s", f.Name)
	}
	return nil
}

// IndexByInstalledName maps RouterOS `/system package print` names to staged
// NPKs for one architecture. RouterOS 6 reports the combined image as
// routeros-<arch> while the download is stored as package "routeros".
func IndexByInstalledName(files []File, arch string) map[string]File {
	byPkg := make(map[string]File, len(files))
	for _, f := range files {
		if f.Architecture != arch {
			continue
		}
		if _, exists := byPkg[f.Package]; !exists {
			byPkg[f.Package] = f
		}
		if f.Package == "routeros" {
			alias := "routeros-" + f.Architecture
			if _, exists := byPkg[alias]; !exists {
				byPkg[alias] = f
			}
		}
	}
	return byPkg
}

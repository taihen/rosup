package release

import "time"

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

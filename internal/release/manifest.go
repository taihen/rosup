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

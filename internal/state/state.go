package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusComplete   = "complete"
	StatusFailed     = "failed"
)

var ErrJobComplete = errors.New("state: job already complete")

type DeviceJob struct {
	Device     string          `json:"device"`
	Release    string          `json:"release"`
	Group      string          `json:"group"`
	Stage      string          `json:"stage"`
	Status     string          `json:"status"` // pending|in_progress|complete|failed
	UpdatedAt  time.Time       `json:"updated_at"`
	LastError  string          `json:"last_error,omitempty"`
	Attempt    int             `json:"attempt"`
	Facts      json.RawMessage `json:"facts,omitempty"`
	ExportPath string          `json:"export_path,omitempty"`
	BackupPath string          `json:"backup_path,omitempty"`
}

func EnsureSecureDir(path string) error {
	if path == "" {
		return errors.New("state: empty path")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("state: mkdir %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("state: chmod %s: %w", path, err)
	}
	return nil
}

func jobPath(stateDir, name string) (string, error) {
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) {
		return "", fmt.Errorf("state: invalid device name %q", name)
	}
	base, err := filepath.Abs(stateDir)
	if err != nil {
		return "", fmt.Errorf("state: resolve %s: %w", stateDir, err)
	}
	path := filepath.Join(base, name+".json")
	if filepath.Dir(path) != base {
		return "", fmt.Errorf("state: invalid device name %q", name)
	}
	return path, nil
}

func Load(stateDir, device string) (*DeviceJob, error) {
	path, err := jobPath(stateDir, device)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("state: load %s: %w", path, err)
	}
	var job DeviceJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("state: parse %s: %w", path, err)
	}
	if job.Device != device {
		return nil, fmt.Errorf("state: device identity %q does not match %q", job.Device, device)
	}
	return &job, nil
}

func Save(stateDir string, job *DeviceJob) error {
	if job == nil {
		return errors.New("state: missing device")
	}
	path, err := jobPath(stateDir, job.Device)
	if err != nil {
		return err
	}

	existing, err := Load(stateDir, job.Device)
	if err == nil {
		if existing.Status == StatusComplete && job.Status != StatusComplete {
			if existing.Release == "" || job.Release == "" || existing.Release == job.Release {
				return ErrJobComplete
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := EnsureSecureDir(stateDir); err != nil {
		return err
	}

	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return fmt.Errorf("state: marshal %s: %w", job.Device, err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("state: write %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("state: chmod %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("state: rename %s: %w", tmp, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("state: chmod %s: %w", path, err)
	}
	return nil
}

func MarkStarted(stateDir string, job *DeviceJob) error {
	if job == nil {
		return errors.New("state: nil job")
	}
	if job.Status == StatusComplete {
		return nil
	}
	job.Status = StatusInProgress
	job.LastError = ""
	job.UpdatedAt = time.Now().UTC()
	return Save(stateDir, job)
}

func Advance(stateDir string, job *DeviceJob, stage string) error {
	if job == nil {
		return errors.New("state: nil job")
	}
	if job.Status == StatusComplete {
		return nil
	}
	job.Stage = stage
	job.UpdatedAt = time.Now().UTC()
	return Save(stateDir, job)
}

func List(stateDir string) ([]*DeviceJob, error) {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("state: list %s: %w", stateDir, err)
	}
	out := make([]*DeviceJob, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		device := strings.TrimSuffix(e.Name(), ".json")
		if device == "" || device != filepath.Base(device) {
			continue
		}
		job, err := Load(stateDir, device)
		if err != nil {
			// One corrupt file must not hide the rest of the fleet.
			out = append(out, &DeviceJob{
				Device:    device,
				Status:    "corrupt",
				LastError: err.Error(),
			})
			continue
		}
		out = append(out, job)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Device < out[j].Device
	})
	return out, nil
}

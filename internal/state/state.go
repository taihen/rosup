package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusComplete   = "complete"
	StatusFailed     = "failed"
)

type DeviceJob struct {
	Device    string    `json:"device"`
	Release   string    `json:"release"`
	Group     string    `json:"group"`
	Stage     string    `json:"stage"`
	Status    string    `json:"status"` // pending|in_progress|complete|failed
	UpdatedAt time.Time `json:"updated_at"`
	LastError string    `json:"last_error,omitempty"`
	Attempt   int       `json:"attempt"`
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

func Load(stateDir, device string) (*DeviceJob, error) {
	path := filepath.Join(stateDir, device+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("state: load %s: %w", path, err)
	}
	var job DeviceJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("state: parse %s: %w", path, err)
	}
	return &job, nil
}

func Save(stateDir string, job *DeviceJob) error {
	if job == nil || job.Device == "" {
		return errors.New("state: missing device")
	}
	if err := EnsureSecureDir(stateDir); err != nil {
		return err
	}

	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return fmt.Errorf("state: marshal %s: %w", job.Device, err)
	}

	path := filepath.Join(stateDir, job.Device+".json")
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
	job.UpdatedAt = time.Now().UTC()
	return Save(stateDir, job)
}

func Advance(stateDir string, job *DeviceJob, stage string) error {
	if job == nil {
		return errors.New("state: nil job")
	}
	job.Stage = stage
	job.UpdatedAt = time.Now().UTC()
	return Save(stateDir, job)
}

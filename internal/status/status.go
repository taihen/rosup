package status

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/inventory"
	"github.com/taihen/rosup/internal/state"
)

type Report struct {
	Devices []Device
	Orphans []Device
}

type Device struct {
	Name      string
	Group     string
	Release   string
	Stage     string
	Status    string
	LastError string
	UpdatedAt time.Time
}

func Run(cfg *config.Config, group, release string) (*Report, error) {
	if cfg == nil {
		return nil, errors.New("status: nil config")
	}
	devices, err := inventory.Load(cfg)
	if err != nil {
		return nil, err
	}
	jobs, err := state.List(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]*state.DeviceJob, len(jobs))
	for _, job := range jobs {
		byName[job.Device] = job
	}

	inInventory := make(map[string]struct{}, len(devices))
	report := &Report{Devices: make([]Device, 0, len(devices))}
	for _, d := range devices {
		inInventory[d.Name] = struct{}{}
		if group != "" && d.Group != group {
			continue
		}
		job := byName[d.Name]
		if release != "" && (job == nil || job.Release != release) {
			continue
		}
		row := Device{
			Name:  d.Name,
			Group: d.Group,
		}
		if job != nil {
			row.Release = job.Release
			row.Stage = job.Stage
			row.Status = job.Status
			row.LastError = job.LastError
			row.UpdatedAt = job.UpdatedAt
		}
		report.Devices = append(report.Devices, row)
	}
	for _, job := range jobs {
		if _, ok := inInventory[job.Device]; ok {
			continue
		}
		if group != "" && job.Group != group {
			continue
		}
		if release != "" && job.Release != release {
			continue
		}
		report.Orphans = append(report.Orphans, Device{
			Name:      job.Device,
			Group:     job.Group,
			Release:   job.Release,
			Stage:     job.Stage,
			Status:    job.Status,
			LastError: job.LastError,
			UpdatedAt: job.UpdatedAt,
		})
	}
	return report, nil
}

func Format(w io.Writer, r *Report) error {
	if r == nil {
		return errors.New("status: nil report")
	}
	if _, err := fmt.Fprintf(w, "devices: %d\n", len(r.Devices)); err != nil {
		return err
	}
	for _, d := range r.Devices {
		if err := writeDevice(w, d); err != nil {
			return err
		}
	}
	if len(r.Orphans) > 0 {
		if _, err := fmt.Fprintf(w, "orphans: %d\n", len(r.Orphans)); err != nil {
			return err
		}
		for _, d := range r.Orphans {
			if err := writeDevice(w, d); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeDevice(w io.Writer, d Device) error {
	status := d.Status
	if status == "" {
		status = "none"
	}
	stage := d.Stage
	if stage == "" {
		stage = "-"
	}
	release := d.Release
	if release == "" {
		release = "-"
	}
	if _, err := fmt.Fprintf(w, "%s\n  group: %s\n  release: %s\n  stage: %s\n  status: %s\n",
		d.Name, d.Group, release, stage, status); err != nil {
		return err
	}
	if !d.UpdatedAt.IsZero() {
		if _, err := fmt.Fprintf(w, "  updated_at: %s\n", d.UpdatedAt.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if d.LastError != "" {
		if _, err := fmt.Fprintf(w, "  last_error: %s\n", d.LastError); err != nil {
			return err
		}
	}
	return nil
}

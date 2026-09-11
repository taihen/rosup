package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/taihen/rosup/internal/auditgit"
	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/inventory"
	"github.com/taihen/rosup/internal/preflight"
	"github.com/taihen/rosup/internal/redact"
	"github.com/taihen/rosup/internal/release"
	"github.com/taihen/rosup/internal/rosname"
	"github.com/taihen/rosup/internal/state"
)

func Rollback(ctx context.Context, cfg *config.Config, name, toVersion string, opts Options) error {
	if cfg == nil {
		return errors.New("rollback: nil config")
	}
	if toVersion == "" {
		return errors.New("rollback: --to-version is required")
	}
	opts = applyDefaults(opts)

	devices, err := inventory.Load(cfg)
	if err != nil {
		return err
	}
	d, err := lookupDevice(devices, name)
	if err != nil {
		return err
	}

	man, err := release.Load(cfg.PackageDir, toVersion)
	if err != nil {
		return err
	}

	existing, err := state.Load(cfg.StateDir, d.Name)
	switch {
	case err == nil && existing.Status == state.StatusInProgress:
		return fmt.Errorf("rollback: %s: upgrade job is in progress at stage %s; finish or clear it first", d.Name, existing.Stage)
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return err
	case errors.Is(err, os.ErrNotExist):
		existing = nil
	}

	facts, err := discover.Probe(ctx, cfg, d, opts.Dial)
	if err != nil {
		return err
	}
	if existing != nil && existing.Status == state.StatusComplete && existing.Release == toVersion {
		if facts.Version != toVersion {
			return fmt.Errorf("rollback: %s: state says %s but device is %s", d.Name, toVersion, facts.Version)
		}
		for _, p := range facts.Packages {
			if p.Version != toVersion {
				return fmt.Errorf("rollback: %s: package %s version %s, want %s", d.Name, p.Name, p.Version, toVersion)
			}
		}
		return nil
	}
	if err := preflight.Check(facts, man); err != nil {
		return err
	}
	files, err := packagesToStage(facts, man)
	if err != nil {
		return fmt.Errorf("rollback: %s: %w", d.Name, err)
	}

	job := &state.DeviceJob{
		Device: d.Name,
		Group:  d.Group,
	}
	if existing != nil {
		job.Attempt = existing.Attempt
	}
	job.Release = toVersion
	job.Status = state.StatusInProgress
	job.Stage = StagePackages
	job.UpdatedAt = time.Now().UTC()
	job.LastError = ""
	raw, err := json.Marshal(facts)
	if err != nil {
		return fmt.Errorf("rollback: %s: marshal facts: %w", d.Name, err)
	}
	job.Facts = raw
	if err := state.Save(cfg.StateDir, job); err != nil {
		return err
	}

	r := &deviceRun{
		ctx:     ctx,
		cfg:     cfg,
		version: toVersion,
		d:       d,
		man:     man,
		opts:    opts,
		job:     job,
	}
	defer r.closeClient()

	if err := r.ensureClient(); err != nil {
		return markFailed(cfg.StateDir, job, err)
	}
	if err := r.uploadFiles(toVersion, files); err != nil {
		return markFailed(cfg.StateDir, job, err)
	}
	job.Stage = StageReboot
	_ = state.Save(cfg.StateDir, job)
	_, err = r.client.Run(ctx, cmdReboot)
	r.closeClient()
	if err != nil && ctx.Err() != nil {
		return markFailed(cfg.StateDir, job, fmt.Errorf("rollback: %s: %s: %w", d.Name, cmdReboot, ctx.Err()))
	}
	if err != nil && !rosname.IsExpectedDisconnect(err) {
		return markFailed(cfg.StateDir, job, fmt.Errorf("rollback: %s: %s: %w", d.Name, cmdReboot, err))
	}

	job.Stage = StageWaitForReconnect
	_ = state.Save(cfg.StateDir, job)
	if err := waitForReconnect(ctx, cfg, d, opts, nil); err != nil {
		return markFailed(cfg.StateDir, job, err)
	}

	got, err := discover.Probe(ctx, cfg, d, opts.Dial)
	if err != nil {
		return markFailed(cfg.StateDir, job, err)
	}
	if got.Version != toVersion {
		err := fmt.Errorf("rollback: %s: version %s, want %s", d.Name, got.Version, toVersion)
		return markFailed(cfg.StateDir, job, err)
	}
	for _, p := range got.Packages {
		if p.Version != toVersion {
			err := fmt.Errorf("rollback: %s: package %s version %s, want %s", d.Name, p.Name, p.Version, toVersion)
			return markFailed(cfg.StateDir, job, err)
		}
	}

	raw, err = json.Marshal(got)
	if err != nil {
		return markFailed(cfg.StateDir, job, err)
	}
	job.Facts = raw
	job.Status = state.StatusInProgress
	job.Stage = StageComplete
	job.LastError = ""
	job.UpdatedAt = time.Now().UTC()
	if err := state.Save(cfg.StateDir, job); err != nil {
		return err
	}

	auditJob := *job
	auditJob.Status = state.StatusComplete
	if err := opts.Push(ctx, cfg, &auditJob, toVersion, auditgit.Artifacts{
		Export: "",
		Result: &auditJob,
		Log:    "rollback",
	}); err != nil {
		job.LastError = redact.String(err.Error())
		_ = state.Save(cfg.StateDir, job)
		return fmt.Errorf("rollback: %s: audit: %w", d.Name, err)
	}

	job.Status = state.StatusComplete
	job.UpdatedAt = time.Now().UTC()
	return state.Save(cfg.StateDir, job)
}

func lookupDevice(devices []inventory.Device, name string) (inventory.Device, error) {
	for _, d := range devices {
		if d.Name == name {
			return d, nil
		}
	}
	return inventory.Device{}, fmt.Errorf("rollback: unknown device %q", name)
}

package upgrade

import (
	"context"
	"errors"
	"fmt"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/inventory"
	"github.com/taihen/rosup/internal/preflight"
	"github.com/taihen/rosup/internal/release"
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

	facts, err := discover.Probe(ctx, cfg, d, opts.Dial)
	if err != nil {
		return err
	}
	if err := preflight.Check(facts, man); err != nil {
		return err
	}
	files, err := packagesToStage(facts, man)
	if err != nil {
		return fmt.Errorf("rollback: %s: %w", d.Name, err)
	}

	r := &deviceRun{
		ctx:     ctx,
		cfg:     cfg,
		version: toVersion,
		d:       d,
		man:     man,
		opts:    opts,
	}
	defer r.closeClient()

	if err := r.ensureClient(); err != nil {
		return err
	}
	if err := r.uploadFiles(toVersion, files); err != nil {
		return err
	}
	_, err = r.client.Run(ctx, cmdReboot)
	r.closeClient()
	if err != nil && ctx.Err() != nil {
		return fmt.Errorf("rollback: %s: %s: %w", d.Name, cmdReboot, ctx.Err())
	}
	return waitForReconnect(ctx, cfg, d, opts, nil)
}

func lookupDevice(devices []inventory.Device, name string) (inventory.Device, error) {
	for _, d := range devices {
		if d.Name == name {
			return d, nil
		}
	}
	return inventory.Device{}, fmt.Errorf("rollback: unknown device %q", name)
}

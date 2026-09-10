package plan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/inventory"
	"github.com/taihen/rosup/internal/preflight"
	"github.com/taihen/rosup/internal/release"
)

type DiscoverFunc func(ctx context.Context, cfg *config.Config, group string) ([]discover.Result, error)

type Report struct {
	Release string
	Devices []Device
}

type Device struct {
	Device  inventory.Device
	Missing []string
	Err     error
}

func Run(ctx context.Context, cfg *config.Config, version, group string, discoverFn DiscoverFunc) (*Report, error) {
	if cfg == nil {
		return nil, errors.New("plan: nil config")
	}
	if version == "" {
		return nil, errors.New("plan: --release is required")
	}
	if discoverFn == nil {
		return nil, errors.New("plan: nil discover")
	}

	man, err := release.Load(cfg.PackageDir, version)
	if err != nil {
		return nil, err
	}

	results, err := discoverFn(ctx, cfg, group)
	if err != nil {
		return nil, err
	}

	report := &Report{
		Release: version,
		Devices: make([]Device, 0, len(results)),
	}
	for _, r := range results {
		report.Devices = append(report.Devices, Device{
			Device:  r.Device,
			Missing: missingPackages(r.Facts, man),
			Err:     preflight.Check(r.Facts, man),
		})
	}
	return report, nil
}

func Format(w io.Writer, r *Report) error {
	if r == nil {
		return errors.New("plan: nil report")
	}
	if _, err := fmt.Fprintf(w, "release: %s\n", r.Release); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "devices: %d\n", len(r.Devices)); err != nil {
		return err
	}
	for _, d := range r.Devices {
		missing := "none"
		if len(d.Missing) > 0 {
			missing = strings.Join(d.Missing, ", ")
		}
		if _, err := fmt.Fprintf(w, "%s\n  order: %d\n  role: %s\n  missing: %s\n",
			d.Device.Name, d.Device.Order, d.Device.Role, missing); err != nil {
			return err
		}
	}
	return nil
}

func Failure(r *Report) error {
	if r == nil {
		return nil
	}
	var parts []string
	for _, d := range r.Devices {
		if d.Err != nil {
			parts = append(parts, fmt.Sprintf("%s: %v", d.Device.Name, d.Err))
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return fmt.Errorf("plan: preflight failed:\n%s", strings.Join(parts, "\n"))
}

func missingPackages(facts discover.Facts, man release.Manifest) []string {
	have := release.IndexByInstalledName(man.Files, facts.ArchitectureName)
	var missing []string
	for _, p := range facts.Packages {
		if _, ok := have[p.Name]; !ok {
			missing = append(missing, p.Name)
		}
	}
	return missing
}

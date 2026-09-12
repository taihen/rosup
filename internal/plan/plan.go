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
	Skip    string
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
		skip := ""
		if preflight.AlreadyOnRelease(r.Facts, version) {
			skip = "already " + r.Facts.Version
		}
		report.Devices = append(report.Devices, Device{
			Device:  r.Device,
			Missing: missingPackages(r.Facts, man),
			Skip:    skip,
			Err:     preflight.Check(r.Facts, man),
		})
	}
	return report, nil
}

func WriteReport(w io.Writer, r *Report) error {
	if r == nil {
		return errors.New("plan: nil report")
	}
	blocked, ready := countBlockedReady(r)
	status := padRight("OK", len("FAILED"))
	if blocked > 0 {
		status = "FAILED"
	}
	if _, err := fmt.Fprintf(w, "plan: %s  %d blocked / %d ready / %d total  release %s\n",
		status, blocked, ready, len(r.Devices), r.Release); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "summary"); err != nil {
		return err
	}
	counts := causeCounts(r)
	for _, key := range summaryCauseOrder {
		if n := counts[key]; n > 0 {
			if err := writeSummaryLine(w, key, n); err != nil {
				return err
			}
		}
	}
	if err := writeSummaryLine(w, "ready", ready); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "hosts"); err != nil {
		return err
	}
	nameWidth, roleWidth, causeWidth := hostColumnWidths(r)
	hasBlocked := blocked > 0
	for _, d := range r.Devices {
		if err := writeHostRow(w, d, nameWidth, roleWidth, causeWidth, hasBlocked); err != nil {
			return err
		}
	}
	return nil
}

func Failure(r *Report) error {
	if r == nil {
		return nil
	}
	blocked, ready := countBlockedReady(r)
	if blocked == 0 {
		return nil
	}
	return fmt.Errorf("plan: preflight failed (%d blocked / %d ready)", blocked, ready)
}

var summaryCauseOrder = []string{"disk", "missing packages", "unsupported", "error"}

const summaryKeyWidth = 18

func writeSummaryLine(w io.Writer, key string, n int) error {
	dots := summaryKeyWidth - len(key)
	if dots < 2 {
		dots = 2
	}
	_, err := fmt.Fprintf(w, "  %s %s %d\n", key, strings.Repeat(".", dots), n)
	return err
}

func countBlockedReady(r *Report) (blocked, ready int) {
	for _, d := range r.Devices {
		if d.Err != nil {
			blocked++
		} else {
			ready++
		}
	}
	return blocked, ready
}

func causeCounts(r *Report) map[string]int {
	counts := map[string]int{}
	for _, d := range r.Devices {
		if d.Err == nil {
			continue
		}
		cause, _ := classify(d.Err)
		counts[cause]++
	}
	return counts
}

func classify(err error) (cause, detail string) {
	var disk *preflight.DiskError
	if errors.As(err, &disk) {
		return "disk", fmt.Sprintf("have %s  need %s", preflight.FormatSize(disk.Have), preflight.FormatSize(disk.Need))
	}
	var miss *preflight.MissingPackagesError
	if errors.As(err, &miss) {
		return "missing packages", miss.Arch + ": " + strings.Join(miss.Packages, ", ")
	}
	var un *preflight.UnsupportedError
	if errors.As(err, &un) {
		return "unsupported", "RouterOS 7 (" + un.Version + ")"
	}
	return "error", strings.TrimPrefix(err.Error(), "preflight: ")
}

func hostColumnWidths(r *Report) (name, role, cause int) {
	for _, d := range r.Devices {
		if n := len(d.Device.Name); n > name {
			name = n
		}
		if n := len(d.Device.Role); n > role {
			role = n
		}
		if d.Err == nil {
			continue
		}
		c, _ := classify(d.Err)
		if n := len(c); n > cause {
			cause = n
		}
	}
	return name, role, cause
}

func padRight(s string, width int) string {
	if width <= len(s) {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func writeHostRow(w io.Writer, d Device, nameWidth, roleWidth, causeWidth int, hasBlocked bool) error {
	name := padRight(d.Device.Name, nameWidth)
	if d.Err != nil {
		cause, detail := classify(d.Err)
		_, err := fmt.Fprintf(w, "  %s  BLOCKED  %s  %s\n", name, padRight(cause, causeWidth), detail)
		return err
	}
	missing := "none"
	if len(d.Missing) > 0 {
		missing = strings.Join(d.Missing, ", ")
	}
	status := "READY"
	if hasBlocked {
		status = padRight(status, len("BLOCKED"))
	}
	_, err := fmt.Fprintf(w, "  %s  %s  order %d  role %s  missing %s\n",
		name, status, d.Device.Order, padRight(d.Device.Role, roleWidth), missing)
	return err
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

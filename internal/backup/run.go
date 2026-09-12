package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/inventory"
	"github.com/taihen/rosup/internal/transport"
)

type DialFunc func(ctx context.Context, cfg config.SSHConfig, address string, port int) (transport.Client, error)

type FleetItem struct {
	Device string
	Export string
	Time   time.Time
}

type PushFunc func(ctx context.Context, cfg *config.Config, items []FleetItem) error

type Outcome struct {
	Device string
	Err    error
}

func Run(ctx context.Context, cfg *config.Config, group string, dial DialFunc, push PushFunc) ([]Outcome, error) {
	if cfg == nil {
		return nil, errors.New("backup: nil config")
	}
	if dial == nil {
		dial = transport.Dial
	}
	if push == nil {
		return nil, errors.New("backup: nil push")
	}

	devices, err := inventory.Load(cfg)
	if err != nil {
		return nil, err
	}
	devices, err = filterGroup(devices, group)
	if err != nil {
		return nil, err
	}

	outcomes := make([]Outcome, 0, len(devices))
	items := make([]FleetItem, 0, len(devices))
	var failed []string
	for _, d := range devices {
		out := Outcome{Device: d.Name}
		result, err := backupDevice(ctx, cfg, d, dial)
		if err != nil {
			out.Err = err
			failed = append(failed, d.Name)
		} else {
			items = append(items, FleetItem{
				Device: d.Name,
				Export: result.Export,
				Time:   result.Time,
			})
		}
		outcomes = append(outcomes, out)
	}

	pushErr := push(ctx, cfg, items)
	if len(items) > 0 || pushErr != nil {
		outcomes = append(outcomes, Outcome{Device: "git", Err: pushErr})
	}

	var deviceErr error
	if len(failed) > 0 {
		deviceErr = fmt.Errorf("backup: %d of %d device(s) failed: %s", len(failed), len(devices), strings.Join(failed, ", "))
	}
	return outcomes, errors.Join(pushErr, deviceErr)
}

func FormatOutcomes(w io.Writer, outcomes []Outcome) error {
	for _, o := range outcomes {
		var err error
		if o.Err != nil {
			_, err = fmt.Fprintf(w, "x  %s  %v\n", o.Device, o.Err)
		} else {
			_, err = fmt.Fprintf(w, "*  %s  ok\n", o.Device)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func backupDevice(ctx context.Context, cfg *config.Config, d inventory.Device, dial DialFunc) (Result, error) {
	port := d.Port
	if port == 0 {
		port = cfg.SSH.DefaultPort
	}
	client, err := dial(ctx, cfg.SSH, d.Address, port)
	if err != nil {
		return Result{}, fmt.Errorf("backup: %s: dial: %w", d.Name, err)
	}
	defer func() { _ = client.Close() }()
	return ExportAndBackup(ctx, cfg, d.Name, client)
}

func filterGroup(devices []inventory.Device, group string) ([]inventory.Device, error) {
	if group == "" {
		return devices, nil
	}
	var out []inventory.Device
	for _, d := range devices {
		if d.Group == group {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("backup: no devices in group %q", group)
	}
	return out, nil
}

package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/inventory"
	"github.com/taihen/rosup/internal/state"
	"github.com/taihen/rosup/internal/transport"
)

const (
	cmdResource    = "/system resource print"
	cmdPackage     = "/system package print"
	cmdRouterboard = "/system routerboard print"
	cmdIdentity    = "/system identity print"
	stageDiscover  = "DISCOVER"
)

type DialFunc func(ctx context.Context, cfg config.SSHConfig, address string, port int) (transport.Client, error)

type Result struct {
	Device inventory.Device
	Facts  Facts
}

func Run(ctx context.Context, cfg *config.Config, group string, dial DialFunc) ([]Result, error) {
	if cfg == nil {
		return nil, errors.New("discover: nil config")
	}
	if dial == nil {
		dial = transport.Dial
	}

	devices, err := inventory.Load(cfg)
	if err != nil {
		return nil, err
	}
	devices, err = filterGroup(devices, group)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(devices))
	for _, d := range devices {
		facts, err := probe(ctx, cfg, d, dial)
		if err != nil {
			return results, err
		}
		if err := writeFacts(cfg.StateDir, d, facts); err != nil {
			return results, err
		}
		results = append(results, Result{Device: d, Facts: facts})
	}
	return results, nil
}

func Format(w io.Writer, results []Result) error {
	for i, r := range results {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "%s\n", r.Device.Name); err != nil {
			return err
		}
		lines := []string{
			"  architecture-name: " + r.Facts.ArchitectureName,
			"  board-name: " + r.Facts.BoardName,
			"  version: " + r.Facts.Version,
			"  free-hdd-space: " + r.Facts.FreeHDDSpace,
			"  current-firmware: " + r.Facts.CurrentFirmware,
			"  upgrade-firmware: " + r.Facts.UpgradeFirmware,
			"  identity: " + r.Facts.Identity,
		}
		for _, line := range lines {
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, "  packages:"); err != nil {
			return err
		}
		for _, p := range r.Facts.Packages {
			if _, err := fmt.Fprintf(w, "    %s %s\n", p.Name, p.Version); err != nil {
				return err
			}
		}
	}
	return nil
}

func probe(ctx context.Context, cfg *config.Config, d inventory.Device, dial DialFunc) (Facts, error) {
	port := d.Port
	if port == 0 {
		port = cfg.SSH.DefaultPort
	}
	client, err := dial(ctx, cfg.SSH, d.Address, port)
	if err != nil {
		return Facts{}, fmt.Errorf("discover: %s: %w", d.Name, err)
	}
	defer func() { _ = client.Close() }()

	resource, err := client.Run(ctx, cmdResource)
	if err != nil {
		return Facts{}, fmt.Errorf("discover: %s: %s: %w", d.Name, cmdResource, err)
	}
	packages, err := client.Run(ctx, cmdPackage)
	if err != nil {
		return Facts{}, fmt.Errorf("discover: %s: %s: %w", d.Name, cmdPackage, err)
	}
	routerboard, err := client.Run(ctx, cmdRouterboard)
	if err != nil {
		return Facts{}, fmt.Errorf("discover: %s: %s: %w", d.Name, cmdRouterboard, err)
	}
	identity, err := client.Run(ctx, cmdIdentity)
	if err != nil {
		return Facts{}, fmt.Errorf("discover: %s: %s: %w", d.Name, cmdIdentity, err)
	}

	facts, err := Parse(resource, packages, routerboard, identity)
	if err != nil {
		return Facts{}, fmt.Errorf("discover: %s: %w", d.Name, err)
	}
	return facts, nil
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
		return nil, fmt.Errorf("discover: no devices in group %q", group)
	}
	return out, nil
}

func writeFacts(stateDir string, d inventory.Device, facts Facts) error {
	raw, err := json.Marshal(facts)
	if err != nil {
		return fmt.Errorf("discover: marshal facts: %w", err)
	}

	job, err := state.Load(stateDir, d.Name)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		job = &state.DeviceJob{
			Device: d.Name,
			Group:  d.Group,
			Stage:  stageDiscover,
			Status: state.StatusPending,
		}
	}

	job.Facts = raw
	job.UpdatedAt = time.Now().UTC()
	if job.Group == "" {
		job.Group = d.Group
	}
	return state.Save(stateDir, job)
}

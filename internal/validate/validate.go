package validate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/inventory"
	"github.com/taihen/rosup/internal/state"
	"github.com/taihen/rosup/internal/transport"
)

const (
	SystemLogCmd   = `/log print where topics~"system"`
	cmdResource    = "/system resource print"
	cmdPackage     = "/system package print"
	cmdRouterboard = "/system routerboard print"
	cmdIdentity    = "/system identity print"
	baselineFile   = "baseline.json"

	CmdOSPFNeighbor = "/routing ospf neighbor print"
	CmdIPRoute      = "/ip route print"
	CmdPPPoEServer  = "/interface pppoe-server server print"
	CmdPPPAAA       = "/ppp aaa print"
	CmdRADIUS       = "/radius print"
	CmdPPPActive    = "/ppp active print"
	CmdWireless     = "/interface wireless print"
	CmdWirelessReg  = "/interface wireless registration-table print"
	CmdBridge       = "/interface bridge print"
	CmdBridgeVLAN   = "/interface bridge vlan print"
	CmdInterface    = "/interface print"
	CmdIPAddress    = "/ip address print"
)

type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

type RoleFunc func(ctx context.Context, client transport.Client, baseline Baseline, facts discover.Facts) error

// RoleChecks is the extension point for role profiles (tasks 15–19).
// Shared checks always run first; a missing entry means no role-specific work yet.
var RoleChecks = map[string]RoleFunc{}

var ErrUnreachable = errors.New("ssh unreachable")

type Request struct {
	Config          *config.Config
	Device          inventory.Device
	Target          string
	Dial            discover.DialFunc
	Clock           Clock
	Profile         string
	UpgradeFirmware bool
	RoleCheck       RoleFunc
}

type Baseline struct {
	Device          string             `json:"device"`
	Version         string             `json:"version"`
	Packages        []discover.Package `json:"packages"`
	SSHUp           bool               `json:"ssh_up"`
	CurrentFirmware string             `json:"current_firmware"`
	UpgradeFirmware string             `json:"upgrade_firmware"`
	RoleFacts       json.RawMessage    `json:"role_facts"`
}

type realClock struct{}

func (realClock) Now() time.Time        { return time.Now() }
func (realClock) Sleep(d time.Duration) { time.Sleep(d) }

func FromFacts(device string, facts discover.Facts, sshUp bool, roleFacts json.RawMessage) Baseline {
	if len(roleFacts) == 0 {
		roleFacts = json.RawMessage("{}")
	}
	return Baseline{
		Device:          device,
		Version:         facts.Version,
		Packages:        facts.Packages,
		SSHUp:           sshUp,
		CurrentFirmware: facts.CurrentFirmware,
		UpgradeFirmware: facts.UpgradeFirmware,
		RoleFacts:       roleFacts,
	}
}

func JobDir(cfg *config.Config, device, release string) (string, error) {
	if cfg == nil {
		return "", errors.New("validate: nil config")
	}
	if cfg.DataDir == "" {
		return "", errors.New("validate: data_dir is required")
	}
	if err := safeName("device", device); err != nil {
		return "", err
	}
	if err := safeName("release", release); err != nil {
		return "", err
	}
	base, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return "", fmt.Errorf("validate: resolve data_dir: %w", err)
	}
	path := filepath.Join(base, device, release)
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("validate: invalid job dir %q", path)
	}
	return path, nil
}

func WriteBaseline(jobDir string, b Baseline) error {
	if len(b.RoleFacts) == 0 {
		b.RoleFacts = json.RawMessage("{}")
	}
	if err := state.EnsureSecureDir(jobDir); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("validate: marshal baseline: %w", err)
	}
	path := filepath.Join(jobDir, baselineFile)
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("validate: write %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("validate: chmod %s: %w", path, err)
	}
	return nil
}

func ReadBaseline(jobDir string) (Baseline, error) {
	path := filepath.Join(jobDir, baselineFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return Baseline{}, fmt.Errorf("validate: read %s: %w", path, err)
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return Baseline{}, fmt.Errorf("validate: parse %s: %w", path, err)
	}
	return b, nil
}

func Check(ctx context.Context, req Request) error {
	if req.Config == nil {
		return errors.New("validate: nil config")
	}
	if req.Target == "" {
		return errors.New("validate: target release is required")
	}
	if req.Dial == nil {
		req.Dial = transport.Dial
	}
	if req.Clock == nil {
		req.Clock = realClock{}
	}

	profileName := req.Profile
	if profileName == "" {
		profileName = req.Device.ValidationProfile
	}
	if profileName == "" {
		profileName = req.Device.Role
	}
	profile, err := LoadProfile(req.Config, profileName)
	if err != nil {
		return err
	}
	req.Clock.Sleep(profile.ConvergenceTimeout)

	port := req.Device.Port
	if port == 0 {
		port = req.Config.SSH.DefaultPort
	}
	client, err := req.Dial(ctx, req.Config.SSH, req.Device.Address, port)
	if err != nil {
		return fmt.Errorf("validate: %s: %w: %w", req.Device.Name, ErrUnreachable, err)
	}
	defer func() { _ = client.Close() }()

	facts, err := factsFromClient(ctx, req.Device.Name, client)
	if err != nil {
		return err
	}

	dir, err := JobDir(req.Config, req.Device.Name, req.Target)
	if err != nil {
		return err
	}
	baseline, err := ReadBaseline(dir)
	if err != nil {
		return err
	}

	if facts.Version != req.Target {
		return fmt.Errorf("validate: %s: version %s, want %s", req.Device.Name, facts.Version, req.Target)
	}
	if err := checkPackages(req.Device.Name, baseline.Packages, facts.Packages); err != nil {
		return err
	}
	if err := checkFirmware(req.Device.Name, facts.CurrentFirmware, facts.UpgradeFirmware, req.UpgradeFirmware); err != nil {
		return err
	}

	logOut, err := client.Run(ctx, SystemLogCmd)
	if err != nil {
		return fmt.Errorf("validate: %s: %s: %w", req.Device.Name, SystemLogCmd, err)
	}
	if err := InstallErrors(logOut); err != nil {
		return fmt.Errorf("validate: %s: %w", req.Device.Name, err)
	}

	fn := req.RoleCheck
	if fn == nil {
		fn = RoleChecks[profileName]
	}
	if fn != nil {
		return fn(withRoleMeta(ctx, req.Clock, profile), client, baseline, facts)
	}
	return nil
}

func factsFromClient(ctx context.Context, name string, client transport.Client) (discover.Facts, error) {
	resource, err := client.Run(ctx, cmdResource)
	if err != nil {
		return discover.Facts{}, fmt.Errorf("validate: %s: %s: %w", name, cmdResource, err)
	}
	packages, err := client.Run(ctx, cmdPackage)
	if err != nil {
		return discover.Facts{}, fmt.Errorf("validate: %s: %s: %w", name, cmdPackage, err)
	}
	routerboard, err := client.Run(ctx, cmdRouterboard)
	if err != nil {
		return discover.Facts{}, fmt.Errorf("validate: %s: %s: %w", name, cmdRouterboard, err)
	}
	identity, err := client.Run(ctx, cmdIdentity)
	if err != nil {
		return discover.Facts{}, fmt.Errorf("validate: %s: %s: %w", name, cmdIdentity, err)
	}
	facts, err := discover.Parse(resource, packages, routerboard, identity)
	if err != nil {
		return discover.Facts{}, fmt.Errorf("validate: %s: %w", name, err)
	}
	return facts, nil
}

func checkPackages(device string, want, have []discover.Package) error {
	wantNames := packageNames(want)
	haveNames := packageNames(have)
	wantSet := make(map[string]struct{}, len(wantNames))
	haveSet := make(map[string]struct{}, len(haveNames))
	for _, n := range wantNames {
		wantSet[n] = struct{}{}
	}
	for _, n := range haveNames {
		haveSet[n] = struct{}{}
	}
	var missing, extra []string
	for _, n := range wantNames {
		if _, ok := haveSet[n]; !ok {
			missing = append(missing, n)
		}
	}
	for _, n := range haveNames {
		if _, ok := wantSet[n]; !ok {
			extra = append(extra, n)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}
	switch {
	case len(missing) > 0 && len(extra) > 0:
		return fmt.Errorf("validate: %s: package set mismatch: missing %s, extra %s", device, strings.Join(missing, ", "), strings.Join(extra, ", "))
	case len(missing) > 0:
		return fmt.Errorf("validate: %s: missing packages: %s", device, strings.Join(missing, ", "))
	default:
		return fmt.Errorf("validate: %s: extra packages: %s", device, strings.Join(extra, ", "))
	}
}

func packageNames(pkgs []discover.Package) []string {
	out := make([]string, 0, len(pkgs))
	seen := make(map[string]struct{}, len(pkgs))
	for _, p := range pkgs {
		if p.Name == "" {
			continue
		}
		if _, ok := seen[p.Name]; ok {
			continue
		}
		seen[p.Name] = struct{}{}
		out = append(out, p.Name)
	}
	sort.Strings(out)
	return out
}

func checkFirmware(device, current, upgrade string, upgradingThisPass bool) error {
	if current == "" || upgrade == "" {
		return fmt.Errorf("validate: %s: missing RouterBOOT firmware", device)
	}
	if current == upgrade {
		return nil
	}
	if upgradingThisPass {
		return fmt.Errorf("validate: %s: RouterBOOT current-firmware %s, want %s", device, current, upgrade)
	}
	return nil
}

func safeName(kind, name string) error {
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) {
		return fmt.Errorf("validate: invalid %s %q", kind, name)
	}
	return nil
}

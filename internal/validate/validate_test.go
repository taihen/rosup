package validate_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/inventory"
	"github.com/taihen/rosup/internal/transport"
	"github.com/taihen/rosup/internal/validate"
)

const (
	target  = "6.49.21"
	current = "6.49.18"
)

func TestWriteBaselineSnapshotsSharedFactsUnderJobDir(t *testing.T) {
	cfg := testConfig(t)
	facts := sampleFacts(current)
	dir, err := validate.JobDir(cfg, "router-01", target)
	if err != nil {
		t.Fatal(err)
	}

	b := validate.FromFacts("router-01", facts, true, nil)
	if err := validate.WriteBaseline(dir, b); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "baseline.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"device", "version", "packages", "ssh_up", "current_firmware", "upgrade_firmware", "role_facts"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing %s in %s", key, raw)
		}
	}

	loaded, err := validate.ReadBaseline(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Device != "router-01" {
		t.Fatalf("device %q", loaded.Device)
	}
	if loaded.Version != current {
		t.Fatalf("version %q", loaded.Version)
	}
	if !loaded.SSHUp {
		t.Fatal("ssh_up")
	}
	if loaded.CurrentFirmware != "6.49.13" || loaded.UpgradeFirmware != "6.49.18" {
		t.Fatalf("firmware %s / %s", loaded.CurrentFirmware, loaded.UpgradeFirmware)
	}
	if len(loaded.Packages) != 2 || loaded.Packages[0].Name != "routeros" || loaded.Packages[1].Name != "wireless" {
		t.Fatalf("packages %+v", loaded.Packages)
	}
	if string(loaded.RoleFacts) != "{}" {
		t.Fatalf("role_facts %s", loaded.RoleFacts)
	}
}

func TestJobDirIsLocalDataDirNotAudit(t *testing.T) {
	cfg := testConfig(t)
	dir, err := validate.JobDir(cfg, "router-01", target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dir, cfg.DataDir) {
		t.Fatalf("job dir %q not under data_dir %q", dir, cfg.DataDir)
	}
	if strings.Contains(dir, "audit") {
		t.Fatalf("job dir must not be git audit: %s", dir)
	}
	if dir != filepath.Join(cfg.DataDir, "jobs", "router-01", target) {
		t.Fatalf("job dir %q", dir)
	}
}

func TestJobDirMigratesLegacyDeviceTree(t *testing.T) {
	cfg := testConfig(t)
	legacy := filepath.Join(cfg.DataDir, "boa", target)
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	baseline := filepath.Join(legacy, "baseline.json")
	if err := os.WriteFile(baseline, []byte(`{"device":"boa"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	dir, err := validate.JobDir(cfg, "boa", target)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cfg.DataDir, "jobs", "boa", target)
	if dir != want {
		t.Fatalf("job dir %q, want %q", dir, want)
	}
	if _, err := os.Stat(filepath.Join(want, "baseline.json")); err != nil {
		t.Fatalf("migrated baseline: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "boa")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy device dir still present: %v", err)
	}
}

func TestJobDirDoesNotMoveOpsCheckout(t *testing.T) {
	cfg := testConfig(t)
	ops := filepath.Join(cfg.DataDir, "ops")
	if err := os.MkdirAll(filepath.Join(ops, "inventory"), 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(ops, "inventory", "devices.yaml")
	if err := os.WriteFile(marker, []byte("devices: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := validate.JobDir(cfg, "ops", target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("ops checkout was moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "jobs", "ops")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reserved name should not be migrated into jobs")
	}
}

func TestParseInstallLogFixtureHasNoErrors(t *testing.T) {
	if err := validate.InstallErrors(ros6Fixture(t, "log-print-system.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestParseInstallLogFixtureDetectsError(t *testing.T) {
	err := validate.InstallErrors(ros6Fixture(t, "log-print-system-error.txt"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "install") {
		t.Fatalf("got %v", err)
	}
}

func TestParseInstallLogWithoutInstallLinesIsOK(t *testing.T) {
	if err := validate.InstallErrors(ros6Fixture(t, "log-print-system-login-only.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProfileConvergenceTimeoutFromOpsPath(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", "convergence_timeout: 5m\n")
	p, err := validate.LoadProfile(cfg, "ospf")
	if err != nil {
		t.Fatal(err)
	}
	if p.ConvergenceTimeout != 5*time.Minute {
		t.Fatalf("timeout %s", p.ConvergenceTimeout)
	}
}

func TestCheckWaitsProfileTimeoutNotReconnectBudget(t *testing.T) {
	cfg := testConfig(t)
	cfg.Reconnect.Timeout = 3 * time.Minute
	writeProfile(t, cfg, "ospf", "convergence_timeout: 5m\n")
	writeBaselineFor(t, cfg, sampleFacts(current))

	clock := newFakeClock()
	client := newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt"))
	err := validate.Check(context.Background(), validate.Request{
		Config:  cfg,
		Device:  testDevice(),
		Target:  target,
		Dial:    dialClient(client),
		Clock:   clock,
		Profile: "ospf",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(clock.sleeps) != 1 {
		t.Fatalf("sleeps %v", clock.sleeps)
	}
	if clock.sleeps[0] != 5*time.Minute {
		t.Fatalf("slept %s, want profile 5m not reconnect 3m", clock.sleeps[0])
	}
	if clock.now.Sub(clock.start) == 3*time.Minute {
		t.Fatal("used reconnect budget")
	}
}

func TestCheckRequiresTargetVersion(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", "convergence_timeout: 1ms\n")
	writeBaselineFor(t, cfg, sampleFacts(current))
	client := newFakeClient(t, current, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt"))

	err := validate.Check(context.Background(), validate.Request{
		Config:  cfg,
		Device:  testDevice(),
		Target:  target,
		Dial:    dialClient(client),
		Clock:   newFakeClock(),
		Profile: "ospf",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), current) || !strings.Contains(err.Error(), target) {
		t.Fatalf("got %v", err)
	}
}

func TestCheckRequiresBaselinePackageSet(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", "convergence_timeout: 1ms\n")
	writeBaselineFor(t, cfg, sampleFacts(current))
	client := newFakeClient(t, target, []string{"routeros"}, ros6Fixture(t, "log-print-system.txt"))

	err := validate.Check(context.Background(), validate.Request{
		Config:  cfg,
		Device:  testDevice(),
		Target:  target,
		Dial:    dialClient(client),
		Clock:   newFakeClock(),
		Profile: "ospf",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "wireless") {
		t.Fatalf("got %v", err)
	}
}

func TestCheckRejectsInstallErrorsFromSystemLog(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", "convergence_timeout: 1ms\n")
	writeBaselineFor(t, cfg, sampleFacts(current))
	client := newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system-error.txt"))

	err := validate.Check(context.Background(), validate.Request{
		Config:  cfg,
		Device:  testDevice(),
		Target:  target,
		Dial:    dialClient(client),
		Clock:   newFakeClock(),
		Profile: "ospf",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "install") {
		t.Fatalf("got %v", err)
	}
}

func TestCheckRequiresSSHReachable(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", "convergence_timeout: 1ms\n")
	writeBaselineFor(t, cfg, sampleFacts(current))

	err := validate.Check(context.Background(), validate.Request{
		Config: cfg,
		Device: testDevice(),
		Target: target,
		Dial: func(context.Context, config.SSHConfig, string, int) (transport.Client, error) {
			return nil, errors.New("connection refused")
		},
		Clock:   newFakeClock(),
		Profile: "ospf",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ssh") && !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("got %v", err)
	}
}

func TestCheckAcceptsMatchingRouterBOOTWhenNotUpgradingFirmware(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", "convergence_timeout: 1ms\n")
	facts := sampleFacts(current)
	facts.CurrentFirmware = "6.49.21"
	facts.UpgradeFirmware = "6.49.21"
	writeBaselineFor(t, cfg, facts)
	client := newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt"))
	client.firmwareCurrent = "6.49.21"
	client.firmwareUpgrade = "6.49.21"

	if err := validate.Check(context.Background(), validate.Request{
		Config:  cfg,
		Device:  testDevice(),
		Target:  target,
		Dial:    dialClient(client),
		Clock:   newFakeClock(),
		Profile: "ospf",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckAllowsNewerRouterBOOTWhenNotUpgradingThisPass(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", "convergence_timeout: 1ms\n")
	writeBaselineFor(t, cfg, sampleFacts(current))
	client := newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt"))
	client.firmwareCurrent = "6.49.13"
	client.firmwareUpgrade = "6.49.21"

	if err := validate.Check(context.Background(), validate.Request{
		Config:          cfg,
		Device:          testDevice(),
		Target:          target,
		Dial:            dialClient(client),
		Clock:           newFakeClock(),
		Profile:         "ospf",
		UpgradeFirmware: false,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckCallsRoleHookAfterSharedChecks(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", "convergence_timeout: 1ms\n")
	writeBaselineFor(t, cfg, sampleFacts(current))
	client := newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt"))

	var called bool
	err := validate.Check(context.Background(), validate.Request{
		Config:  cfg,
		Device:  testDevice(),
		Target:  target,
		Dial:    dialClient(client),
		Clock:   newFakeClock(),
		Profile: "ospf",
		RoleCheck: func(_ context.Context, _ transport.Client, b validate.Baseline, facts discover.Facts) error {
			called = true
			if b.Device != "router-01" {
				return fmt.Errorf("baseline device %q", b.Device)
			}
			if facts.Version != target {
				return fmt.Errorf("facts version %q", facts.Version)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("role hook not called")
	}
}

func TestRoleChecksRegisteredForAllProfiles(t *testing.T) {
	if validate.RoleChecks == nil {
		t.Fatal("RoleChecks must exist for profiles 15-19")
	}
	for _, role := range []string{"ospf", "pppoe", "radio", "switch", "access"} {
		if _, ok := validate.RoleChecks[role]; !ok {
			t.Fatalf("RoleChecks[%s] is missing", role)
		}
	}
}

func ros6Fixture(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "testdata", "ros6", name)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	ops := filepath.Join(root, "ops")
	if err := os.MkdirAll(filepath.Join(ops, "inventory", "profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	return &config.Config{
		DataDir:  filepath.Join(root, "data"),
		StateDir: filepath.Join(root, "state"),
		Ops: config.OpsConfig{
			Path:          ops,
			InventoryFile: "inventory/devices.yaml",
			AuditDir:      "audit",
		},
		SSH: config.SSHConfig{
			DefaultPort: 22,
		},
		Reconnect: config.Reconnect{
			Timeout:  3 * time.Minute,
			Attempts: 3,
		},
	}
}

func writeProfile(t *testing.T, cfg *config.Config, role, body string) {
	t.Helper()
	path := filepath.Join(cfg.Ops.Path, "inventory", "profiles", role+".yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeBaselineFor(t *testing.T, cfg *config.Config, facts discover.Facts) {
	t.Helper()
	writeBaselineRole(t, cfg, facts, nil)
}

func writeBaselineRole(t *testing.T, cfg *config.Config, facts discover.Facts, roleFacts json.RawMessage) {
	t.Helper()
	dir, err := validate.JobDir(cfg, "router-01", target)
	if err != nil {
		t.Fatal(err)
	}
	if err := validate.WriteBaseline(dir, validate.FromFacts("router-01", facts, true, roleFacts)); err != nil {
		t.Fatal(err)
	}
}

func testDevice() inventory.Device {
	return testDeviceRole("ospf")
}

func testDeviceRole(role string) inventory.Device {
	return inventory.Device{
		Name:              "router-01",
		Address:           "192.0.2.1",
		Role:              role,
		Group:             "core-a",
		ValidationProfile: role,
	}
}

func ospfProfileYAML() string {
	return `convergence_timeout: 1ms
neighbor_state_allow:
  - Full
  - 2-Way
route_count_tolerance: 0
`
}

func runCheck(t *testing.T, cfg *config.Config, device inventory.Device, client *fakeClient, clock validate.Clock) error {
	t.Helper()
	if clock == nil {
		clock = newFakeClock()
	}
	return validate.Check(context.Background(), validate.Request{
		Config:  cfg,
		Device:  device,
		Target:  target,
		Dial:    dialClient(client),
		Clock:   clock,
		Profile: device.ValidationProfile,
	})
}

func sampleFacts(version string) discover.Facts {
	return discover.Facts{
		ArchitectureName: "arm",
		BoardName:        "hAP ac^2",
		Version:          version,
		FreeHDDSpace:     "10.0MiB",
		Packages: []discover.Package{
			{Name: "routeros", Version: version},
			{Name: "wireless", Version: version},
		},
		CurrentFirmware: "6.49.13",
		UpgradeFirmware: "6.49.18",
		Identity:        "router-01",
	}
}

type fakeClock struct {
	start  time.Time
	now    time.Time
	sleeps []time.Duration
}

func newFakeClock() *fakeClock {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	return &fakeClock{start: now, now: now}
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(d time.Duration) {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
}

type fakeClient struct {
	version         string
	packages        []string
	log             string
	firmwareCurrent string
	firmwareUpgrade string
	runs            []string
	outputs         map[string]string
	seq             map[string][]string
	seqPos          map[string]int
}

func newFakeClient(t *testing.T, version string, pkgs []string, log string) *fakeClient {
	t.Helper()
	return &fakeClient{
		version:         version,
		packages:        pkgs,
		log:             log,
		firmwareCurrent: "6.49.13",
		firmwareUpgrade: "6.49.18",
		outputs:         map[string]string{},
		seq:             map[string][]string{},
		seqPos:          map[string]int{},
	}
}

func (c *fakeClient) set(command, output string) *fakeClient {
	c.outputs[command] = output
	return c
}

func (c *fakeClient) setSeq(command string, outputs ...string) *fakeClient {
	c.seq[command] = outputs
	c.seqPos[command] = 0
	return c
}

func (c *fakeClient) Run(_ context.Context, command string) (string, error) {
	c.runs = append(c.runs, command)
	if strings.Contains(command, "wifi") {
		return "", fmt.Errorf("ROS7 command not allowed: %s", command)
	}
	if outs := c.seq[command]; len(outs) > 0 {
		i := c.seqPos[command]
		if i >= len(outs) {
			i = len(outs) - 1
		} else {
			c.seqPos[command] = i + 1
		}
		return outs[i], nil
	}
	if out, ok := c.outputs[command]; ok {
		return out, nil
	}
	switch command {
	case "/system resource print":
		return fmt.Sprintf(`                  version: %s (long-term)
           free-hdd-space: 10.0MiB
        architecture-name: arm
               board-name: hAP ac^2
`, c.version), nil
	case "/system package print":
		var b strings.Builder
		for i, p := range c.packages {
			fmt.Fprintf(&b, " %d   %s                %s\n", i, p, c.version)
		}
		return b.String(), nil
	case "/system routerboard print":
		return fmt.Sprintf(`        board-name: hAP ac^2
  current-firmware: %s
  upgrade-firmware: %s
`, c.firmwareCurrent, c.firmwareUpgrade), nil
	case "/system identity print":
		return "  name: router-01\n", nil
	case validate.SystemLogCmd:
		return c.log, nil
	case "/routing ospf neighbor print", "/ip route print",
		"/interface pppoe-server server print", "/interface pppoe-server print",
		"/ppp aaa print", "/radius print", "/ppp active print",
		"/interface wireless print", "/interface wireless registration-table print",
		"/interface bridge print", "/interface bridge vlan print",
		"/interface print", "/ip address print":
		return "", nil
	default:
		return "", fmt.Errorf("unexpected command %q", command)
	}
}

func (c *fakeClient) Upload(context.Context, string, string) error {
	return errors.New("upload not allowed")
}
func (c *fakeClient) Download(context.Context, string, string) error {
	return errors.New("download not allowed")
}
func (c *fakeClient) Remove(context.Context, string) error {
	return errors.New("remove not allowed")
}
func (c *fakeClient) Close() error { return nil }

func dialClient(c transport.Client) discover.DialFunc {
	return func(context.Context, config.SSHConfig, string, int) (transport.Client, error) {
		return c, nil
	}
}

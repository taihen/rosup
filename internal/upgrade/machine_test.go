package upgrade_test

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

	"github.com/taihen/rosup/internal/auditgit"
	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/inventory"
	"github.com/taihen/rosup/internal/release"
	"github.com/taihen/rosup/internal/state"
	"github.com/taihen/rosup/internal/transport"
	"github.com/taihen/rosup/internal/upgrade"
	"github.com/taihen/rosup/internal/validate"
)

const (
	target  = "6.49.21"
	current = "6.49.18"
)

func TestRunRequiresRelease(t *testing.T) {
	cfg, world := setup(t, device("router-01", "core-a", 10, nil))
	err := upgrade.Run(context.Background(), cfg, "", "core-a", world.opts())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--release") {
		t.Fatalf("got %v", err)
	}
	if world.dialCount() != 0 {
		t.Fatalf("dialed %d times", world.dialCount())
	}
}

func TestHappyPathReachesCompleteWithoutRouterBOOT(t *testing.T) {
	cfg, world := setup(t, device("router-01", "core-a", 10, nil))
	sim := world.sim("router-01")

	if err := upgrade.Run(context.Background(), cfg, target, "core-a", world.opts()); err != nil {
		t.Fatal(err)
	}

	job, err := state.Load(cfg.StateDir, "router-01")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != state.StatusComplete {
		t.Fatalf("status %q", job.Status)
	}
	if job.Stage != upgrade.StageComplete {
		t.Fatalf("stage %q", job.Stage)
	}
	if job.Release != target {
		t.Fatalf("release %q", job.Release)
	}

	if sim.reboots != 1 {
		t.Fatalf("reboots %d", sim.reboots)
	}
	if got := sim.uploadedRemotes(); strings.Join(got, ",") != "routeros-arm-6.49.21.npk,wireless-6.49.21-arm.npk" {
		t.Fatalf("uploads %v", got)
	}
	for _, remote := range sim.uploadedRemotes() {
		if !strings.HasSuffix(remote, ".npk") {
			t.Fatalf("upload %q", remote)
		}
	}
	for _, cmd := range sim.runs {
		if strings.Contains(cmd, "routerboard upgrade") {
			t.Fatalf("RouterBOOT upgrade ran: %s", cmd)
		}
	}
	if !contains(sim.runs, "/export hide-sensitive") {
		t.Fatal("missing text export")
	}
	if !containsPrefix(sim.runs, "/system backup save name=") {
		t.Fatal("missing binary backup")
	}
	if !contains(sim.runs, "/system reboot") {
		t.Fatal("missing reboot")
	}
	if !contains(sim.runs, "/system resource print") {
		t.Fatal("missing discover")
	}
	if !contains(sim.runs, validate.SystemLogCmd) {
		t.Fatal("missing system log check")
	}

	dir, err := validate.JobDir(cfg, "router-01", target)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := validate.ReadBaseline(dir)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Version != current {
		t.Fatalf("baseline version %q, want pre-reboot %q", baseline.Version, current)
	}
	if !baseline.SSHUp {
		t.Fatal("baseline ssh_up")
	}
	if baseline.CurrentFirmware == "" || baseline.UpgradeFirmware == "" {
		t.Fatalf("baseline firmware %q / %q", baseline.CurrentFirmware, baseline.UpgradeFirmware)
	}
	if string(baseline.RoleFacts) != "{}" {
		t.Fatalf("role_facts slot %s", baseline.RoleFacts)
	}
	if world.clock.now.Sub(world.clock.start) != 5*time.Minute {
		t.Fatalf("convergence wait %s, want profile 5m not reconnect 3m", world.clock.now.Sub(world.clock.start))
	}
}

func TestValidateRoleUsesProfileTimeoutNotReconnectBudget(t *testing.T) {
	cfg, world := setup(t, device("router-01", "core-a", 10, nil))
	cfg.Reconnect.Timeout = 3 * time.Minute
	writeProfile(t, cfg, "ospf", "convergence_timeout: 10m\n")

	if err := upgrade.Run(context.Background(), cfg, target, "core-a", world.opts()); err != nil {
		t.Fatal(err)
	}
	if world.clock.now.Sub(world.clock.start) != 10*time.Minute {
		t.Fatalf("elapsed %s, want 10m profile timeout", world.clock.now.Sub(world.clock.start))
	}
	for _, d := range world.clock.sleeps {
		if d == cfg.Reconnect.Timeout {
			t.Fatal("validation slept the reconnect budget")
		}
	}
}

func TestStagePackagesUploadsOnlyInstalledPackageNPKs(t *testing.T) {
	cfg, world := setup(t, device("router-01", "core-a", 10, nil))
	writeExtraNPK(t, cfg, target, release.File{
		Name:         "dhcp-6.49.21-arm.npk",
		Architecture: "arm",
		Package:      "dhcp",
		SHA256:       "abc",
		Size:         1000,
	})

	if err := upgrade.Run(context.Background(), cfg, target, "core-a", world.opts()); err != nil {
		t.Fatal(err)
	}
	got := world.sim("router-01").uploadedRemotes()
	if strings.Join(got, ",") != "routeros-arm-6.49.21.npk,wireless-6.49.21-arm.npk" {
		t.Fatalf("uploads %v", got)
	}
}

func TestCompleteSameReleaseSkipsSecondUpgrade(t *testing.T) {
	cfg, world := setup(t, device("router-01", "core-a", 10, nil))
	if err := upgrade.Run(context.Background(), cfg, target, "core-a", world.opts()); err != nil {
		t.Fatal(err)
	}
	sim := world.sim("router-01")
	sim.uploads = nil
	sim.runs = nil
	firstReboots := sim.reboots
	dialsAfterFirst := world.dialCount()
	if dialsAfterFirst == 0 {
		t.Fatal("expected first-run dials")
	}

	if err := upgrade.Run(context.Background(), cfg, target, "core-a", world.opts()); err != nil {
		t.Fatal(err)
	}
	if len(sim.uploads) != 0 {
		t.Fatalf("second run uploaded %v", sim.uploads)
	}
	if sim.reboots != firstReboots {
		t.Fatalf("second run rebooted, reboots %d", sim.reboots)
	}
	if world.dialCount() != dialsAfterFirst {
		t.Fatalf("skip dialed extra times: %d -> %d", dialsAfterFirst, world.dialCount())
	}
}

func TestResumeFromStage(t *testing.T) {
	tests := []struct {
		stage      string
		wantUpload bool
		wantReboot bool
	}{
		{stage: upgrade.StageDiscover, wantUpload: true, wantReboot: true},
		{stage: upgrade.StagePreflight, wantUpload: true, wantReboot: true},
		{stage: upgrade.StageExportText, wantUpload: true, wantReboot: true},
		{stage: upgrade.StageSaveBinaryBackup, wantUpload: true, wantReboot: true},
		{stage: upgrade.StagePackages, wantUpload: true, wantReboot: true},
		{stage: upgrade.StageReboot, wantUpload: false, wantReboot: true},
		{stage: upgrade.StageWaitForReconnect, wantUpload: false, wantReboot: false},
		{stage: upgrade.StageValidateRole, wantUpload: false, wantReboot: false},
		{stage: upgrade.StageComplete, wantUpload: false, wantReboot: false},
	}
	for _, tc := range tests {
		t.Run(tc.stage, func(t *testing.T) {
			cfg, world := setup(t, device("router-01", "core-a", 10, nil))
			sim := world.sim("router-01")
			upgraded := stageIndex(tc.stage) >= stageIndex(upgrade.StageWaitForReconnect)
			if upgraded {
				sim.version = target
				sim.rebooted = true
			}
			job := &state.DeviceJob{
				Device:  "router-01",
				Release: target,
				Group:   "core-a",
				Stage:   tc.stage,
				Status:  state.StatusInProgress,
				Facts:   factsJSON(t, sampleFacts(current)),
			}
			if err := state.Save(cfg.StateDir, job); err != nil {
				t.Fatal(err)
			}
			if stageIndex(tc.stage) >= stageIndex(upgrade.StageWaitForReconnect) {
				writeJobBaseline(t, cfg, "router-01", sampleFacts(current))
			}

			if err := upgrade.Run(context.Background(), cfg, target, "core-a", world.opts()); err != nil {
				t.Fatal(err)
			}

			got, err := state.Load(cfg.StateDir, "router-01")
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != state.StatusComplete {
				t.Fatalf("status %q", got.Status)
			}
			if got.Stage != upgrade.StageComplete {
				t.Fatalf("stage %q", got.Stage)
			}
			if tc.wantUpload && len(sim.uploads) == 0 {
				t.Fatal("expected package upload")
			}
			if !tc.wantUpload && len(sim.uploads) != 0 {
				t.Fatalf("re-uploaded from %s: %v", tc.stage, sim.uploads)
			}
			if tc.wantReboot && sim.reboots == 0 {
				t.Fatal("expected reboot")
			}
			if !tc.wantReboot && sim.reboots != 0 {
				t.Fatalf("rebooted from %s", tc.stage)
			}
		})
	}
}

func TestFirstFailedStopsTheGroup(t *testing.T) {
	cfg, world := setup(t,
		device("router-01", "core-a", 10, nil),
		device("router-02", "core-a", 20, nil),
	)
	world.sim("router-01").dialErr = errors.New("ssh down")

	err := upgrade.Run(context.Background(), cfg, target, "core-a", world.opts())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "router-01") {
		t.Fatalf("got %v", err)
	}

	job, err := state.Load(cfg.StateDir, "router-01")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != state.StatusFailed {
		t.Fatalf("status %q", job.Status)
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, "router-02.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("router-02 should not have started: %v", err)
	}
	if world.lookups["192.0.2.2"] != 0 {
		t.Fatalf("dialed second device %d times", world.lookups["192.0.2.2"])
	}
}

func TestDependsOnMustAlreadyBeComplete(t *testing.T) {
	cfg, world := setup(t,
		device("core-1", "core", 10, nil),
		device("router-01", "edge", 10, []string{"core-1"}),
		device("router-02", "edge", 20, nil),
	)

	err := upgrade.Run(context.Background(), cfg, target, "edge", world.opts())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "depends_on") {
		t.Fatalf("got %v", err)
	}

	job, err := state.Load(cfg.StateDir, "router-01")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != state.StatusFailed {
		t.Fatalf("status %q", job.Status)
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, "router-02.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("router-02 should not have started: %v", err)
	}
	if world.dialCount() != 0 {
		t.Fatalf("dialed %d times", world.dialCount())
	}
}

func TestReconnectTimeoutMarksFailedWithoutDowngrade(t *testing.T) {
	cfg, world := setup(t, device("router-01", "core-a", 10, nil))
	cfg.Reconnect.Timeout = 3 * time.Minute
	cfg.Reconnect.Attempts = 3
	sim := world.sim("router-01")
	sim.version = target
	sim.rebooted = true
	sim.reconnectFailsLeft = 100
	job := &state.DeviceJob{
		Device:  "router-01",
		Release: target,
		Group:   "core-a",
		Stage:   upgrade.StageWaitForReconnect,
		Status:  state.StatusInProgress,
		Facts:   factsJSON(t, sampleFacts(current)),
	}
	if err := state.Save(cfg.StateDir, job); err != nil {
		t.Fatal(err)
	}
	writeJobBaseline(t, cfg, "router-01", sampleFacts(current))
	world.clock.jump = cfg.Reconnect.Timeout

	err := upgrade.Run(context.Background(), cfg, target, "core-a", world.opts())
	if err == nil {
		t.Fatal("expected error")
	}

	got, err := state.Load(cfg.StateDir, "router-01")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusFailed {
		t.Fatalf("status %q", got.Status)
	}
	if got.Stage != upgrade.StageWaitForReconnect {
		t.Fatalf("stage %q", got.Stage)
	}
	if len(sim.uploads) != 0 {
		t.Fatalf("downgrade/upload after timeout: %v", sim.uploads)
	}
	if sim.reboots != 0 {
		t.Fatalf("extra reboot %d", sim.reboots)
	}
	if containsPrefix(sim.runs, "/system package") && contains(sim.runs, "downgrade") {
		t.Fatal("attempted downgrade")
	}
	if world.clock.now.Sub(world.clock.start) < cfg.Reconnect.Timeout {
		t.Fatalf("timeout did not consume reconnect budget: elapsed %s", world.clock.now.Sub(world.clock.start))
	}
}

func TestReconnectSucceedsOnThirdAttempt(t *testing.T) {
	cfg, world := setup(t, device("router-01", "core-a", 10, nil))
	sim := world.sim("router-01")
	sim.reconnectFailsLeft = 2

	if err := upgrade.Run(context.Background(), cfg, target, "core-a", world.opts()); err != nil {
		t.Fatal(err)
	}
	job, err := state.Load(cfg.StateDir, "router-01")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != state.StatusComplete {
		t.Fatalf("status %q", job.Status)
	}
	if sim.reconnectAttempts < 3 {
		t.Fatalf("reconnect attempts %d", sim.reconnectAttempts)
	}
}

func TestAuditPushFailureDoesNotUncomplete(t *testing.T) {
	cfg, world := setup(t, device("router-01", "core-a", 10, nil))
	opts := world.opts()
	opts.Push = func(context.Context, *config.Config, *state.DeviceJob, string, auditgit.Artifacts) error {
		return errors.New("push denied")
	}

	err := upgrade.Run(context.Background(), cfg, target, "core-a", opts)
	if err == nil {
		t.Fatal("expected audit error")
	}
	if !strings.Contains(err.Error(), "push denied") {
		t.Fatalf("got %v", err)
	}

	job, err := state.Load(cfg.StateDir, "router-01")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != state.StatusComplete {
		t.Fatalf("status %q", job.Status)
	}
	if job.Stage != upgrade.StageComplete {
		t.Fatalf("stage %q", job.Stage)
	}
}

func TestValidateRoleRequiresTargetVersionAndPackages(t *testing.T) {
	cfg, world := setup(t, device("router-01", "core-a", 10, nil))
	sim := world.sim("router-01")
	sim.applyOnReboot = false

	err := upgrade.Run(context.Background(), cfg, target, "core-a", world.opts())
	if err == nil {
		t.Fatal("expected error")
	}

	job, err := state.Load(cfg.StateDir, "router-01")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != state.StatusFailed {
		t.Fatalf("status %q", job.Status)
	}
	if job.Stage != upgrade.StageValidateRole {
		t.Fatalf("stage %q", job.Stage)
	}
}

type fakeClock struct {
	start  time.Time
	now    time.Time
	jump   time.Duration
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(d time.Duration) {
	c.sleeps = append(c.sleeps, d)
	if c.jump > 0 {
		c.now = c.now.Add(c.jump)
		return
	}
	c.now = c.now.Add(d)
}

type fakeWorld struct {
	t       *testing.T
	cfg     *config.Config
	clock   *fakeClock
	sims    map[string]*deviceSim
	lookups map[string]int
}

func (w *fakeWorld) opts() upgrade.Options {
	return upgrade.Options{
		Dial:  w.dial,
		Clock: w.clock,
		Push:  nopPush,
	}
}

func (w *fakeWorld) sim(name string) *deviceSim {
	for _, s := range w.sims {
		if s.name == name {
			return s
		}
	}
	w.t.Fatalf("missing sim %s", name)
	return nil
}

func (w *fakeWorld) dialCount() int {
	n := 0
	for _, v := range w.lookups {
		n += v
	}
	return n
}

func (w *fakeWorld) dial(ctx context.Context, _ config.SSHConfig, address string, _ int) (transport.Client, error) {
	w.lookups[address]++
	sim, ok := w.sims[address]
	if !ok {
		return nil, fmt.Errorf("no device at %s", address)
	}
	if sim.dialErr != nil {
		return nil, sim.dialErr
	}
	if sim.rebooted && sim.reconnectFailsLeft > 0 {
		sim.reconnectAttempts++
		sim.reconnectFailsLeft--
		return nil, errors.New("connection refused")
	}
	if sim.rebooted {
		sim.reconnectAttempts++
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return sim, nil
}

type deviceSim struct {
	name               string
	version            string
	target             string
	packages           []string
	applyOnReboot      bool
	rebooted           bool
	reboots            int
	uploads            [][2]string
	runs               []string
	files              map[string][]byte
	dialErr            error
	reconnectFailsLeft int
	reconnectAttempts  int
}

func (s *deviceSim) Run(_ context.Context, command string) (string, error) {
	s.runs = append(s.runs, command)
	switch {
	case command == "/system resource print":
		return resourcePrint(s.version), nil
	case command == "/system package print":
		return packagePrint(s.version, s.packages), nil
	case command == "/system routerboard print":
		return routerboardPrint(), nil
	case command == "/system identity print":
		return "  name: " + s.name + "\n", nil
	case command == "/export hide-sensitive":
		return "/ip address print\n", nil
	case strings.HasPrefix(command, "/system backup save name="):
		name, ok := backupNameFromSave(command)
		if !ok {
			return "", errors.New("malformed backup save")
		}
		if s.files == nil {
			s.files = map[string][]byte{}
		}
		s.files[name+".backup"] = []byte("binary-backup")
		return "", nil
	case command == "/system reboot":
		s.reboots++
		s.rebooted = true
		if s.applyOnReboot {
			s.version = s.target
		}
		return "", errors.New("connection reset by peer")
	case command == validate.SystemLogCmd:
		return systemLogPrint(s.version, s.packages), nil
	default:
		return "", fmt.Errorf("unexpected command %q", command)
	}
}

func (s *deviceSim) Upload(_ context.Context, local, remote string) error {
	if _, err := os.Stat(local); err != nil {
		return err
	}
	s.uploads = append(s.uploads, [2]string{local, remote})
	return nil
}

func (s *deviceSim) Download(_ context.Context, remote, local string) error {
	data, ok := s.files[remote]
	if !ok {
		return errors.New("missing remote " + remote)
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o700); err != nil {
		return err
	}
	return os.WriteFile(local, data, 0o600)
}

func (s *deviceSim) Remove(_ context.Context, remote string) error {
	delete(s.files, remote)
	return nil
}

func (s *deviceSim) Close() error { return nil }

func (s *deviceSim) uploadedRemotes() []string {
	out := make([]string, 0, len(s.uploads))
	for _, u := range s.uploads {
		out = append(out, filepath.Base(u[1]))
	}
	return out
}

func nopPush(context.Context, *config.Config, *state.DeviceJob, string, auditgit.Artifacts) error {
	return nil
}

func setup(t *testing.T, devices ...inventory.Device) (*config.Config, *fakeWorld) {
	t.Helper()
	root := t.TempDir()
	ops := filepath.Join(root, "ops")
	inv := filepath.Join(ops, "inventory", "devices.yaml")
	if err := os.MkdirAll(filepath.Dir(inv), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inv, []byte(inventoryYAML(devices)), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DataDir:             filepath.Join(root, "data"),
		StateDir:            filepath.Join(root, "state"),
		PackageDir:          filepath.Join(root, "packages"),
		BackupDir:           filepath.Join(root, "backups"),
		BackupRetentionDays: 30,
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
	writeDefaultProfiles(t, cfg)
	writeRelease(t, cfg, target, []release.File{
		npk("routeros", "arm"),
		npk("wireless", "arm"),
	})
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	world := &fakeWorld{
		t:       t,
		cfg:     cfg,
		clock:   &fakeClock{start: now, now: now},
		sims:    map[string]*deviceSim{},
		lookups: map[string]int{},
	}
	for i, d := range devices {
		addr := d.Address
		if addr == "" {
			addr = fmt.Sprintf("192.0.2.%d", i+1)
		}
		world.sims[addr] = &deviceSim{
			name:          d.Name,
			version:       current,
			target:        target,
			packages:      []string{"routeros", "wireless"},
			applyOnReboot: true,
		}
	}
	return cfg, world
}

func writeRelease(t *testing.T, cfg *config.Config, version string, files []release.File) {
	t.Helper()
	dir := filepath.Join(cfg.PackageDir, version)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	man := release.Manifest{
		Version:       version,
		Channel:       "long-term",
		Architectures: []string{"arm"},
		Files:         files,
	}
	raw, err := json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.Name), []byte("npk"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeExtraNPK(t *testing.T, cfg *config.Config, version string, f release.File) {
	t.Helper()
	man, err := release.Load(cfg.PackageDir, version)
	if err != nil {
		t.Fatal(err)
	}
	man.Files = append(man.Files, f)
	raw, err := json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cfg.PackageDir, version)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, f.Name), []byte("npk"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func npk(pkg, arch string) release.File {
	name := pkg + "-" + target + "-" + arch + ".npk"
	if pkg == "routeros" {
		name = "routeros-" + arch + "-" + target + ".npk"
	}
	return release.File{
		Name:         name,
		Architecture: arch,
		Package:      pkg,
		SHA256:       "abc",
		Size:         1000,
	}
}

func device(name, group string, order int, deps []string) inventory.Device {
	addr := "192.0.2.1"
	switch name {
	case "router-02":
		addr = "192.0.2.2"
	case "core-1":
		addr = "192.0.2.10"
	}
	role := "ospf"
	if name == "core-1" {
		role = "switch"
	}
	return inventory.Device{
		Name:              name,
		Address:           addr,
		Role:              role,
		Group:             group,
		Order:             order,
		ValidationProfile: role,
		DependsOn:         deps,
	}
}

func writeDefaultProfiles(t *testing.T, cfg *config.Config) {
	t.Helper()
	profiles := map[string]string{
		"ospf":   "convergence_timeout: 5m\n",
		"pppoe":  "convergence_timeout: 10m\n",
		"radio":  "convergence_timeout: 5m\n",
		"switch": "convergence_timeout: 2m\n",
		"access": "convergence_timeout: 2m\n",
	}
	for role, body := range profiles {
		writeProfile(t, cfg, role, body)
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

func writeJobBaseline(t *testing.T, cfg *config.Config, device string, facts discover.Facts) {
	t.Helper()
	dir, err := validate.JobDir(cfg, device, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := validate.WriteBaseline(dir, validate.FromFacts(device, facts, true, nil)); err != nil {
		t.Fatal(err)
	}
}

func systemLogPrint(version string, pkgs []string) string {
	var b strings.Builder
	b.WriteString("  jan/02/1970 00:06:31 system,info,account user rosup logged in from 192.0.2.1 via ssh\n")
	for _, p := range pkgs {
		fmt.Fprintf(&b, "  jan/02/1970 00:07:54 system,info installed %s-%s\n", p, version)
	}
	return b.String()
}

func inventoryYAML(devices []inventory.Device) string {
	var b strings.Builder
	b.WriteString("devices:\n")
	for _, d := range devices {
		fmt.Fprintf(&b, "  - name: %s\n    address: %s\n    role: %s\n    group: %s\n    order: %d\n    validation_profile: %s\n",
			d.Name, d.Address, d.Role, d.Group, d.Order, d.ValidationProfile)
		if len(d.DependsOn) > 0 {
			b.WriteString("    depends_on:\n")
			for _, dep := range d.DependsOn {
				fmt.Fprintf(&b, "      - %s\n", dep)
			}
		}
	}
	return b.String()
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

func factsJSON(t *testing.T, facts discover.Facts) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func resourcePrint(version string) string {
	return fmt.Sprintf(`                  version: %s (long-term)
           free-hdd-space: 10.0MiB
        architecture-name: arm
               board-name: hAP ac^2
`, version)
}

func packagePrint(version string, pkgs []string) string {
	var b strings.Builder
	for i, p := range pkgs {
		fmt.Fprintf(&b, " %d   %s                %s\n", i, p, version)
	}
	return b.String()
}

func routerboardPrint() string {
	return `        board-name: hAP ac^2
  current-firmware: 6.49.13
  upgrade-firmware: 6.49.18
`
}

func backupNameFromSave(command string) (string, bool) {
	const prefix = "/system backup save name="
	if !strings.HasPrefix(command, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(command, prefix)
	name, _, ok := strings.Cut(rest, " ")
	if !ok || name == "" {
		return "", false
	}
	return name, true
}

func contains(cmds []string, want string) bool {
	for _, c := range cmds {
		if c == want {
			return true
		}
	}
	return false
}

func containsPrefix(cmds []string, prefix string) bool {
	for _, c := range cmds {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func stageIndex(stage string) int {
	for i, s := range []string{
		upgrade.StageDiscover,
		upgrade.StagePreflight,
		upgrade.StageExportText,
		upgrade.StageSaveBinaryBackup,
		upgrade.StagePackages,
		upgrade.StageReboot,
		upgrade.StageWaitForReconnect,
		upgrade.StageValidateRole,
		upgrade.StageComplete,
	} {
		if s == stage {
			return i
		}
	}
	return -1
}

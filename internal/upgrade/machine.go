package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/taihen/rosup/internal/auditgit"
	"github.com/taihen/rosup/internal/backup"
	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/inventory"
	"github.com/taihen/rosup/internal/preflight"
	"github.com/taihen/rosup/internal/progress"
	"github.com/taihen/rosup/internal/redact"
	"github.com/taihen/rosup/internal/release"
	"github.com/taihen/rosup/internal/rosname"
	"github.com/taihen/rosup/internal/routerboot"
	"github.com/taihen/rosup/internal/state"
	"github.com/taihen/rosup/internal/transport"
	"github.com/taihen/rosup/internal/validate"
)

const (
	StageDiscover                = "DISCOVER"
	StagePreflight               = "PREFLIGHT"
	StageExportText              = "EXPORT_TEXT"
	StageSaveBinaryBackup        = "SAVE_BINARY_BACKUP"
	StagePackages                = "STAGE_PACKAGES"
	StageReboot                  = "REBOOT"
	StageWaitForReconnect        = "WAIT_FOR_RECONNECT"
	StageValidateRole            = "VALIDATE_ROLE"
	StageRouterbootUpdate        = "ROUTERBOOT_UPDATE"
	StageRebootRouterboot        = "REBOOT_ROUTERBOOT"
	StageValidateAfterRouterboot = "VALIDATE_ROLE_AFTER_ROUTERBOOT"
	StageComplete                = "COMPLETE"
	cmdReboot                    = "/system reboot"
)

var stages = []string{
	StageDiscover,
	StagePreflight,
	StageExportText,
	StageSaveBinaryBackup,
	StagePackages,
	StageReboot,
	StageWaitForReconnect,
	StageValidateRole,
	StageRouterbootUpdate,
	StageRebootRouterboot,
	StageValidateAfterRouterboot,
	StageComplete,
}

type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

type Options struct {
	Dial   discover.DialFunc
	Clock  Clock
	Push   func(ctx context.Context, cfg *config.Config, job *state.DeviceJob, jobID string, artifacts auditgit.Artifacts) error
	Out    io.Writer
	Resume bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) Sleep(d time.Duration) { time.Sleep(d) }

func Run(ctx context.Context, cfg *config.Config, version, group string, opts Options) error {
	if cfg == nil {
		return errors.New("upgrade: nil config")
	}
	if version == "" {
		return errors.New("upgrade: --release is required")
	}
	opts = applyDefaults(opts)

	man, err := release.Load(cfg.PackageDir, version)
	if err != nil {
		return err
	}
	devices, err := inventory.Load(cfg)
	if err != nil {
		return err
	}
	devices, err = filterGroup(devices, group)
	if err != nil {
		return err
	}

	names := make([]string, len(devices))
	for i, d := range devices {
		names[i] = d.Name
	}
	printer := progress.New(opts.Out, names)

	for _, d := range devices {
		if err := runDevice(ctx, cfg, version, d, man, opts, printer); err != nil {
			return err
		}
	}
	return nil
}

func applyDefaults(opts Options) Options {
	if opts.Dial == nil {
		opts.Dial = transport.Dial
	}
	if opts.Clock == nil {
		opts.Clock = realClock{}
	}
	if opts.Push == nil {
		opts.Push = auditgit.Push
	}
	return opts
}

func runDevice(ctx context.Context, cfg *config.Config, version string, d inventory.Device, man release.Manifest, opts Options, printer *progress.Printer) error {
	job, err := loadOrNew(cfg.StateDir, d)
	if err != nil {
		return err
	}
	if job.Status == state.StatusComplete && job.Release == version {
		printer.Skip(d.Name, "already "+version)
		return nil
	}
	if job.Status == state.StatusComplete {
		// Prior release finished; start a fresh job for this release.
		job = &state.DeviceJob{
			Device: d.Name,
			Group:  d.Group,
			Facts:  job.Facts,
		}
	}

	switch {
	case job.Status == state.StatusInProgress || job.Status == state.StatusFailed:
		if job.Release != "" && job.Release != version {
			return fmt.Errorf("upgrade: %s: incomplete job is for release %s, not %s", d.Name, job.Release, version)
		}
		if !opts.Resume {
			return fmt.Errorf("upgrade: %s: job is %s at stage %s; re-run with --resume", d.Name, job.Status, job.Stage)
		}
	case opts.Resume:
		printer.Skip(d.Name, "nothing to resume")
		return nil
	}

	if err := checkDepends(cfg.StateDir, d, version); err != nil {
		job.Release = version
		job.Group = d.Group
		return markFailed(cfg.StateDir, job, err)
	}

	job.Release = version
	job.Group = d.Group
	job.Attempt++
	if err := state.MarkStarted(cfg.StateDir, job); err != nil {
		return err
	}

	r := &deviceRun{
		ctx:      ctx,
		cfg:      cfg,
		version:  version,
		d:        d,
		man:      man,
		opts:     opts,
		job:      job,
		progress: printer,
	}
	defer r.closeClient()

	start, err := startIndex(job.Stage)
	if err != nil {
		return markFailed(cfg.StateDir, job, fmt.Errorf("upgrade: %s: %w", d.Name, err))
	}
	// Refresh live facts before package/reboot decisions on resume. Do not probe
	// while waiting for reconnect — the device may still be down.
	if pkgIdx, _ := startIndex(StagePackages); start > 0 && start <= pkgIdx {
		if err := r.discover(); err != nil {
			return markFailed(cfg.StateDir, job, err)
		}
	}

	skippedCurrent := false
	for i := start; i < len(stages); i++ {
		st := stages[i]
		if r.skipBecauseCurrent(st) {
			if !skippedCurrent {
				r.progress.Skip(d.Name, "already "+version)
				skippedCurrent = true
			}
			continue
		}
		if skip, err := r.skipRouterbootStage(st); err != nil {
			return markFailed(cfg.StateDir, job, err)
		} else if skip {
			if st == StageRouterbootUpdate {
				r.progress.Skip(d.Name, "RouterBOOT already current")
			}
			continue
		}
		if err := state.Advance(cfg.StateDir, job, st); err != nil {
			return err
		}
		if err := r.step(st); err != nil {
			if st == StageComplete {
				r.job.LastError = redact.String(err.Error())
				r.job.UpdatedAt = time.Now().UTC()
				_ = state.Save(r.cfg.StateDir, r.job)
				return err
			}
			if isValidationStage(st) {
				err = r.maybeDowngrade(err)
				if r.job.Status == state.StatusComplete {
					return err
				}
			}
			return markFailed(cfg.StateDir, job, err)
		}
	}
	return nil
}

type deviceRun struct {
	ctx        context.Context
	cfg        *config.Config
	version    string
	d          inventory.Device
	man        release.Manifest
	opts       Options
	job        *state.DeviceJob
	client     transport.Client
	backedUp   bool
	export     string
	skipRB     *bool
	downgraded bool
	progress   *progress.Printer
}

func (r *deviceRun) tracked(label string, fn func() error) error {
	return r.progress.Track(r.d.Name, label, fn)
}

func (r *deviceRun) step(st string) error {
	switch st {
	case StageDiscover:
		return r.tracked("checking SSH and version", r.discover)
	case StagePreflight:
		return r.tracked("checking this release", r.preflight)
	case StageExportText, StageSaveBinaryBackup:
		if r.backedUp {
			return nil
		}
		return r.tracked("saving backups", r.backup)
	case StagePackages:
		return r.tracked("installing packages", r.stagePackages)
	case StageReboot:
		return r.tracked("rebooting", r.reboot)
	case StageWaitForReconnect:
		return waitForReconnect(r.ctx, r.cfg, r.d, r.opts, r.progress)
	case StageValidateRole:
		return r.checkRoleAt(r.version, false, "")
	case StageRouterbootUpdate:
		return r.tracked("updating RouterBOOT", r.routerbootUpdate)
	case StageRebootRouterboot:
		return r.tracked("rebooting for RouterBOOT", r.rebootRouterboot)
	case StageValidateAfterRouterboot:
		if err := waitForReconnect(r.ctx, r.cfg, r.d, r.opts, r.progress); err != nil {
			return err
		}
		return r.checkRoleAt(r.version, true, " after RouterBOOT")
	case StageComplete:
		return r.tracked("writing audit", r.complete)
	default:
		return fmt.Errorf("upgrade: %s: unknown stage %s", r.d.Name, st)
	}
}

func (r *deviceRun) discover() error {
	facts, err := discover.Probe(r.ctx, r.cfg, r.d, r.opts.Dial)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return fmt.Errorf("upgrade: %s: marshal facts: %w", r.d.Name, err)
	}
	r.job.Facts = raw
	return state.Save(r.cfg.StateDir, r.job)
}

func (r *deviceRun) preflight() error {
	facts, err := factsFrom(r.job)
	if err != nil {
		return fmt.Errorf("upgrade: %s: %w", r.d.Name, err)
	}
	return preflight.Check(facts, r.man)
}

func (r *deviceRun) backup() error {
	if r.backedUp {
		return nil
	}
	if err := r.ensureClient(); err != nil {
		return err
	}
	got, err := backup.ExportAndBackup(r.ctx, r.cfg, r.d.Name, r.client)
	if err != nil {
		return err
	}
	r.export = got.Export
	r.job.ExportPath = got.ExportPath
	r.job.BackupPath = got.BackupPath
	r.backedUp = true
	return state.Save(r.cfg.StateDir, r.job)
}

func (r *deviceRun) stagePackages() error {
	facts, err := factsFrom(r.job)
	if err != nil {
		return fmt.Errorf("upgrade: %s: %w", r.d.Name, err)
	}
	files, err := packagesToStage(facts, r.man)
	if err != nil {
		return fmt.Errorf("upgrade: %s: %w", r.d.Name, err)
	}
	if err := r.ensureClient(); err != nil {
		return err
	}
	return r.uploadFiles(r.version, files)
}

func (r *deviceRun) uploadFiles(version string, files []release.File) error {
	dir := filepath.Join(r.cfg.PackageDir, version)
	for _, f := range files {
		if err := release.VerifyLocalFile(dir, f); err != nil {
			return fmt.Errorf("upgrade: %s: %w", r.d.Name, err)
		}
		local := filepath.Join(dir, f.Name)
		if err := r.client.Upload(r.ctx, local, f.Name); err != nil {
			r.closeClient()
			return fmt.Errorf("upgrade: %s: upload %s: %w", r.d.Name, f.Name, err)
		}
	}
	return nil
}

func (r *deviceRun) reboot() error {
	if err := r.ensureClient(); err != nil {
		return err
	}
	if err := r.snapshotBaseline(); err != nil {
		return err
	}
	_, err := r.client.Run(r.ctx, cmdReboot)
	r.closeClient()
	if err != nil && r.ctx.Err() != nil {
		return fmt.Errorf("upgrade: %s: %s: %w", r.d.Name, cmdReboot, r.ctx.Err())
	}
	if err != nil && !rosname.IsExpectedDisconnect(err) {
		return fmt.Errorf("upgrade: %s: %s: %w", r.d.Name, cmdReboot, err)
	}
	return nil
}

func (r *deviceRun) snapshotBaseline() error {
	facts, err := factsFrom(r.job)
	if err != nil {
		return fmt.Errorf("upgrade: %s: %w", r.d.Name, err)
	}
	dir, err := validate.JobDir(r.cfg, r.d.Name, r.version)
	if err != nil {
		return fmt.Errorf("upgrade: %s: %w", r.d.Name, err)
	}
	profile := r.d.ValidationProfile
	if profile == "" {
		profile = r.d.Role
	}
	roleFacts, err := validate.CaptureRoleFacts(r.ctx, r.client, profile)
	if err != nil {
		return fmt.Errorf("upgrade: %s: %w", r.d.Name, err)
	}
	return validate.WriteBaseline(dir, validate.FromFacts(r.d.Name, facts, true, roleFacts))
}

func (r *deviceRun) skipBecauseCurrent(st string) bool {
	switch st {
	case StageDiscover, StageComplete,
		StageRouterbootUpdate, StageRebootRouterboot, StageValidateAfterRouterboot:
		return false
	}
	facts, err := factsFrom(r.job)
	if err != nil {
		return false
	}
	return preflight.AlreadyOnRelease(facts, r.version)
}

func (r *deviceRun) skipRouterbootStage(st string) (bool, error) {
	switch st {
	case StageRouterbootUpdate:
		newer, err := r.firmwareNewer()
		if err != nil {
			return false, err
		}
		skip := !newer
		r.skipRB = &skip
		return skip, nil
	case StageRebootRouterboot, StageValidateAfterRouterboot:
		return r.skipRB != nil && *r.skipRB, nil
	default:
		return false, nil
	}
}

func (r *deviceRun) firmwareNewer() (bool, error) {
	if err := r.ensureClient(); err != nil {
		return false, err
	}
	out, err := r.client.Run(r.ctx, "/system routerboard print")
	if err != nil {
		return false, fmt.Errorf("upgrade: %s: /system routerboard print: %w", r.d.Name, err)
	}
	current, upgradeFW := routerboot.ParseFirmware(out)
	return routerboot.Compare(upgradeFW, current)
}

func (r *deviceRun) routerbootUpdate() error {
	if err := r.ensureClient(); err != nil {
		return err
	}
	if err := r.ensureBaseline(); err != nil {
		return err
	}
	if _, err := r.client.Run(r.ctx, routerboot.CmdUpgrade); err != nil {
		return fmt.Errorf("upgrade: %s: %s: %w", r.d.Name, routerboot.CmdUpgrade, err)
	}
	return nil
}

func (r *deviceRun) ensureBaseline() error {
	dir, err := validate.JobDir(r.cfg, r.d.Name, r.version)
	if err != nil {
		return fmt.Errorf("upgrade: %s: %w", r.d.Name, err)
	}
	if _, err := validate.ReadBaseline(dir); err == nil {
		return nil
	}
	return r.snapshotBaseline()
}

func (r *deviceRun) rebootRouterboot() error {
	if err := r.ensureClient(); err != nil {
		return err
	}
	_, err := r.client.Run(r.ctx, cmdReboot)
	r.closeClient()
	if err != nil && r.ctx.Err() != nil {
		return fmt.Errorf("upgrade: %s: %s: %w", r.d.Name, cmdReboot, r.ctx.Err())
	}
	if err != nil && !rosname.IsExpectedDisconnect(err) {
		return fmt.Errorf("upgrade: %s: %s: %w", r.d.Name, cmdReboot, err)
	}
	return nil
}

func (r *deviceRun) checkRoleAt(target string, upgradeFirmware bool, suffix string) error {
	profile := r.d.ValidationProfile
	if profile == "" {
		profile = r.d.Role
	}
	return validate.Check(r.ctx, validate.Request{
		Config:          r.cfg,
		Device:          r.d,
		Target:          target,
		Dial:            r.opts.Dial,
		Clock:           r.opts.Clock,
		Profile:         profile,
		UpgradeFirmware: upgradeFirmware,
		RoleCheck:       validate.RoleChecks[profile],
		Progress:        r.progress,
		LabelSuffix:     suffix,
	})
}

func isValidationStage(st string) bool {
	return st == StageValidateRole || st == StageValidateAfterRouterboot
}

func (r *deviceRun) maybeDowngrade(cause error) error {
	if r.downgraded {
		return cause
	}
	if errors.Is(cause, validate.ErrUnreachable) {
		return cause
	}
	r.downgraded = true
	facts, err := factsFrom(r.job)
	if err != nil {
		return fmt.Errorf("%w (downgrade: %v)", cause, err)
	}
	prev := facts.Version
	if err := r.downgradeToPrevious(); err != nil {
		return fmt.Errorf("%w (downgrade: %v)", cause, err)
	}
	// Device is healthy on the previous release; record that so --resume does
	// not keep validating the abandoned target against a restored box.
	r.job.Release = prev
	r.job.Status = state.StatusComplete
	r.job.Stage = StageComplete
	r.job.LastError = redact.String(fmt.Sprintf("validation failed after upgrade; restored %s: %v", prev, cause))
	r.job.UpdatedAt = time.Now().UTC()
	if err := state.Save(r.cfg.StateDir, r.job); err != nil {
		return fmt.Errorf("%w (downgrade save: %v)", cause, err)
	}
	return fmt.Errorf("%w (restored %s)", cause, prev)
}

func (r *deviceRun) downgradeToPrevious() error {
	facts, err := factsFrom(r.job)
	if err != nil {
		return err
	}
	prev := facts.Version
	if prev == "" || prev == r.version {
		return fmt.Errorf("no previous release to restore")
	}
	man, err := release.Load(r.cfg.PackageDir, prev)
	if err != nil {
		return err
	}
	files, err := packagesToStage(facts, man)
	if err != nil {
		return err
	}
	r.closeClient()
	if err := r.ensureClient(); err != nil {
		return err
	}
	if err := r.tracked("restoring previous release", func() error {
		if err := r.uploadFiles(prev, files); err != nil {
			return err
		}
		_, err := r.client.Run(r.ctx, cmdReboot)
		r.closeClient()
		if err != nil && r.ctx.Err() != nil {
			return fmt.Errorf("upgrade: %s: %s: %w", r.d.Name, cmdReboot, r.ctx.Err())
		}
		if err != nil && !rosname.IsExpectedDisconnect(err) {
			return fmt.Errorf("upgrade: %s: %s: %w", r.d.Name, cmdReboot, err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := waitForReconnect(r.ctx, r.cfg, r.d, r.opts, r.progress); err != nil {
		return err
	}
	if err := r.copyBaseline(prev); err != nil {
		return err
	}
	return r.checkRoleAt(prev, false, "")
}

func (r *deviceRun) copyBaseline(prev string) error {
	src, err := validate.JobDir(r.cfg, r.d.Name, r.version)
	if err != nil {
		return err
	}
	baseline, err := validate.ReadBaseline(src)
	if err != nil {
		return err
	}
	dst, err := validate.JobDir(r.cfg, r.d.Name, prev)
	if err != nil {
		return err
	}
	return validate.WriteBaseline(dst, baseline)
}

func (r *deviceRun) complete() error {
	export, err := r.exportForAudit()
	if err != nil {
		return err
	}
	// Stay in_progress until the audit push succeeds so --resume can retry.
	r.job.Status = state.StatusInProgress
	r.job.Stage = StageComplete
	r.job.LastError = ""
	r.job.UpdatedAt = time.Now().UTC()
	if err := state.Save(r.cfg.StateDir, r.job); err != nil {
		return err
	}
	auditJob := *r.job
	auditJob.Status = state.StatusComplete
	if err := r.opts.Push(r.ctx, r.cfg, &auditJob, r.version, auditgit.Artifacts{
		Export: export,
		Result: &auditJob,
		Log:    r.job.Stage,
	}); err != nil {
		return fmt.Errorf("upgrade: %s: audit: %w", r.d.Name, err)
	}
	r.job.Status = state.StatusComplete
	r.job.UpdatedAt = time.Now().UTC()
	return state.Save(r.cfg.StateDir, r.job)
}

func (r *deviceRun) exportForAudit() (string, error) {
	if r.export != "" || r.job.ExportPath == "" {
		return r.export, nil
	}
	data, err := os.ReadFile(r.job.ExportPath)
	if err != nil {
		return "", fmt.Errorf("upgrade: %s: read export %s: %w", r.d.Name, r.job.ExportPath, err)
	}
	r.export = string(data)
	return r.export, nil
}

func (r *deviceRun) ensureClient() error {
	if r.client != nil {
		return nil
	}
	port := r.d.Port
	if port == 0 {
		port = r.cfg.SSH.DefaultPort
	}
	client, err := r.opts.Dial(r.ctx, r.cfg.SSH, r.d.Address, port)
	if err != nil {
		return fmt.Errorf("upgrade: %s: %w", r.d.Name, err)
	}
	if err := verifyDialIdentity(r.ctx, client, r.d.Name); err != nil {
		_ = client.Close()
		return fmt.Errorf("upgrade: %s: %w", r.d.Name, err)
	}
	r.client = client
	return nil
}

func (r *deviceRun) closeClient() {
	if r.client == nil {
		return
	}
	_ = r.client.Close()
	r.client = nil
}

func waitForReconnect(ctx context.Context, cfg *config.Config, d inventory.Device, opts Options, printer *progress.Printer) error {
	attempts := cfg.Reconnect.Attempts
	timeout := cfg.Reconnect.Timeout
	if attempts <= 0 {
		attempts = 3
	}
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	waiting := "waiting for SSH"
	printer.Start(d.Name, waiting+" ("+progress.FormatDuration(timeout)+")")
	deadline := opts.Clock.Now().Add(timeout)
	port := d.Port
	if port == 0 {
		port = cfg.SSH.DefaultPort
	}

	var last error
	for n := 1; n <= attempts; n++ {
		if err := ctx.Err(); err != nil {
			printer.Fail()
			return err
		}
		left := max(time.Duration(0), deadline.Sub(opts.Clock.Now()))
		printer.Update(waiting + " (" + progress.FormatLeft(left, timeout) + ")")
		if !opts.Clock.Now().Before(deadline) {
			break
		}
		client, err := opts.Dial(ctx, cfg.SSH, d.Address, port)
		if err == nil {
			idErr := verifyDialIdentity(ctx, client, d.Name)
			_ = client.Close()
			if idErr != nil {
				last = idErr
			} else {
				printer.OK()
				return nil
			}
		} else {
			last = err
		}
		if n == attempts {
			break
		}
		remaining := deadline.Sub(opts.Clock.Now())
		if remaining <= 0 {
			break
		}
		opts.Clock.Sleep(remaining / time.Duration(attempts-n+1))
	}
	printer.Fail()
	return reconnectTimeout(d.Name, last)
}

func reconnectTimeout(name string, last error) error {
	if last != nil {
		return fmt.Errorf("upgrade: %s: reconnect timed out: %w", name, last)
	}
	return fmt.Errorf("upgrade: %s: reconnect timed out", name)
}

func loadOrNew(stateDir string, d inventory.Device) (*state.DeviceJob, error) {
	job, err := state.Load(stateDir, d.Name)
	if err == nil {
		return job, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return &state.DeviceJob{Device: d.Name, Group: d.Group}, nil
}

func verifyDialIdentity(ctx context.Context, client transport.Client, want string) error {
	out, err := client.Run(ctx, "/system identity print")
	if err != nil {
		return err
	}
	id := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "name" {
			id = strings.TrimSpace(val)
			break
		}
	}
	if id == "" {
		return errors.New("missing identity")
	}
	if id != want {
		return fmt.Errorf("identity %q does not match inventory name %q", id, want)
	}
	return nil
}

func checkDepends(stateDir string, d inventory.Device, version string) error {
	for _, dep := range d.DependsOn {
		job, err := state.Load(stateDir, dep)
		if err != nil {
			return fmt.Errorf("upgrade: %s: depends_on %s: %w", d.Name, dep, err)
		}
		if job.Status != state.StatusComplete {
			return fmt.Errorf("upgrade: %s: depends_on %s is %s, want complete", d.Name, dep, job.Status)
		}
		if job.Release != version {
			return fmt.Errorf("upgrade: %s: depends_on %s is complete on %s, want %s", d.Name, dep, job.Release, version)
		}
	}
	return nil
}

func markFailed(stateDir string, job *state.DeviceJob, cause error) error {
	job.Status = state.StatusFailed
	job.LastError = redact.String(cause.Error())
	job.UpdatedAt = time.Now().UTC()
	if err := state.Save(stateDir, job); err != nil {
		return fmt.Errorf("upgrade: save failed state: %v: %w", cause, err)
	}
	return cause
}

func factsFrom(job *state.DeviceJob) (discover.Facts, error) {
	if job == nil || len(job.Facts) == 0 {
		return discover.Facts{}, errors.New("missing discover facts")
	}
	var facts discover.Facts
	if err := json.Unmarshal(job.Facts, &facts); err != nil {
		return discover.Facts{}, fmt.Errorf("parse facts: %w", err)
	}
	return facts, nil
}

func packagesToStage(facts discover.Facts, man release.Manifest) ([]release.File, error) {
	byPkg := release.IndexByInstalledName(man.Files, facts.ArchitectureName)
	out := make([]release.File, 0, len(facts.Packages))
	for _, p := range facts.Packages {
		f, ok := byPkg[p.Name]
		if !ok {
			return nil, fmt.Errorf("missing package %s", p.Name)
		}
		out = append(out, f)
	}
	return out, nil
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
		return nil, fmt.Errorf("upgrade: no devices in group %q", group)
	}
	return out, nil
}

func startIndex(stage string) (int, error) {
	if stage == "" {
		return 0, nil
	}
	for i, s := range stages {
		if s == stage {
			return i, nil
		}
	}
	return 0, fmt.Errorf("unknown stage %q; refuse to resume", stage)
}

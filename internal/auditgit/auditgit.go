package auditgit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/opsgit"
	"github.com/taihen/rosup/internal/state"
)

type Artifacts struct {
	Export string
	Result any
	Log    string
}

func Push(ctx context.Context, cfg *config.Config, job *state.DeviceJob, jobID string, artifacts Artifacts) error {
	if cfg == nil {
		return errors.New("auditgit: nil config")
	}
	if job == nil {
		return errors.New("auditgit: nil job")
	}
	if err := validateName("device", job.Device); err != nil {
		return err
	}
	if err := validateName("job id", jobID); err != nil {
		return err
	}

	auditDir := cfg.Ops.AuditDir
	if auditDir == "" {
		auditDir = "audit"
	}

	resultJSON, err := json.MarshalIndent(artifacts.Result, "", "  ")
	if err != nil {
		return fmt.Errorf("auditgit: result.json: %w", err)
	}
	items := []opsgit.Item{
		{Rel: filepath.ToSlash(filepath.Join(auditDir, job.Device, jobID, "export.rsc")), Body: artifacts.Export},
		{Rel: filepath.ToSlash(filepath.Join(auditDir, job.Device, jobID, "result.json")), Body: string(resultJSON) + "\n"},
		{Rel: filepath.ToSlash(filepath.Join(auditDir, job.Device, jobID, "log.txt")), Body: artifacts.Log},
	}
	if err := opsgit.Commit(ctx, cfg, "audit: "+job.Device+" "+jobID, items); err != nil {
		return fmt.Errorf("auditgit: %w", err)
	}
	return nil
}

func validateName(kind, name string) error {
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) {
		return fmt.Errorf("auditgit: invalid %s %q", kind, name)
	}
	return nil
}

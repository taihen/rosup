package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/taihen/rosup/internal/config"
	"gopkg.in/yaml.v3"
)

type Profile struct {
	ConvergenceTimeout    time.Duration
	NeighborStateAllow    []string
	RouteCountTolerance   int
	SessionRestoreTimeout time.Duration
}

func LoadProfile(cfg *config.Config, role string) (Profile, error) {
	if cfg == nil {
		return Profile{}, fmt.Errorf("validate: nil config")
	}
	if err := safeName("role", role); err != nil {
		return Profile{}, err
	}
	path := filepath.Join(cfg.Ops.Path, "inventory", "profiles", role+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, fmt.Errorf("validate: read profile %s: %w", path, err)
	}
	var raw struct {
		ConvergenceTimeout    string   `yaml:"convergence_timeout"`
		NeighborStateAllow    []string `yaml:"neighbor_state_allow"`
		RouteCountTolerance   int      `yaml:"route_count_tolerance"`
		SessionRestoreTimeout string   `yaml:"session_restore_timeout"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Profile{}, fmt.Errorf("validate: parse profile %s: %w", path, err)
	}
	if raw.ConvergenceTimeout == "" {
		return Profile{}, fmt.Errorf("validate: profile %s: convergence_timeout is required", role)
	}
	d, err := time.ParseDuration(raw.ConvergenceTimeout)
	if err != nil {
		return Profile{}, fmt.Errorf("validate: profile %s: convergence_timeout: %w", role, err)
	}
	if d <= 0 {
		return Profile{}, fmt.Errorf("validate: profile %s: convergence_timeout must be positive", role)
	}
	p := Profile{
		ConvergenceTimeout:  d,
		NeighborStateAllow:  raw.NeighborStateAllow,
		RouteCountTolerance: raw.RouteCountTolerance,
	}
	if len(p.NeighborStateAllow) == 0 {
		p.NeighborStateAllow = []string{"Full", "2-Way"}
	}
	if raw.SessionRestoreTimeout != "" {
		s, err := time.ParseDuration(raw.SessionRestoreTimeout)
		if err != nil {
			return Profile{}, fmt.Errorf("validate: profile %s: session_restore_timeout: %w", role, err)
		}
		if s < 0 {
			return Profile{}, fmt.Errorf("validate: profile %s: session_restore_timeout must not be negative", role)
		}
		p.SessionRestoreTimeout = s
	}
	return p, nil
}

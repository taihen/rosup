package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/lockfile"
	"github.com/taihen/rosup/internal/plan"
	"github.com/taihen/rosup/internal/transport"
)

func NewPlanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "plan",
		Short: "Show what an upgrade would do",
		Args:  cobra.NoArgs,
		RunE:  runPlan,
	}
}

func runPlan(cmd *cobra.Command, _ []string) error {
	version, err := cmd.Flags().GetString("release")
	if err != nil {
		return err
	}
	if version == "" {
		return errors.New("plan: --release is required")
	}
	group, err := cmd.Flags().GetString("group")
	if err != nil {
		return err
	}

	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	unlock, err := lockfile.Acquire(cfg.LockPath)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	report, err := plan.Run(cmd.Context(), cfg, version, group, sshDiscover)
	if err != nil {
		return err
	}
	if err := plan.Format(cmd.OutOrStdout(), report); err != nil {
		return err
	}
	return plan.Failure(report)
}

func sshDiscover(ctx context.Context, cfg *config.Config, group string) ([]discover.Result, error) {
	return discover.Run(ctx, cfg, group, transport.Dial)
}

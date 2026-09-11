package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
	"github.com/taihen/rosup/internal/lockfile"
	"github.com/taihen/rosup/internal/transport"
	"github.com/taihen/rosup/internal/upgrade"
)

func NewRollbackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rollback [device]",
		Short: "Downgrade packages to a complete local release",
		Args:  cobra.ExactArgs(1),
		RunE:  runRollback,
	}
}

func runRollback(cmd *cobra.Command, args []string) error {
	toVersion, err := cmd.Flags().GetString("to-version")
	if err != nil {
		return err
	}
	if toVersion == "" {
		return errors.New("rollback: --to-version is required")
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

	ctx := cmd.Context()
	if cfg.UpgradeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.UpgradeTimeout)
		defer cancel()
	}

	return upgrade.Rollback(ctx, cfg, args[0], toVersion, upgrade.Options{
		Dial: transport.Dial,
	})
}

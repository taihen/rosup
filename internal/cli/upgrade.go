package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
	"github.com/taihen/rosup/internal/lockfile"
	"github.com/taihen/rosup/internal/transport"
	"github.com/taihen/rosup/internal/upgrade"
)

func NewUpgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade [device]",
		Short: "Run the upgrade for a device, group, or the whole fleet",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runUpgrade,
	}
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	version, err := cmd.Flags().GetString("release")
	if err != nil {
		return err
	}
	if version == "" {
		return errors.New("upgrade: --release is required")
	}
	group, err := cmd.Flags().GetString("group")
	if err != nil {
		return err
	}
	resume, err := cmd.Flags().GetBool("resume")
	if err != nil {
		return err
	}
	name := optionalArg(args)

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

	return upgrade.Run(ctx, cfg, version, group, name, upgrade.Options{
		Dial:   transport.Dial,
		Out:    cmd.OutOrStdout(),
		Resume: resume,
	})
}

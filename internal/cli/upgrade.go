package cli

import (
	"errors"

	"github.com/spf13/cobra"
	"github.com/taihen/rosup/internal/lockfile"
	"github.com/taihen/rosup/internal/transport"
	"github.com/taihen/rosup/internal/upgrade"
)

func NewUpgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Run the upgrade for a group",
		Args:  cobra.NoArgs,
		RunE:  runUpgrade,
	}
}

func runUpgrade(cmd *cobra.Command, _ []string) error {
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

	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	unlock, err := lockfile.Acquire(cfg.LockPath)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	return upgrade.Run(cmd.Context(), cfg, version, group, upgrade.Options{
		Dial: transport.Dial,
	})
}

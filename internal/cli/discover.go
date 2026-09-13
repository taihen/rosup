package cli

import (
	"github.com/spf13/cobra"
	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/lockfile"
	"github.com/taihen/rosup/internal/transport"
)

func NewDiscoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "discover [device]",
		Short: "Read-only facts from a device",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runDiscover,
	}
}

func runDiscover(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	group, err := cmd.Flags().GetString("group")
	if err != nil {
		return err
	}
	name := optionalArg(args)

	unlock, err := lockfile.Acquire(cfg.LockPath)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	results, err := discover.Run(cmd.Context(), cfg, group, name, transport.Dial)
	if err != nil {
		return err
	}
	return discover.Format(cmd.OutOrStdout(), results)
}

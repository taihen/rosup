package cli

import (
	"github.com/spf13/cobra"
	"github.com/taihen/rosup/internal/status"
)

func NewStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [device]",
		Short: "Show upgrade job progress from local state",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runStatus,
	}
}

func runStatus(cmd *cobra.Command, args []string) error {
	group, err := cmd.Flags().GetString("group")
	if err != nil {
		return err
	}
	release, err := cmd.Flags().GetString("release")
	if err != nil {
		return err
	}
	name := optionalArg(args)

	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}

	report, err := status.Run(cfg, group, name, release)
	if err != nil {
		return err
	}
	return status.Format(cmd.OutOrStdout(), report)
}

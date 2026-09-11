package cli

import (
	"github.com/spf13/cobra"
	"github.com/taihen/rosup/internal/status"
)

func NewStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show upgrade job progress from local state",
		Args:  cobra.NoArgs,
		RunE:  runStatus,
	}
}

func runStatus(cmd *cobra.Command, _ []string) error {
	group, err := cmd.Flags().GetString("group")
	if err != nil {
		return err
	}
	release, err := cmd.Flags().GetString("release")
	if err != nil {
		return err
	}

	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}

	report, err := status.Run(cfg, group, release)
	if err != nil {
		return err
	}
	return status.Format(cmd.OutOrStdout(), report)
}

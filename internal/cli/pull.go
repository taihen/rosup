package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/taihen/rosup/internal/lockfile"
	"github.com/taihen/rosup/internal/opsgit"
)

func NewPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Discard local ops checkout drift; hard-reset to origin tip",
		Args:  cobra.NoArgs,
		RunE:  runPull,
	}
}

func runPull(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}

	unlock, err := lockfile.Acquire(cfg.LockPath)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	res, err := opsgit.Pull(cmd.Context(), cfg)
	if err != nil {
		return err
	}

	msg := "pulled"
	if res.AlreadyUpToDate {
		msg = "already up to date"
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", msg, res.Hash.String()[:7])
	return err
}

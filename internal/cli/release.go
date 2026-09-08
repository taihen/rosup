package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/lockfile"
	"github.com/taihen/rosup/internal/release"
)

func NewReleaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release",
		Short: "Manage synced RouterOS releases",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("a release subcommand is required")
		},
	}
	cmd.AddCommand(newReleaseSyncCmd(), newReleaseListCmd())
	return cmd
}

func loadConfig(cmd *cobra.Command) (*config.Config, error) {
	flagVal, err := cmd.Flags().GetString("config")
	if err != nil {
		return nil, err
	}
	return config.Load(config.ResolvePath(flagVal))
}

func newReleaseSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Download Long-term packages",
		Args:  cobra.NoArgs,
		RunE:  runReleaseSync,
	}
}

func newReleaseListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List locally synced release versions",
		Args:  cobra.NoArgs,
		RunE:  runReleaseList,
	}
}

func runReleaseSync(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	unlock, err := lockfile.Acquire(cfg.LockPath)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	man, err := release.Sync(cmd.Context(), cfg, release.NewHTTPClient())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "synced %s (%d files)\n", man.Version, len(man.Files))
	return err
}

func runReleaseList(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	versions, err := release.List(cfg.PackageDir)
	if err != nil {
		return err
	}
	for _, v := range versions {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), v); err != nil {
			return err
		}
	}
	return nil
}

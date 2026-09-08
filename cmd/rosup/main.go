package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/taihen/rosup/internal/cli"
	"github.com/taihen/rosup/internal/config"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "rosup",
		Short:         "MikroTik RouterOS 6 Long-term upgrade controller",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("a command is required")
		},
		PersistentPreRunE: checkConfigFlag,
	}

	root.PersistentFlags().String("config", "", "path to YAML config file")
	root.PersistentFlags().String("release", "", "release version")
	root.PersistentFlags().String("group", "", "device group")
	root.PersistentFlags().String("to-version", "", "target version")
	root.PersistentFlags().String("file", "", "backup file path")
	root.PersistentFlags().Bool("resume", false, "reserved")

	root.AddCommand(
		newVersionCmd(),
		cli.NewDiscoverCmd(),
		cli.NewReleaseCmd(),
		stubCmd("plan", "Show what an upgrade would do"),
		stubCmd("upgrade", "Run the upgrade for a group"),
		stubCmd("verify", "Re-check a device against its role profile"),
		stubCmd("rollback", "Downgrade packages to a complete local release"),
		newBackupCmd(),
	)
	return root
}

func checkConfigFlag(cmd *cobra.Command, _ []string) error {
	switch cmd.Name() {
	case "version", "help", "completion":
		return nil
	}
	if !cmd.Flags().Changed("config") {
		return nil
	}
	path, err := configFlagPath(cmd)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	return nil
}

func configFlagPath(cmd *cobra.Command) (string, error) {
	flagVal, err := cmd.Flags().GetString("config")
	if err != nil {
		return "", err
	}
	return config.ResolvePath(flagVal), nil
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the build version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "rosup %s (%s) %s\n", version, commit, date)
			return err
		},
	}
}

func newBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Manage local binary backups",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("a backup subcommand is required")
		},
	}
	cmd.AddCommand(stubCmd("restore", "Restore a local backup file to a device"))
	return cmd
}

func stubCmd(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("not implemented")
		},
	}
}

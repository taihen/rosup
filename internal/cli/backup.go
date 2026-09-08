package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/taihen/rosup/internal/backup"
	"github.com/taihen/rosup/internal/inventory"
	"github.com/taihen/rosup/internal/lockfile"
	"github.com/taihen/rosup/internal/transport"
)

func NewBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Manage local binary backups",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("a backup subcommand is required")
		},
	}
	cmd.AddCommand(newBackupRestoreCmd())
	return cmd
}

func newBackupRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore [device]",
		Short: "Restore a local backup file to a device",
		Args:  cobra.ExactArgs(1),
		RunE:  runBackupRestore,
	}
}

func runBackupRestore(cmd *cobra.Command, args []string) error {
	path, err := cmd.Flags().GetString("file")
	if err != nil {
		return err
	}
	if path == "" {
		return errors.New("backup restore: --file is required")
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

	name := args[0]
	devices, err := inventory.Load(cfg)
	if err != nil {
		return err
	}
	var device *inventory.Device
	for i := range devices {
		if devices[i].Name == name {
			device = &devices[i]
			break
		}
	}
	if device == nil {
		return fmt.Errorf("backup restore: unknown device %q", name)
	}

	port := device.Port
	if port == 0 {
		port = cfg.SSH.DefaultPort
	}
	client, err := transport.Dial(cmd.Context(), cfg.SSH, device.Address, port)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	return backup.Restore(cmd.Context(), device.Name, client, path)
}

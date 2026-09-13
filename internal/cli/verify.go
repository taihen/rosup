package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/taihen/rosup/internal/inventory"
	"github.com/taihen/rosup/internal/lockfile"
	"github.com/taihen/rosup/internal/progress"
	"github.com/taihen/rosup/internal/transport"
	"github.com/taihen/rosup/internal/validate"
)

func NewVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify [device]",
		Short: "Re-check a device against its role profile",
		Args:  cobra.ExactArgs(1),
		RunE:  runVerify,
	}
}

func runVerify(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	group, err := cmd.Flags().GetString("group")
	if err != nil {
		return err
	}
	unlock, err := lockfile.Acquire(cfg.LockPath)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	devices, err := inventory.Load(cfg)
	if err != nil {
		return err
	}
	device, err := inventory.Lookup(devices, group, args[0])
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}

	return validate.Verify(cmd.Context(), validate.Request{
		Config:   cfg,
		Device:   device,
		Dial:     transport.Dial,
		Progress: progress.New(cmd.OutOrStdout(), []string{device.Name}),
	})
}

package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/aolmosj/azsel/internal/config"
	"github.com/spf13/cobra"
)

func newDefaultCmd() *cobra.Command {
	var clear bool

	c := &cobra.Command{
		Use:   "default [name]",
		Short: "Set, show or clear the default tenant for new shells",
		Long: `Set the default tenant by pointing ~/.azure at its profile, so every
new shell — and anything else that runs az, including cron and IDEs —
starts on that tenant without running azsel first.

  azsel default <name>   make <name> the default
  azsel default          show the current default
  azsel default --clear  remove the default, returning az to its own ~/.azure

Setting a default takes over ~/.azure. An existing ~/.azure directory is
moved to a backup under ~/.azsel/backups first; it is never deleted.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			switch {
			case clear:
				if len(args) > 0 {
					return fmt.Errorf("--clear takes no tenant name")
				}
				return runDefaultClear(cfg)
			case len(args) == 1:
				return runDefaultSet(cfg, args[0])
			default:
				return runDefaultShow(cfg)
			}
		},
	}

	c.Flags().BoolVar(&clear, "clear", false, "Remove the default tenant")
	return c
}

func runDefaultSet(cfg *config.Config, name string) error {
	// The timestamp names the backup directory. It is generated here, in the
	// command layer, so config stays testable against a fixed clock.
	stamp := time.Now().Format("20060102-150405")
	res, err := config.SetDefault(cfg, name, stamp)
	if err != nil {
		return err
	}
	if res.BackupPath != "" {
		fmt.Fprintf(os.Stderr, "Moved your existing ~/.azure to %s\n", res.BackupPath)
	}
	if res.Repointed {
		fmt.Fprintf(os.Stderr, "Default tenant is now %q.\n", res.Tenant)
	} else {
		fmt.Fprintf(os.Stderr, "Default tenant set to %q.\n", res.Tenant)
		fmt.Fprintln(os.Stderr, "New shells will start on this tenant. Open one to try it,")
		fmt.Fprintln(os.Stderr, "or run 'azsel use "+res.Tenant+"' to switch this shell now.")
	}
	return nil
}

func runDefaultClear(cfg *config.Config) error {
	res, err := config.ClearDefault(cfg)
	if err != nil {
		return err
	}
	if res.Cleared {
		fmt.Fprintln(os.Stderr, "Default cleared. az will use its own ~/.azure again.")
	} else {
		fmt.Fprintln(os.Stderr, "No default was set.")
	}
	if res.LatestBackup != "" {
		fmt.Fprintf(os.Stderr, "Your previous ~/.azure is at %s\n", res.LatestBackup)
		fmt.Fprintln(os.Stderr, "Restore it with: rm -rf ~/.azure && mv "+res.LatestBackup+" ~/.azure")
	}
	return nil
}

func runDefaultShow(cfg *config.Config) error {
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		return err
	}
	switch info.State {
	case config.DefaultSet:
		fmt.Fprintf(os.Stderr, "Default tenant: %s\n", info.Tenant)
	case config.DefaultNone, config.DefaultNative:
		fmt.Fprintln(os.Stderr, "No default set. az uses its own ~/.azure.")
		fmt.Fprintln(os.Stderr, "Set one with: azsel default <name>")
	case config.DefaultForeign:
		fmt.Fprintf(os.Stderr, "~/.azure is a symlink to %s, which azsel did not create.\n", info.Target)
	case config.DefaultBroken:
		fmt.Fprintf(os.Stderr, "~/.azure is a broken symlink to %s.\n", info.Target)
		fmt.Fprintln(os.Stderr, "az will fail until this is fixed. Run 'azsel default --clear' to remove it,")
		fmt.Fprintln(os.Stderr, "or 'azsel default <name>' to point it at a tenant.")
	}
	return nil
}

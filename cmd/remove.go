package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/aolmosj/azsel/internal/config"
	"github.com/spf13/cobra"
)

func newRemoveCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a tenant configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			name := args[0]
			tenant := cfg.FindTenant(name)
			if tenant == nil {
				return fmt.Errorf("tenant %q not found", name)
			}

			if !force {
				fmt.Fprintf(os.Stderr, "Remove tenant %q and delete %s? [y/N] ", tenant.Name, tenant.ConfigDir)
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					fmt.Fprintln(os.Stderr, "Aborted.")
					return nil
				}
			}

			configDir := tenant.ConfigDir

			// If this tenant is the default, drop the ~/.azure link before
			// anything else. Removing the directory first would leave the
			// link dangling, and az fails hard on a dangling ~/.azure. This
			// has to happen while the tenant is still in the config, or
			// ClearDefault would read the link as one it does not own and
			// refuse to touch it.
			if info, err := config.ResolveDefault(cfg); err == nil &&
				info.State == config.DefaultSet && strings.EqualFold(info.Tenant, name) {
				if _, err := config.ClearDefault(cfg); err != nil {
					return fmt.Errorf("clearing default before removal: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Cleared default (was %q).\n", name)
			}

			if err := cfg.RemoveTenant(name); err != nil {
				return err
			}
			if err := os.RemoveAll(configDir); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not remove %s: %v\n", configDir, err)
			}

			fmt.Fprintf(os.Stderr, "Tenant %q removed.\n", name)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	return cmd
}

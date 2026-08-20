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

			// If ~/.azure points at this tenant, drop the link before removing
			// the directory — deleting it first would leave ~/.azure dangling,
			// and az fails hard on that. Keyed on the link target rather than
			// on ResolveDefault's state so it also catches an already-dangling
			// link into this tenant (the DefaultBroken case). Done while the
			// tenant is still in the config, or ClearDefault would read the
			// link as one it does not own and refuse it.
			switch target, err := config.DefaultLinkTarget(); {
			case err != nil:
				// Could not read the link. Surface it rather than delete
				// blindly and risk a silent dangle.
				fmt.Fprintf(os.Stderr, "Warning: could not check the default link: %v\n", err)
			case target == tenant.ConfigDir:
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

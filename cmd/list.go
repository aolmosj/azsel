package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/aolmosj/azsel/internal/config"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all configured tenants",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if len(cfg.Tenants) == 0 {
				fmt.Fprintln(os.Stderr, "No tenants configured. Run 'azsel add' to add one.")
				return nil
			}
			currentDir := os.Getenv("AZURE_CONFIG_DIR")

			// "active" and "default" are different things: active is the
			// tenant this shell points at right now, default is the one new
			// shells will start on. A tenant can be either, both, or neither.
			def, defErr := config.ResolveDefault(cfg)

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ACTIVE\tDEFAULT\tNAME\tTENANT ID\tCONFIG DIR")
			for _, t := range cfg.Tenants {
				active := ""
				if t.ConfigDir == currentDir {
					active = "*"
				}
				isDefault := ""
				if defErr == nil && def.State == config.DefaultSet && strings.EqualFold(def.Tenant, t.Name) {
					isDefault = "D"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", active, isDefault, t.Name, t.TenantID, t.ConfigDir)
			}
			if err := w.Flush(); err != nil {
				return err
			}

			// A broken or foreign ~/.azure never shows as a default above, so
			// say why here — list is where someone looks when az misbehaves.
			if defErr == nil {
				switch def.State {
				case config.DefaultBroken:
					fmt.Fprintf(os.Stderr, "\nWarning: ~/.azure is a broken symlink to %s; az will fail until it is fixed.\n", def.Target)
					fmt.Fprintln(os.Stderr, "Run 'azsel default --clear' or 'azsel default <name>' to repair it.")
				case config.DefaultForeign:
					fmt.Fprintf(os.Stderr, "\nNote: ~/.azure is a symlink to %s that azsel did not create.\n", def.Target)
				}
			}
			return nil
		},
	}
}

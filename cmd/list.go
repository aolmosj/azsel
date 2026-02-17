package cmd

import (
	"fmt"
	"os"
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
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ACTIVE\tNAME\tTENANT ID\tCONFIG DIR")
			for _, t := range cfg.Tenants {
				active := ""
				if t.ConfigDir == currentDir {
					active = "*"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", active, t.Name, t.TenantID, t.ConfigDir)
			}
			return w.Flush()
		},
	}
}

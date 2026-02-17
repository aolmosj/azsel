package cmd

import (
	"fmt"
	"os"

	"github.com/aolmosj/azsel/internal/config"
	"github.com/spf13/cobra"
)

func newUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Print export command for a tenant",
		Long:  `Prints "export AZURE_CONFIG_DIR=..." to stdout. Use with: eval $(azsel use <name>)`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			tenant := cfg.FindTenant(args[0])
			if tenant == nil {
				return fmt.Errorf("tenant %q not found", args[0])
			}
			if _, err := os.Stat(tenant.ConfigDir); os.IsNotExist(err) {
				return fmt.Errorf("config directory %s does not exist — try running 'azsel add' again", tenant.ConfigDir)
			}
			extDir, err := config.ExtensionsDir()
			if err != nil {
				return err
			}
			fmt.Printf("export AZURE_CONFIG_DIR=%s\n", tenant.ConfigDir)
			fmt.Printf("export AZURE_EXTENSION_DIR=%s\n", extDir)
			fmt.Fprintf(os.Stderr, "Switched to tenant %q\n", tenant.Name)
			return nil
		},
	}
}

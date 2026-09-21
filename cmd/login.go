package cmd

import (
	"fmt"
	"os"

	"github.com/aolmosj/azsel/internal/azure"
	"github.com/aolmosj/azsel/internal/config"
	"github.com/spf13/cobra"
)

func newLoginCmd() *cobra.Command {
	var deviceCode bool
	c := &cobra.Command{
		Use:   "login [name]",
		Short: "Log in to a tenant, or refresh an expired session",
		Long: `Run 'az login' for a tenant, scoped to its isolated config directory.

Use this to sign in again after a session expires (az's refresh tokens lapse
after a period of inactivity) without disturbing the other tenants. With no
name the default tenant is used.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := azure.Available(); err != nil {
				return err
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if len(cfg.Tenants) == 0 {
				fmt.Fprintln(os.Stderr, "No tenants configured. Run 'azsel add' to add one.")
				return nil
			}
			tenant, err := resolveTenant(cfg, args)
			if err != nil {
				return err
			}
			if _, err := os.Stat(tenant.ConfigDir); os.IsNotExist(err) {
				return fmt.Errorf("config directory %s does not exist — run 'azsel add' first", tenant.ConfigDir)
			}
			// Keep the shared-extensions link current, as 'use' does, so the
			// refreshed session sees the same extensions.
			if err := config.EnsureSharedExtensionsLink(tenant.ConfigDir); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Logging in to tenant %q...\n", tenant.Name)
			return azure.Login(tenant.TenantID, tenant.ConfigDir, deviceCode)
		},
	}
	c.Flags().BoolVar(&deviceCode, "device-code", false, "use the device code flow instead of a browser")
	return c
}

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/aolmosj/azsel/internal/azure"
	"github.com/aolmosj/azsel/internal/config"
	"github.com/spf13/cobra"
)

var nameRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func newAddCmd() *cobra.Command {
	var useDeviceCode bool

	c := &cobra.Command{
		Use:   "add",
		Short: "Add a new Azure tenant",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			reader := bufio.NewReader(os.Stdin)

			fmt.Fprint(os.Stderr, "Tenant name (lowercase, alphanumeric, hyphens): ")
			name, _ := reader.ReadString('\n')
			name = strings.TrimSpace(name)
			if !nameRegex.MatchString(name) {
				return fmt.Errorf("invalid name %q — use lowercase alphanumeric and hyphens only", name)
			}
			if cfg.FindTenant(name) != nil {
				return fmt.Errorf("tenant %q already exists", name)
			}

			fmt.Fprint(os.Stderr, "Azure Tenant ID: ")
			tenantID, _ := reader.ReadString('\n')
			tenantID = strings.TrimSpace(tenantID)
			if tenantID == "" {
				return fmt.Errorf("tenant ID cannot be empty")
			}

			configDir, err := config.TenantDir(name)
			if err != nil {
				return err
			}

			extDir, err := config.ExtensionsDir()
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "\nLogging in to tenant %q (%s)...\n", name, tenantID)
			if err := azure.Login(tenantID, configDir, extDir, useDeviceCode); err != nil {
				return fmt.Errorf("az login failed: %w", err)
			}

			tenant := config.Tenant{
				Name:      name,
				TenantID:  tenantID,
				ConfigDir: configDir,
			}
			if err := cfg.AddTenant(tenant); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "\nTenant %q added successfully.\n", name)
			fmt.Fprintf(os.Stderr, "To activate: eval $(azsel use %s)\n", name)
			return nil
		},
	}

	c.Flags().BoolVar(&useDeviceCode, "device-code", false, "Use device code flow instead of opening a browser")
	return c
}

package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/aolmosj/azsel/internal/azure"
	"github.com/aolmosj/azsel/internal/config"
	"github.com/aolmosj/azsel/internal/pim"
	"github.com/spf13/cobra"
)

func newPIMCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "pim",
		Short: "Work with Azure PIM eligible resource roles",
	}
	c.AddCommand(newPIMListCmd())
	return c
}

func newPIMListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [name]",
		Short: "List eligible Azure resource-role PIM assignments for a tenant",
		Long: `List the Azure resource-role (RBAC) assignments you are eligible to
activate in a tenant, via Azure PIM. With no name the default tenant is used.

This is read-only; it does not activate anything.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Fail before any work if the Azure CLI is missing: the whole
			// command depends on `az rest`.
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
			if err := requireSession(tenant); err != nil {
				return err
			}

			rows, err := pim.ListEligible(tenant.ConfigDir, tenant.TenantID)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				fmt.Fprintf(os.Stderr, "No eligible resource-role assignments for %q.\n", tenant.Name)
				return nil
			}

			// Table on stdout (like 'list'); stdout stays parseable.
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ROLE\tSCOPE\tTYPE\tVIA\tUNTIL")
			for _, r := range rows {
				until := "permanent"
				if r.End != nil {
					until = r.End.Format(time.RFC3339)
				}
				via := r.ViaGroup()
				if via == "" {
					via = "direct"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.RoleName, r.ScopeName, r.ScopeType, via, until)
			}
			return w.Flush()
		},
	}
}

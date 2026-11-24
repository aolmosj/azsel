package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
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
	c.AddCommand(newPIMActivateCmd())
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

			// pim.ListEligible acquires a token for the tenant itself, which is
			// the real session gate (and specific to this tenant, unlike the
			// active-subscription session a general check would see).
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

func newPIMActivateCmd() *cobra.Command {
	var role, scope string
	var yes bool
	c := &cobra.Command{
		Use:   "activate [name]",
		Short: "Activate an eligible Azure resource-role PIM assignment",
		Long: `Activate one eligible Azure resource-role (RBAC) assignment via PIM, for the
maximum duration the policy allows, with the justification "Activated via
azsel". Choose the role with --role (and --scope to disambiguate when it is
eligible at several). With no name the default tenant is used.

A policy that requires MFA or a ticket at activation will make Azure reject the
request; that error is shown as-is.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := azure.Available(); err != nil {
				return err
			}
			if strings.TrimSpace(role) == "" {
				return fmt.Errorf("--role is required; run 'azsel pim list' to see the eligible roles")
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

			rows, err := pim.ListEligible(tenant.ConfigDir, tenant.TenantID)
			if err != nil {
				return err
			}
			var matches []pim.Eligible
			for _, r := range rows {
				if strings.EqualFold(r.RoleName, role) && (scope == "" || strings.EqualFold(r.ScopeName, scope)) {
					matches = append(matches, r)
				}
			}
			if len(matches) == 0 {
				return fmt.Errorf("you are not eligible for role %q%s in %q; run 'azsel pim list %s'",
					role, scopeSuffix(scope), tenant.Name, tenant.Name)
			}
			if len(matches) > 1 {
				var b strings.Builder
				fmt.Fprintf(&b, "role %q is eligible at several scopes; pick one with --scope:", role)
				for _, m := range matches {
					fmt.Fprintf(&b, "\n  --scope %q  (%s)", m.ScopeName, m.ScopeType)
				}
				return fmt.Errorf("%s", b.String())
			}

			e := matches[0]
			if !yes {
				fmt.Fprintf(os.Stderr, "Activate %q at %q (%s) for the policy's maximum duration? [y/N] ",
					e.RoleName, e.ScopeName, e.ScopeType)
				if !confirmed(os.Stdin) {
					fmt.Fprintln(os.Stderr, "Cancelled.")
					return nil
				}
			}
			fmt.Fprintf(os.Stderr, "Activating %q at %q...\n", e.RoleName, e.ScopeName)
			summary, err := pim.Activate(tenant.ConfigDir, tenant.TenantID, e)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, summary)
			return nil
		},
	}
	c.Flags().StringVar(&role, "role", "", "role to activate, exactly as shown by 'azsel pim list'")
	c.Flags().StringVar(&scope, "scope", "", "scope name to disambiguate when a role is eligible at several")
	c.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return c
}

func scopeSuffix(scope string) string {
	if scope == "" {
		return ""
	}
	return fmt.Sprintf(" at scope %q", scope)
}

// confirmed reads one line and reports whether it is an affirmative y/yes.
func confirmed(r io.Reader) bool {
	s := bufio.NewScanner(r)
	if !s.Scan() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(s.Text())) {
	case "y", "yes":
		return true
	}
	return false
}

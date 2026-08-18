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
			// Before anything the user would have to undo. Everything below
			// this line either asks them for input or creates directories,
			// and all of it is wasted if az is not installed.
			if err := azure.Available(); err != nil {
				return err
			}

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

			configDir, createdDir, err := config.EnsureTenantDir(name)
			if err != nil {
				return err
			}

			extDir, err := config.EnsureExtensionsDir()
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "\nLogging in to tenant %q (%s)...\n", name, tenantID)
			if err := azure.Login(tenantID, configDir, extDir, useDeviceCode); err != nil {
				// Undo only what this run created. A directory that was
				// already there can hold valid credentials from an earlier
				// attempt, and deleting those would turn a failed login into
				// a lost session.
				if createdDir {
					if rmErr := os.RemoveAll(configDir); rmErr != nil {
						fmt.Fprintf(os.Stderr, "Warning: could not remove %s: %v\n", configDir, rmErr)
					}
				}
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
			fmt.Fprint(os.Stderr, activationHint(name, shellIntegrationInstalled()))
			return nil
		},
	}

	c.Flags().BoolVar(&useDeviceCode, "device-code", false, "Use device code flow instead of opening a browser")
	return c
}

// activationHint explains how to activate a freshly added tenant.
//
// It used to suggest `eval $(azsel use NAME)`, which does nothing: use writes
// the export snippet to a file and reports to stderr, so the command
// substitution captures an empty string. Users with shell integration saw it
// work anyway — the wrapper sourced the file — which made the wrong advice
// look right.
//
// The wrapper sets EnvSwitchFile when invoking the binary, so its absence is
// a reliable sign that integration is not installed. Worth saying out loud
// here: without it, `azsel use` cannot reach the caller's shell at all.
// shellIntegrationInstalled reports whether the wrapper looks set up.
//
// EnvSwitchFile is set by the wrapper itself, so it is the strongest signal —
// but `command azsel add` deliberately bypasses the wrapper, and so does any
// script. Falling back to the rc file avoids telling a correctly configured
// user to run `azsel init`, which would just answer "already configured" and
// leave them stuck.
func shellIntegrationInstalled() bool {
	if os.Getenv(config.EnvSwitchFile) != "" {
		return true
	}
	rcFile, _, err := detectShellRC()
	if err != nil || rcFile == "" {
		return false
	}
	return rcContainsInit(rcFile)
}

func activationHint(name string, integrationActive bool) string {
	hint := fmt.Sprintf("To activate: azsel use %s\n", name)
	if integrationActive {
		return hint
	}
	return hint + "\nShell integration does not look active. Run 'azsel init' and reload your\n" +
		"shell — otherwise 'azsel use' cannot change AZURE_CONFIG_DIR in your session.\n"
}

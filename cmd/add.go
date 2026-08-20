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

// az login --tenant takes either a tenant GUID or one of the tenant's
// verified domains, so accepting only GUIDs would reject a legitimate case.
var (
	tenantGUIDRegex = regexp.MustCompile(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	// Two or more dot-separated labels; a label is alphanumeric and may
	// contain inner hyphens.
	tenantDomainRegex = regexp.MustCompile(
		`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)
)

// normalizeTenantID checks the tenant ID and returns it canonicalised.
//
// Only the shape is checked — whether the tenant exists is az's business.
// The point is to fail on an obvious typo before the browser opens, with a
// message better than whatever Azure returns.
func normalizeTenantID(raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return "", fmt.Errorf("tenant ID cannot be empty")
	}
	// Both forms are case-insensitive to Azure, so the casing carries no
	// meaning. Storing one keeps what `azsel list` and the TUI display
	// consistent. Nothing depends on it: list marks the active tenant by
	// comparing ConfigDir, and the TUI filter already ignores case.
	if tenantGUIDRegex.MatchString(id) || tenantDomainRegex.MatchString(id) {
		return strings.ToLower(id), nil
	}
	return "", fmt.Errorf("invalid tenant ID %q — expected a GUID such as "+
		"11111111-1111-1111-1111-111111111111, or a domain such as "+
		"contoso.onmicrosoft.com", id)
}

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

			fmt.Fprint(os.Stderr, "Azure Tenant ID (GUID or domain): ")
			rawTenantID, _ := reader.ReadString('\n')
			tenantID, err := normalizeTenantID(rawTenantID)
			if err != nil {
				return err
			}

			configDir, createdDir, err := config.EnsureTenantDir(name)
			if err != nil {
				return err
			}

			// From here on the tenant directory may exist without a config
			// entry to match, so every exit has to undo it. A deferred
			// rollback covers the paths a hand-written one keeps missing:
			// the login, but also a failing extensions directory or a
			// config.json that cannot be written.
			//
			// Only what this run created. A directory that was already there
			// can hold valid credentials from an earlier attempt, and
			// deleting those would turn a failed add into a lost session.
			added := false
			defer func() {
				if added || !createdDir {
					return
				}
				if rmErr := os.RemoveAll(configDir); rmErr != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not remove %s: %v\n", configDir, rmErr)
				}
			}()

			// Share extensions through the filesystem before az runs, so the
			// login already reads and writes them in the shared directory
			// (see #26). Reached through the default link there is no
			// AZURE_EXTENSION_DIR to steer az, so the symlink is what makes
			// sharing hold however the tenant is later entered.
			if err := config.EnsureSharedExtensionsLink(configDir); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "\nLogging in to tenant %q (%s)...\n", name, tenantID)
			if err := azure.Login(tenantID, configDir, useDeviceCode); err != nil {
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
			added = true

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

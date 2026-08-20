package cmd

import (
	"bufio"
	"fmt"
	"io"
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
	var (
		useDeviceCode    bool
		tenantFlag       string
		servicePrincipal bool
		username         string
		certificate      string
		passwordStdin    bool
	)

	c := &cobra.Command{
		Use:   "add [name]",
		Short: "Add a new Azure tenant",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Before anything the user would have to undo. Everything below
			// either asks for input or creates directories, all wasted if az
			// is not installed.
			if err := azure.Available(); err != nil {
				return err
			}
			if err := validateAddFlags(args, tenantFlag, servicePrincipal, useDeviceCode, username, certificate, passwordStdin); err != nil {
				return err
			}

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			reader := bufio.NewReader(os.Stdin)

			// Name: a positional argument, or a prompt when omitted.
			var name string
			if len(args) == 1 {
				name = strings.TrimSpace(args[0])
			} else {
				fmt.Fprint(os.Stderr, "Tenant name (lowercase, alphanumeric, hyphens): ")
				line, _ := reader.ReadString('\n')
				name = strings.TrimSpace(line)
			}
			if !nameRegex.MatchString(name) {
				return fmt.Errorf("invalid name %q — use lowercase alphanumeric and hyphens only", name)
			}
			if cfg.FindTenant(name) != nil {
				return fmt.Errorf("tenant %q already exists", name)
			}

			// Tenant ID: the --tenant flag, or a prompt when omitted.
			rawTenantID := tenantFlag
			if rawTenantID == "" {
				fmt.Fprint(os.Stderr, "Azure Tenant ID (GUID or domain): ")
				rawTenantID, _ = reader.ReadString('\n')
			}
			tenantID, err := normalizeTenantID(rawTenantID)
			if err != nil {
				return err
			}

			// Read a service-principal secret before touching disk. It comes
			// from stdin, never a flag, so it stays out of shell history and
			// azsel's own argv (az still takes it as --password in its argv).
			var secret string
			if servicePrincipal && passwordStdin {
				b, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("reading secret from stdin: %w", err)
				}
				secret = strings.TrimRight(string(b), "\r\n")
				if secret == "" {
					return fmt.Errorf("--password-stdin given but stdin was empty")
				}
			}
			if servicePrincipal && certificate != "" {
				if _, err := os.Stat(certificate); err != nil {
					return fmt.Errorf("certificate: %w", err)
				}
			}

			configDir, createdDir, err := config.EnsureTenantDir(name)
			if err != nil {
				return err
			}

			// From here the tenant directory may exist without a matching
			// config entry, so every exit undoes it — but only what this run
			// created; a pre-existing directory can hold valid credentials.
			added := false
			defer func() {
				if added || !createdDir {
					return
				}
				if rmErr := os.RemoveAll(configDir); rmErr != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not remove %s: %v\n", configDir, rmErr)
				}
			}()

			// Share extensions through the filesystem before az runs (see
			// #26): reached through the default link there is no
			// AZURE_EXTENSION_DIR, so the symlink is what makes sharing hold.
			if err := config.EnsureSharedExtensionsLink(configDir); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "\nLogging in to tenant %q (%s)...\n", name, tenantID)
			var loginErr error
			if servicePrincipal {
				loginErr = azure.LoginServicePrincipal(tenantID, configDir, username, certificate, secret)
			} else {
				loginErr = azure.Login(tenantID, configDir, useDeviceCode)
			}
			if loginErr != nil {
				return fmt.Errorf("az login failed: %w", loginErr)
			}

			tenant := config.Tenant{Name: name, TenantID: tenantID, ConfigDir: configDir}
			if err := cfg.AddTenant(tenant); err != nil {
				return err
			}
			added = true

			fmt.Fprintf(os.Stderr, "\nTenant %q added successfully.\n", name)
			fmt.Fprint(os.Stderr, activationHint(name, shellIntegrationInstalled()))
			return nil
		},
	}

	f := c.Flags()
	f.BoolVar(&useDeviceCode, "device-code", false, "Use device code flow instead of opening a browser")
	f.StringVar(&tenantFlag, "tenant", "", "Tenant ID (GUID or domain); prompted if omitted")
	f.BoolVar(&servicePrincipal, "service-principal", false, "Log in with a service principal instead of interactively")
	f.StringVarP(&username, "username", "u", "", "Service principal app (client) ID")
	f.StringVar(&certificate, "certificate", "", "PEM file for service-principal auth")
	f.BoolVar(&passwordStdin, "password-stdin", false, "Read the service-principal secret from stdin")
	return c
}

// validateAddFlags enforces the service-principal contract before any prompt
// or disk work. Service-principal mode is non-interactive: the name and
// --tenant must be given up front, so nothing has to be prompted — which
// matters because --password-stdin claims stdin for the secret.
func validateAddFlags(args []string, tenantFlag string, sp, deviceCode bool, username, certificate string, passwordStdin bool) error {
	if !sp {
		switch {
		case username != "", certificate != "", passwordStdin:
			return fmt.Errorf("--username, --certificate and --password-stdin require --service-principal")
		}
		return nil
	}
	if deviceCode {
		return fmt.Errorf("--service-principal cannot be combined with --device-code")
	}
	if len(args) != 1 {
		return fmt.Errorf("service-principal mode is non-interactive: pass the tenant name as an argument")
	}
	if tenantFlag == "" {
		return fmt.Errorf("service-principal mode is non-interactive: pass --tenant")
	}
	if username == "" {
		return fmt.Errorf("--service-principal requires --username (the app/client ID)")
	}
	if (certificate != "") == passwordStdin {
		// Both set or neither set — need exactly one.
		return fmt.Errorf("provide exactly one of --certificate or --password-stdin")
	}
	return nil
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

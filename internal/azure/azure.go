package azure

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// binary is the Azure CLI executable azsel drives.
const binary = "az"

// run executes a prepared command.
//
// It is a package variable so tests can observe the command azsel builds —
// arguments, environment, stream wiring — without an Azure CLI installed, a
// browser opening or a network.
var run = func(cmd *exec.Cmd) error { return cmd.Run() }

// lookPath resolves an executable on PATH, injectable for the same reason.
var lookPath = exec.LookPath

// installURL is where to get the Azure CLI when it turns out to be missing.
const installURL = "https://learn.microsoft.com/cli/azure/install-azure-cli"

// Available reports whether the Azure CLI can be found. Commands that shell
// out to az should call this before doing anything the user would have to
// undo — asking for input, creating directories — so a missing az costs
// nothing but the message.
//
// LookPath's own error ("executable file not found in $PATH") only restates
// this one, so it is not wrapped; what the user needs is where to get az.
func Available() error {
	if _, err := lookPath(binary); err != nil {
		return fmt.Errorf("Azure CLI (%s) not found in PATH.\nInstall it: %s", binary, installURL)
	}
	return nil
}

// command builds an az invocation scoped to one tenant's config directory.
// The environment is inherited so az keeps the user's proxy, locale and so
// on; only AZURE_CONFIG_DIR is overridden.
//
// Extensions are no longer steered with AZURE_EXTENSION_DIR: each tenant's
// cliextensions is a symlink to the shared directory, so az finds shared
// extensions through the filesystem however the tenant is reached (see
// config.EnsureSharedExtensionsLink). AZURE_CONFIG_DIR may already be exported
// — by azsel itself in an active session — and that is fine: os/exec
// deduplicates Env keeping the last occurrence, so what is appended here wins.
func command(configDir string, args ...string) *exec.Cmd {
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), "AZURE_CONFIG_DIR="+configDir)
	return cmd
}

func Login(tenantID, configDir string, useDeviceCode bool) error {
	args := []string{"login", "--tenant", tenantID}
	if useDeviceCode {
		args = append(args, "--use-device-code")
	}
	cmd := command(configDir, args...)
	// Deliberate, and easy to undo by accident: the login is interactive, so
	// stdin must reach az; and az's stdout is sent to stderr because azsel
	// keeps stdout clean for the shell to consume.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return run(cmd)
}

// LoginServicePrincipal logs a tenant in with a service principal, scoped to
// configDir. Either certificate (a PEM path) or secret is used, never both;
// the caller enforces that. Non-interactive, so stdin is not connected.
//
// With a secret, az takes it as --password in its own argv — there is no
// stdin or env option in `az login` for it — so it is briefly visible in the
// process list. Prefer certificate where the exposure matters.
func LoginServicePrincipal(tenantID, configDir, appID, certificate, secret string) error {
	// Values are joined with '=' rather than passed as separate argv tokens.
	// az parses with argparse, which treats a value beginning with '-' (an
	// Azure secret can) as another flag; az's own help mandates the
	// --password=secret form for exactly that case. The '=' form is
	// unambiguous for every value.
	args := []string{
		"login", "--service-principal",
		"--tenant=" + tenantID,
		"--username=" + appID,
	}
	if certificate != "" {
		args = append(args, "--certificate="+certificate)
	} else {
		args = append(args, "--password="+secret)
	}
	cmd := command(configDir, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return run(cmd)
}

// SessionState reports whether the profile in configDir still has a usable
// session for the given tenant. It asks az to acquire a token (which exercises
// the refresh token) and reads its expiry; a lapsed session — az's AADSTS700082
// / AADSTS70043, or never having logged in — makes az exit non-zero, reported
// here as valid=false.
//
// tenantID matters: a profile can hold subscriptions from several tenants and
// its active one may be a different tenant, so without --tenant this would check
// the wrong session. An empty tenantID falls back to the active session.
// Callers gate on Available first; any az failure is treated as "not signed in"
// rather than surfaced, since that is what the caller shows.
func SessionState(configDir, tenantID string) (valid bool, expiresOn string) {
	args := []string{"account", "get-access-token"}
	if strings.TrimSpace(tenantID) != "" {
		args = append(args, "--tenant", tenantID)
	}
	args = append(args, "--query", "expiresOn", "--output", "tsv")
	cmd := command(configDir, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{}
	if err := run(cmd); err != nil {
		return false, ""
	}
	return true, strings.TrimSpace(out.String())
}

// TenantToken acquires an ARM access token for a specific tenant, regardless of
// which subscription is currently active. This matters because a profile's
// active subscription may belong to a different tenant, and az rest would then
// use that wrong-tenant token — which the PIM API rejects with a misleading
// AadPremiumLicenseRequired. A failure here usually means the session for this
// tenant has lapsed.
func TenantToken(configDir, tenantID string) (string, error) {
	cmd := command(configDir, "account", "get-access-token",
		"--tenant", tenantID, "--resource", "https://management.azure.com",
		"--query", "accessToken", "--output", "tsv", "--only-show-errors")
	out, err := output(cmd)
	if err != nil {
		return "", fmt.Errorf("could not get a token for tenant %s (its session may have expired — run 'azsel login'): %w", tenantID, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// RestGET runs `az rest --method GET --url <url>` scoped to configDir and
// returns the response body. Unlike the login helpers, which stream az's output
// to stderr, this captures stdout — az writes the raw JSON body there — so
// callers can parse it.
//
// When token is non-empty it is passed as an explicit Authorization header, so
// the call uses that tenant's token rather than whatever subscription happens to
// be active (az rest honors a supplied Authorization header). url is a single
// argv token: exec runs no shell, so ?api-version=…&$filter=asTarget() reaches
// az verbatim.
func RestGET(configDir, url, token string) ([]byte, error) {
	args := []string{"rest", "--method", "GET", "--url", url, "--only-show-errors"}
	if token != "" {
		args = append(args, "--headers", "Authorization=Bearer "+token)
	}
	return output(command(configDir, args...))
}

// output runs cmd capturing stdout, folding az's stderr into the error so
// "run 'az login'" and AADSTS messages survive.
func output(cmd *exec.Cmd) ([]byte, error) {
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := run(cmd); err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			return nil, fmt.Errorf("%w\n%s", err, msg)
		}
		return nil, err
	}
	return out.Bytes(), nil
}

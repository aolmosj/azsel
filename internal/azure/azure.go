package azure

import (
	"fmt"
	"os"
	"os/exec"
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

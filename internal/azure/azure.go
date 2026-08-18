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

// command builds an az invocation scoped to one tenant's directories. The
// environment is inherited so az keeps the user's proxy, locale and so on;
// only the two directories azsel controls are overridden.
//
// Those two may already be exported — by azsel itself in an active session,
// or by an Azure CLI install that sets AZURE_EXTENSION_DIR system-wide. That
// is fine: os/exec deduplicates Env keeping the last occurrence, so what is
// appended here wins. TestCommandEnvOverridesInheritedValues pins it, because
// the guarantee is not obvious from reading this.
func command(configDir, extensionsDir string, args ...string) *exec.Cmd {
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(),
		"AZURE_CONFIG_DIR="+configDir,
		"AZURE_EXTENSION_DIR="+extensionsDir,
	)
	return cmd
}

func Login(tenantID, configDir, extensionsDir string, useDeviceCode bool) error {
	args := []string{"login", "--tenant", tenantID}
	if useDeviceCode {
		args = append(args, "--use-device-code")
	}
	cmd := command(configDir, extensionsDir, args...)
	// Deliberate, and easy to undo by accident: the login is interactive, so
	// stdin must reach az; and az's stdout is sent to stderr because azsel
	// keeps stdout clean for the shell to consume.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return run(cmd)
}

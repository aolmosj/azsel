package azure

import (
	"bytes"
	"encoding/json"
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
// browser opening or a network. #9 and #10 need this seam more than this
// package does: one has to simulate a failed login, the other a missing az.
var run = func(cmd *exec.Cmd) error { return cmd.Run() }

// lookPath resolves an executable on PATH, injectable for the same reason.
var lookPath = exec.LookPath

type AccountInfo struct {
	TenantID string `json:"tenantId"`
	Name     string `json:"name"`
	User     struct {
		Name string `json:"name"`
	} `json:"user"`
}

// Available reports whether the Azure CLI can be found. Commands that shell
// out to az should check this before doing anything the user would have to
// undo — see #10.
func Available() error {
	if _, err := lookPath(binary); err != nil {
		return fmt.Errorf("Azure CLI (%s) not found in PATH: %w", binary, err)
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

func AccountShow(configDir, extensionsDir string) (*AccountInfo, error) {
	cmd := command(configDir, extensionsDir, "account", "show", "--output", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := run(cmd); err != nil {
		// az explains itself on stderr; without this the caller only sees
		// "exit status 1".
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("az account show: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("az account show: %w", err)
	}

	var info AccountInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return nil, fmt.Errorf("parsing account info: %w", err)
	}
	return &info, nil
}

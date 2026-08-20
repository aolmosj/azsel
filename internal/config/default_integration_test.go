package config_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

// These tests run the real Azure CLI to pin the behaviours the whole default
// mechanism rests on: az follows the ~/.azure symlink, AZURE_CONFIG_DIR still
// overrides it, and with no default az uses its own directory. They were
// verified by hand while designing #23; here they become a guard against a
// future az — or a future azsel change — breaking a claim silently.
//
// They are opt-in rather than skip-if-absent, so a CI runner that happens not
// to ship az cannot let them pass as green by skipping (the mistake #18 fixed
// for the shell tests). Run them with:
//
//	AZSEL_INTEGRATION=1 go test ./internal/config/
//
// `az config set` is local — no login, no network.
func requireAzureCLI(t *testing.T) string {
	t.Helper()
	if os.Getenv("AZSEL_INTEGRATION") == "" {
		t.Skip("integration test; set AZSEL_INTEGRATION=1 to run")
	}
	az, err := exec.LookPath("az")
	if err != nil {
		t.Fatal("AZSEL_INTEGRATION set but az not found in PATH")
	}
	return az
}

// azConfigSet runs `az config set` with the given HOME and AZURE_CONFIG_DIR
// (empty to leave it unset), a local write that lands in az's config dir.
func azConfigSet(t *testing.T, az, home, configDir string) {
	t.Helper()
	cmd := exec.Command(az, "config", "set", "core.collect_telemetry=false", "--only-show-errors")
	env := []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
	if configDir != "" {
		env = append(env, "AZURE_CONFIG_DIR="+configDir)
	}
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("az config set: %v\n%s", err, out)
	}
}

// wrote reports whether az left a config file in dir.
func wrote(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "config"))
	return err == nil
}

// Core claim: with ~/.azure a symlink to a tenant and no AZURE_CONFIG_DIR, az
// writes into that tenant. This is also the non-interactive guarantee — no rc
// is loaded here, yet the default is honoured, because the link is a
// filesystem fact rather than an environment variable.
func TestIntegrationAzFollowsDefaultLink(t *testing.T) {
	az := requireAzureCLI(t)
	home, cfg := azureSandbox(t, "contoso")
	if _, err := config.SetDefault(cfg, "contoso", "20260220-120000"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	_ = home

	azConfigSet(t, az, home, "")

	tenant := cfg.FindTenant("contoso").ConfigDir
	if !wrote(tenant) {
		t.Error("az no escribió en el tenant por defecto vía el enlace")
	}
}

// AZURE_CONFIG_DIR must still win, so azsel use keeps controlling the session.
func TestIntegrationConfigDirOverridesLink(t *testing.T) {
	az := requireAzureCLI(t)
	home, cfg := azureSandbox(t, "contoso", "fabrikam")
	if _, err := config.SetDefault(cfg, "contoso", "20260220-120000"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	other := cfg.FindTenant("fabrikam").ConfigDir
	azConfigSet(t, az, home, other)

	if !wrote(other) {
		t.Error("az no respetó AZURE_CONFIG_DIR")
	}
	if wrote(cfg.FindTenant("contoso").ConfigDir) {
		t.Error("az escribió en el default pese a AZURE_CONFIG_DIR; la variable debe ganar")
	}
}

// With no default (real ~/.azure), az uses its own directory.
func TestIntegrationNoDefaultUsesNativeAzure(t *testing.T) {
	az := requireAzureCLI(t)
	home, _ := azureSandbox(t)
	azure := filepath.Join(home, ".azure")
	if err := os.MkdirAll(azure, 0755); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	azConfigSet(t, az, home, "")
	if !wrote(azure) {
		t.Error("az no usó su ~/.azure nativo sin default")
	}
}

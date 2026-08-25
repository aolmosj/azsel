package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

// azureSandbox isolates both directories the default mechanism touches:
// ~/.azsel via AZSEL_HOME, and ~/.azure via HOME. It returns the fake home so
// a test can build ~/.azure paths under it, and a config with the given
// tenants already present.
func azureSandbox(t *testing.T, tenants ...string) (home string, cfg *config.Config) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.EnvHome, filepath.Join(home, ".azsel"))

	cfg = &config.Config{}
	for _, name := range tenants {
		dir, _, err := config.EnsureTenantDir(name)
		if err != nil {
			t.Fatalf("setup tenant %q: %v", name, err)
		}
		cfg.Tenants = append(cfg.Tenants, config.Tenant{Name: name, ConfigDir: dir})
	}
	return home, cfg
}

func azurePath(t *testing.T, home string) string {
	t.Helper()
	return filepath.Join(home, ".azure")
}

func TestResolveDefaultNone(t *testing.T) {
	_, cfg := azureSandbox(t)
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		t.Fatalf("ResolveDefault: %v", err)
	}
	if info.State != config.DefaultNone {
		t.Errorf("State = %v, wanted DefaultNone", info.State)
	}
	if info.Tenant != "" {
		t.Errorf("Tenant = %q, wanted empty", info.Tenant)
	}
}

func TestResolveDefaultNative(t *testing.T) {
	home, cfg := azureSandbox(t)
	// ~/.azure is a real directory: az's native profile.
	if err := os.MkdirAll(azurePath(t, home), 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		t.Fatalf("ResolveDefault: %v", err)
	}
	if info.State != config.DefaultNative {
		t.Errorf("State = %v, wanted DefaultNative", info.State)
	}
}

func TestResolveDefaultSet(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	target := cfg.FindTenant("contoso").ConfigDir
	if err := os.Symlink(target, azurePath(t, home)); err != nil {
		t.Fatalf("setup link: %v", err)
	}
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		t.Fatalf("ResolveDefault: %v", err)
	}
	if info.State != config.DefaultSet {
		t.Fatalf("State = %v, wanted DefaultSet", info.State)
	}
	if info.Tenant != "contoso" {
		t.Errorf("Tenant = %q, wanted 'contoso'", info.Tenant)
	}
	if info.Target != target {
		t.Errorf("Target = %q, wanted %q", info.Target, target)
	}
}

// Link to a directory inside ~/.azsel/tenants but of a tenant not in
// config.json: it is not an azsel default.
func TestResolveDefaultUnknownTenant(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	// We create 'fabrikam's directory but do NOT add it to the config.
	ghost, _, err := config.EnsureTenantDir("fabrikam")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(ghost, azurePath(t, home)); err != nil {
		t.Fatalf("setup link: %v", err)
	}
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		t.Fatalf("ResolveDefault: %v", err)
	}
	if info.State != config.DefaultForeign {
		t.Errorf("State = %v, wanted DefaultForeign (unknown tenant)", info.State)
	}
}

// Link to something completely outside ~/.azsel.
func TestResolveDefaultForeign(t *testing.T) {
	home, cfg := azureSandbox(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, azurePath(t, home)); err != nil {
		t.Fatalf("setup link: %v", err)
	}
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		t.Fatalf("ResolveDefault: %v", err)
	}
	if info.State != config.DefaultForeign {
		t.Errorf("State = %v, wanted DefaultForeign", info.State)
	}
	if info.Target != outside {
		t.Errorf("Target = %q, wanted %q", info.Target, outside)
	}
}

// The dangerous case: a link whose target no longer exists. az blows up on
// this, so it must be distinguished.
func TestResolveDefaultBroken(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	target := cfg.FindTenant("contoso").ConfigDir
	if err := os.Symlink(target, azurePath(t, home)); err != nil {
		t.Fatalf("setup link: %v", err)
	}
	// Delete the target: the link dangles.
	if err := os.RemoveAll(target); err != nil {
		t.Fatalf("setup: %v", err)
	}
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		t.Fatalf("ResolveDefault: %v", err)
	}
	if info.State != config.DefaultBroken {
		t.Errorf("State = %v, wanted DefaultBroken", info.State)
	}
	if info.Target != target {
		t.Errorf("Target = %q, wanted %q to be able to diagnose", info.Target, target)
	}
}

// A link written with a relative path must resolve the same as an absolute
// one.
func TestResolveDefaultRelativeSymlink(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	target := cfg.FindTenant("contoso").ConfigDir
	rel, err := filepath.Rel(home, target)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Relative link from ~/ to .azsel/tenants/contoso
	if err := os.Symlink(rel, azurePath(t, home)); err != nil {
		t.Fatalf("setup relative link: %v", err)
	}
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		t.Fatalf("ResolveDefault: %v", err)
	}
	if info.State != config.DefaultSet || info.Tenant != "contoso" {
		t.Errorf("State=%v Tenant=%q, wanted DefaultSet/contoso", info.State, info.Tenant)
	}
}

// A link pointing DEEPER INTO a tenant (not at the tenant's directory) must
// not count as an azsel default.
func TestResolveDefaultLinkDeeperThanTenant(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	sub := filepath.Join(cfg.FindTenant("contoso").ConfigDir, "cliextensions")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(sub, azurePath(t, home)); err != nil {
		t.Fatalf("setup link: %v", err)
	}
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		t.Fatalf("ResolveDefault: %v", err)
	}
	if info.State != config.DefaultForeign {
		t.Errorf("State = %v, wanted DefaultForeign (points inside the tenant, not at the tenant)", info.State)
	}
}

// AzureDir needs HOME to locate ~/.azure.
func TestAzureDirNeedsHome(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := config.AzureDir(); err == nil {
		t.Fatal("AzureDir returned nil with no HOME")
	}
}

// A failure to resolve the link target that is NOT "does not exist" — here a
// path component that is a file, not a directory — must propagate as an error,
// not be confused with a broken link.
func TestResolveDefaultTargetStatError(t *testing.T) {
	home, cfg := azureSandbox(t)
	// A file where a directory should be, and the link points at something
	// below it: Stat fails with ENOTDIR, which is not IsNotExist.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(filepath.Join(blocker, "dentro"), azurePath(t, home)); err != nil {
		t.Fatalf("setup link: %v", err)
	}
	_, err := config.ResolveDefault(cfg)
	if err == nil {
		t.Fatal("ResolveDefault did not propagate the target's Stat error")
	}
}

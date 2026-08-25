package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

// removeSandbox isolates ~/.azsel and ~/.azure and returns the fake home.
func removeSandbox(t *testing.T, tenants ...string) (home string, cfg *config.Config) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.EnvHome, filepath.Join(home, ".azsel"))
	cfg = &config.Config{}
	for _, name := range tenants {
		dir, _, err := config.EnsureTenantDir(name)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		cfg.Tenants = append(cfg.Tenants, config.Tenant{Name: name, ConfigDir: dir})
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return home, cfg
}

// Removing the default tenant must clear the link BEFORE deleting the
// directory; otherwise ~/.azure is left dangling and az blows up.
func TestRemoveDefaultTenantClearsLink(t *testing.T) {
	home, cfg := removeSandbox(t, "contoso")
	if _, err := config.SetDefault(cfg, "contoso", "20260220-120000"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	quiet(t)

	if err := run(t, newRemoveCmd(), "contoso", "-f"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// The link must not remain, neither dangling nor of any kind.
	if _, err := os.Lstat(filepath.Join(home, ".azure")); !os.IsNotExist(err) {
		t.Errorf("~/.azure still exists after removing the default tenant (err=%v)", err)
	}
	// And the tenant disappeared from the config.
	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.FindTenant("contoso") != nil {
		t.Error("the tenant is still in config.json")
	}
}

// Removing a tenant that is NOT the default leaves the link intact.
func TestRemoveNonDefaultTenantLeavesLink(t *testing.T) {
	home, cfg := removeSandbox(t, "contoso", "fabrikam")
	if _, err := config.SetDefault(cfg, "contoso", "20260220-120000"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	quiet(t)

	if err := run(t, newRemoveCmd(), "fabrikam", "-f"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// The link to contoso is still alive and pointing correctly.
	got, err := os.Readlink(filepath.Join(home, ".azure"))
	if err != nil {
		t.Fatalf("the default's link disappeared: %v", err)
	}
	if want := cfg.FindTenant("contoso").ConfigDir; got != want {
		t.Errorf("~/.azure -> %q, wanted %q", got, want)
	}
}

// Bug from the review: if the default's link is already broken (the tenant
// deleted by hand) and then `azsel remove` is run, the link was left dangling
// because the guard only looked at DefaultSet, not DefaultBroken.
func TestRemoveTenantWithAlreadyBrokenDefaultLink(t *testing.T) {
	home, cfg := removeSandbox(t, "contoso")
	if _, err := config.SetDefault(cfg, "contoso", "20260220-120000"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	// We break the link by deleting the tenant's directory by hand — now
	// ~/.azure is a dangling link to contoso.
	if err := os.RemoveAll(filepath.Join(home, ".azsel", "tenants", "contoso")); err != nil {
		t.Fatalf("setup: %v", err)
	}
	quiet(t)

	if err := run(t, newRemoveCmd(), "contoso", "-f"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// The broken link must not survive the removal.
	if _, err := os.Lstat(filepath.Join(home, ".azure")); !os.IsNotExist(err) {
		t.Errorf("~/.azure still exists (dangling) after removing the tenant (err=%v)", err)
	}
}

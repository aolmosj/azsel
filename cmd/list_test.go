package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

func listSandbox(t *testing.T, tenants ...string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.EnvHome, filepath.Join(home, ".azsel"))
	cfg := &config.Config{}
	for _, name := range tenants {
		dir, _, err := config.EnsureTenantDir(name)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		cfg.Tenants = append(cfg.Tenants, config.Tenant{Name: name, TenantID: name + "-id", ConfigDir: dir})
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return home
}

// list separates active from default: the active one comes from
// AZURE_CONFIG_DIR, the default from the link. They must be able to be
// different tenants.
func TestListShowsActiveAndDefaultSeparately(t *testing.T) {
	home := listSandbox(t, "contoso", "fabrikam")
	cfg, _ := config.Load()
	if _, err := config.SetDefault(cfg, "contoso", "20260220-120000"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	// active = fabrikam, default = contoso
	t.Setenv("AZURE_CONFIG_DIR", filepath.Join(home, ".azsel", "tenants", "fabrikam"))

	out := quiet(t)
	if err := run(t, newListCmd()); err != nil {
		t.Fatalf("list: %v", err)
	}
	got := out()
	if !strings.Contains(got, "DEFAULT") {
		t.Errorf("the DEFAULT column is missing:\n%s", got)
	}
	// contoso's line has D but not *; fabrikam's the other way around.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "contoso") && !strings.Contains(line, "D") {
			t.Errorf("contoso should be marked as default: %q", line)
		}
		if strings.Contains(line, "fabrikam") && !strings.HasPrefix(strings.TrimSpace(line), "*") {
			t.Errorf("fabrikam should be marked as active: %q", line)
		}
	}
}

func TestListWarnsOnBrokenDefault(t *testing.T) {
	home := listSandbox(t, "contoso")
	cfg, _ := config.Load()
	if _, err := config.SetDefault(cfg, "contoso", "20260220-120000"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	// We break the link by deleting the target by hand.
	if err := os.RemoveAll(filepath.Join(home, ".azsel", "tenants", "contoso")); err != nil {
		t.Fatalf("setup: %v", err)
	}
	out := quiet(t)
	if err := run(t, newListCmd()); err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := out(); !strings.Contains(got, "broken symlink") {
		t.Errorf("list did not warn about the broken link:\n%s", got)
	}
}

func TestListNoDefault(t *testing.T) {
	listSandbox(t, "contoso")
	out := quiet(t)
	if err := run(t, newListCmd()); err != nil {
		t.Fatalf("list: %v", err)
	}
	got := out()
	if !strings.Contains(got, "DEFAULT") {
		t.Errorf("the DEFAULT header must always be present:\n%s", got)
	}
	// Without a default, no tenant line has the D marked.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "contoso") && strings.Contains(strings.Fields(line)[0], "D") {
			t.Errorf("contoso marked as default without it being set: %q", line)
		}
	}
}

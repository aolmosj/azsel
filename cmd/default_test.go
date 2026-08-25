package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

func defaultSandbox(t *testing.T, tenants ...string) string {
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
		cfg.Tenants = append(cfg.Tenants, config.Tenant{Name: name, ConfigDir: dir})
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return home
}

func TestDefaultSetCreatesLink(t *testing.T) {
	home := defaultSandbox(t, "contoso")
	out := quiet(t)
	if err := run(t, newDefaultCmd(), "contoso"); err != nil {
		t.Fatalf("default contoso: %v", err)
	}
	target, err := os.Readlink(filepath.Join(home, ".azure"))
	if err != nil {
		t.Fatalf("~/.azure is not a link: %v", err)
	}
	if !strings.HasSuffix(target, filepath.Join("tenants", "contoso")) {
		t.Errorf("~/.azure -> %q, wanted the contoso tenant", target)
	}
	if got := out(); !strings.Contains(got, "Default tenant set") {
		t.Errorf("output = %q", got)
	}
}

func TestDefaultSetBacksUpRealAzure(t *testing.T) {
	home := defaultSandbox(t, "contoso")
	azure := filepath.Join(home, ".azure")
	if err := os.MkdirAll(azure, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(azure, "tok"), []byte("x"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	out := quiet(t)
	if err := run(t, newDefaultCmd(), "contoso"); err != nil {
		t.Fatalf("default: %v", err)
	}
	if got := out(); !strings.Contains(got, "Moved your existing ~/.azure") {
		t.Errorf("the backup was not reported: %q", got)
	}
	// The backup exists and keeps its content.
	entries, err := os.ReadDir(filepath.Join(home, ".azsel", "backups"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("backups = %v (err %v)", entries, err)
	}
}

func TestDefaultShow(t *testing.T) {
	defaultSandbox(t, "contoso")
	out := quiet(t)
	if err := run(t, newDefaultCmd()); err != nil {
		t.Fatalf("default: %v", err)
	}
	if got := out(); !strings.Contains(got, "No default set") {
		t.Errorf("output = %q, wanted «No default set»", got)
	}
}

func TestDefaultShowWhenSet(t *testing.T) {
	defaultSandbox(t, "contoso")
	quiet(t)
	if err := run(t, newDefaultCmd(), "contoso"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out := quiet(t)
	if err := run(t, newDefaultCmd()); err != nil {
		t.Fatalf("show: %v", err)
	}
	if got := out(); !strings.Contains(got, "Default tenant: contoso") {
		t.Errorf("output = %q", got)
	}
}

func TestDefaultClear(t *testing.T) {
	home := defaultSandbox(t, "contoso")
	quiet(t)
	if err := run(t, newDefaultCmd(), "contoso"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out := quiet(t)
	if err := run(t, newDefaultCmd(), "--clear"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".azure")); !os.IsNotExist(err) {
		t.Error("~/.azure still exists after --clear")
	}
	if got := out(); !strings.Contains(got, "Default cleared") {
		t.Errorf("output = %q", got)
	}
}

func TestDefaultClearReportsBackup(t *testing.T) {
	home := defaultSandbox(t, "contoso")
	if err := os.MkdirAll(filepath.Join(home, ".azure"), 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	quiet(t)
	if err := run(t, newDefaultCmd(), "contoso"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out := quiet(t)
	if err := run(t, newDefaultCmd(), "--clear"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := out(); !strings.Contains(got, "Restore it with:") {
		t.Errorf("clear did not explain how to restore: %q", got)
	}
}

func TestDefaultRejectsUnknownTenant(t *testing.T) {
	defaultSandbox(t, "contoso")
	quiet(t)
	if err := run(t, newDefaultCmd(), "ghost"); err == nil {
		t.Fatal("default accepted a nonexistent tenant")
	}
}

func TestDefaultClearWithNameIsError(t *testing.T) {
	defaultSandbox(t, "contoso")
	quiet(t)
	if err := run(t, newDefaultCmd(), "contoso", "--clear"); err == nil {
		t.Fatal("default --clear with a name should fail")
	}
}

// Showing a broken link should explain the problem, not fake normality.
func TestDefaultShowBrokenLink(t *testing.T) {
	home := defaultSandbox(t, "contoso")
	quiet(t)
	if err := run(t, newDefaultCmd(), "contoso"); err != nil {
		t.Fatalf("set: %v", err)
	}
	// We break the link by deleting the tenant by hand (without going through remove).
	if err := os.RemoveAll(filepath.Join(home, ".azsel", "tenants", "contoso")); err != nil {
		t.Fatalf("setup: %v", err)
	}
	out := quiet(t)
	if err := run(t, newDefaultCmd()); err != nil {
		t.Fatalf("show: %v", err)
	}
	if got := out(); !strings.Contains(got, "broken symlink") {
		t.Errorf("the broken link was not reported: %q", got)
	}
}

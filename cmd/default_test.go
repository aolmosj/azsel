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
			t.Fatalf("preparando: %v", err)
		}
		cfg.Tenants = append(cfg.Tenants, config.Tenant{Name: name, ConfigDir: dir})
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("preparando: %v", err)
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
		t.Fatalf("~/.azure no es enlace: %v", err)
	}
	if !strings.HasSuffix(target, filepath.Join("tenants", "contoso")) {
		t.Errorf("~/.azure -> %q, quería el tenant contoso", target)
	}
	if got := out(); !strings.Contains(got, "Default tenant set") {
		t.Errorf("salida = %q", got)
	}
}

func TestDefaultSetBacksUpRealAzure(t *testing.T) {
	home := defaultSandbox(t, "contoso")
	azure := filepath.Join(home, ".azure")
	if err := os.MkdirAll(azure, 0755); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	if err := os.WriteFile(filepath.Join(azure, "tok"), []byte("x"), 0600); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	out := quiet(t)
	if err := run(t, newDefaultCmd(), "contoso"); err != nil {
		t.Fatalf("default: %v", err)
	}
	if got := out(); !strings.Contains(got, "Moved your existing ~/.azure") {
		t.Errorf("no se informó del backup: %q", got)
	}
	// El backup existe y conserva el contenido.
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
		t.Errorf("salida = %q, quería «No default set»", got)
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
		t.Errorf("salida = %q", got)
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
		t.Error("~/.azure sigue existiendo tras --clear")
	}
	if got := out(); !strings.Contains(got, "Default cleared") {
		t.Errorf("salida = %q", got)
	}
}

func TestDefaultClearReportsBackup(t *testing.T) {
	home := defaultSandbox(t, "contoso")
	if err := os.MkdirAll(filepath.Join(home, ".azure"), 0755); err != nil {
		t.Fatalf("preparando: %v", err)
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
		t.Errorf("clear no explicó cómo restaurar: %q", got)
	}
}

func TestDefaultRejectsUnknownTenant(t *testing.T) {
	defaultSandbox(t, "contoso")
	quiet(t)
	if err := run(t, newDefaultCmd(), "fantasma"); err == nil {
		t.Fatal("default aceptó un tenant inexistente")
	}
}

func TestDefaultClearWithNameIsError(t *testing.T) {
	defaultSandbox(t, "contoso")
	quiet(t)
	if err := run(t, newDefaultCmd(), "contoso", "--clear"); err == nil {
		t.Fatal("default --clear con nombre debería fallar")
	}
}

// Mostrar un enlace roto debe explicar el problema, no fingir normalidad.
func TestDefaultShowBrokenLink(t *testing.T) {
	home := defaultSandbox(t, "contoso")
	quiet(t)
	if err := run(t, newDefaultCmd(), "contoso"); err != nil {
		t.Fatalf("set: %v", err)
	}
	// Rompemos el enlace borrando el tenant a mano (sin pasar por remove).
	if err := os.RemoveAll(filepath.Join(home, ".azsel", "tenants", "contoso")); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	out := quiet(t)
	if err := run(t, newDefaultCmd()); err != nil {
		t.Fatalf("show: %v", err)
	}
	if got := out(); !strings.Contains(got, "broken symlink") {
		t.Errorf("no se avisó del enlace roto: %q", got)
	}
}

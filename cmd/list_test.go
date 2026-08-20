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
			t.Fatalf("preparando: %v", err)
		}
		cfg.Tenants = append(cfg.Tenants, config.Tenant{Name: name, TenantID: name + "-id", ConfigDir: dir})
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	return home
}

// list separa activo de default: el activo sale de AZURE_CONFIG_DIR, el
// default del enlace. Deben poder ser tenants distintos.
func TestListShowsActiveAndDefaultSeparately(t *testing.T) {
	home := listSandbox(t, "contoso", "fabrikam")
	cfg, _ := config.Load()
	if _, err := config.SetDefault(cfg, "contoso", "20260220-120000"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	// activo = fabrikam, default = contoso
	t.Setenv("AZURE_CONFIG_DIR", filepath.Join(home, ".azsel", "tenants", "fabrikam"))

	out := quiet(t)
	if err := run(t, newListCmd()); err != nil {
		t.Fatalf("list: %v", err)
	}
	got := out()
	if !strings.Contains(got, "DEFAULT") {
		t.Errorf("falta la columna DEFAULT:\n%s", got)
	}
	// La línea de contoso lleva D pero no *; la de fabrikam al revés.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "contoso") && !strings.Contains(line, "D") {
			t.Errorf("contoso debería marcarse como default: %q", line)
		}
		if strings.Contains(line, "fabrikam") && !strings.HasPrefix(strings.TrimSpace(line), "*") {
			t.Errorf("fabrikam debería marcarse como activo: %q", line)
		}
	}
}

func TestListWarnsOnBrokenDefault(t *testing.T) {
	home := listSandbox(t, "contoso")
	cfg, _ := config.Load()
	if _, err := config.SetDefault(cfg, "contoso", "20260220-120000"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	// Rompemos el enlace borrando el destino a mano.
	if err := os.RemoveAll(filepath.Join(home, ".azsel", "tenants", "contoso")); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	out := quiet(t)
	if err := run(t, newListCmd()); err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := out(); !strings.Contains(got, "broken symlink") {
		t.Errorf("list no avisó del enlace roto:\n%s", got)
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
		t.Errorf("la cabecera DEFAULT debe estar siempre:\n%s", got)
	}
	// Sin default, ninguna línea de tenant lleva la D marcada.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "contoso") && strings.Contains(strings.Fields(line)[0], "D") {
			t.Errorf("contoso marcado como default sin haberlo fijado: %q", line)
		}
	}
}

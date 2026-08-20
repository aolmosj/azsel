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
			t.Fatalf("preparando: %v", err)
		}
		cfg.Tenants = append(cfg.Tenants, config.Tenant{Name: name, ConfigDir: dir})
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	return home, cfg
}

// Borrar el tenant por defecto debe eliminar el enlace ANTES de borrar el
// directorio; si no, ~/.azure queda colgando y az revienta.
func TestRemoveDefaultTenantClearsLink(t *testing.T) {
	home, cfg := removeSandbox(t, "contoso")
	if _, err := config.SetDefault(cfg, "contoso", "20260220-120000"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	quiet(t)

	if err := run(t, newRemoveCmd(), "contoso", "-f"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// El enlace no debe quedar, ni colgando ni de ningún tipo.
	if _, err := os.Lstat(filepath.Join(home, ".azure")); !os.IsNotExist(err) {
		t.Errorf("~/.azure sigue existiendo tras borrar el tenant por defecto (err=%v)", err)
	}
	// Y el tenant desapareció del config.
	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.FindTenant("contoso") != nil {
		t.Error("el tenant sigue en config.json")
	}
}

// Borrar un tenant que NO es el default deja el enlace intacto.
func TestRemoveNonDefaultTenantLeavesLink(t *testing.T) {
	home, cfg := removeSandbox(t, "contoso", "fabrikam")
	if _, err := config.SetDefault(cfg, "contoso", "20260220-120000"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	quiet(t)

	if err := run(t, newRemoveCmd(), "fabrikam", "-f"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// El enlace a contoso sigue vivo y apuntando bien.
	got, err := os.Readlink(filepath.Join(home, ".azure"))
	if err != nil {
		t.Fatalf("el enlace del default desapareció: %v", err)
	}
	if want := cfg.FindTenant("contoso").ConfigDir; got != want {
		t.Errorf("~/.azure -> %q, quería %q", got, want)
	}
}

// Bug del review: si el enlace del default ya está roto (el tenant borrado a
// mano) y luego se hace `azsel remove`, el enlace quedaba colgando porque el
// guard solo miraba DefaultSet, no DefaultBroken.
func TestRemoveTenantWithAlreadyBrokenDefaultLink(t *testing.T) {
	home, cfg := removeSandbox(t, "contoso")
	if _, err := config.SetDefault(cfg, "contoso", "20260220-120000"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	// Rompemos el enlace borrando el directorio del tenant a mano — ahora
	// ~/.azure es un enlace colgando hacia contoso.
	if err := os.RemoveAll(filepath.Join(home, ".azsel", "tenants", "contoso")); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	quiet(t)

	if err := run(t, newRemoveCmd(), "contoso", "-f"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// El enlace roto no debe sobrevivir a la eliminación.
	if _, err := os.Lstat(filepath.Join(home, ".azure")); !os.IsNotExist(err) {
		t.Errorf("~/.azure sigue existiendo (colgando) tras borrar el tenant (err=%v)", err)
	}
}

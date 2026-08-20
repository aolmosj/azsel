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
			t.Fatalf("preparando tenant %q: %v", name, err)
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
		t.Errorf("State = %v, quería DefaultNone", info.State)
	}
	if info.Tenant != "" {
		t.Errorf("Tenant = %q, quería vacío", info.Tenant)
	}
}

func TestResolveDefaultNative(t *testing.T) {
	home, cfg := azureSandbox(t)
	// ~/.azure es un directorio real: el perfil nativo de az.
	if err := os.MkdirAll(azurePath(t, home), 0755); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		t.Fatalf("ResolveDefault: %v", err)
	}
	if info.State != config.DefaultNative {
		t.Errorf("State = %v, quería DefaultNative", info.State)
	}
}

func TestResolveDefaultSet(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	target := cfg.FindTenant("contoso").ConfigDir
	if err := os.Symlink(target, azurePath(t, home)); err != nil {
		t.Fatalf("preparando enlace: %v", err)
	}
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		t.Fatalf("ResolveDefault: %v", err)
	}
	if info.State != config.DefaultSet {
		t.Fatalf("State = %v, quería DefaultSet", info.State)
	}
	if info.Tenant != "contoso" {
		t.Errorf("Tenant = %q, quería «contoso»", info.Tenant)
	}
	if info.Target != target {
		t.Errorf("Target = %q, quería %q", info.Target, target)
	}
}

// Enlace a un directorio dentro de ~/.azsel/tenants pero de un tenant que no
// está en config.json: no es un default de azsel.
func TestResolveDefaultUnknownTenant(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	// Creamos el directorio de 'fabrikam' pero NO lo añadimos al config.
	ghost, _, err := config.EnsureTenantDir("fabrikam")
	if err != nil {
		t.Fatalf("preparando: %v", err)
	}
	if err := os.Symlink(ghost, azurePath(t, home)); err != nil {
		t.Fatalf("preparando enlace: %v", err)
	}
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		t.Fatalf("ResolveDefault: %v", err)
	}
	if info.State != config.DefaultForeign {
		t.Errorf("State = %v, quería DefaultForeign (tenant desconocido)", info.State)
	}
}

// Enlace a algo completamente fuera de ~/.azsel.
func TestResolveDefaultForeign(t *testing.T) {
	home, cfg := azureSandbox(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, azurePath(t, home)); err != nil {
		t.Fatalf("preparando enlace: %v", err)
	}
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		t.Fatalf("ResolveDefault: %v", err)
	}
	if info.State != config.DefaultForeign {
		t.Errorf("State = %v, quería DefaultForeign", info.State)
	}
	if info.Target != outside {
		t.Errorf("Target = %q, quería %q", info.Target, outside)
	}
}

// El caso peligroso: enlace cuyo destino ya no existe. az revienta con esto,
// así que hay que distinguirlo.
func TestResolveDefaultBroken(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	target := cfg.FindTenant("contoso").ConfigDir
	if err := os.Symlink(target, azurePath(t, home)); err != nil {
		t.Fatalf("preparando enlace: %v", err)
	}
	// Borramos el destino: el enlace queda colgando.
	if err := os.RemoveAll(target); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		t.Fatalf("ResolveDefault: %v", err)
	}
	if info.State != config.DefaultBroken {
		t.Errorf("State = %v, quería DefaultBroken", info.State)
	}
	if info.Target != target {
		t.Errorf("Target = %q, quería %q para poder diagnosticar", info.Target, target)
	}
}

// Un enlace escrito con ruta relativa debe resolverse igual que uno absoluto.
func TestResolveDefaultRelativeSymlink(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	target := cfg.FindTenant("contoso").ConfigDir
	rel, err := filepath.Rel(home, target)
	if err != nil {
		t.Fatalf("preparando: %v", err)
	}
	// Enlace relativo desde ~/ hacia .azsel/tenants/contoso
	if err := os.Symlink(rel, azurePath(t, home)); err != nil {
		t.Fatalf("preparando enlace relativo: %v", err)
	}
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		t.Fatalf("ResolveDefault: %v", err)
	}
	if info.State != config.DefaultSet || info.Tenant != "contoso" {
		t.Errorf("State=%v Tenant=%q, quería DefaultSet/contoso", info.State, info.Tenant)
	}
}

// Un enlace que apunta MÁS ADENTRO de un tenant (no al directorio del tenant)
// no debe contar como default de azsel.
func TestResolveDefaultLinkDeeperThanTenant(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	sub := filepath.Join(cfg.FindTenant("contoso").ConfigDir, "cliextensions")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	if err := os.Symlink(sub, azurePath(t, home)); err != nil {
		t.Fatalf("preparando enlace: %v", err)
	}
	info, err := config.ResolveDefault(cfg)
	if err != nil {
		t.Fatalf("ResolveDefault: %v", err)
	}
	if info.State != config.DefaultForeign {
		t.Errorf("State = %v, quería DefaultForeign (apunta dentro del tenant, no al tenant)", info.State)
	}
}

// AzureDir necesita HOME para poder situar ~/.azure.
func TestAzureDirNeedsHome(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := config.AzureDir(); err == nil {
		t.Fatal("AzureDir devolvió nil sin HOME")
	}
}

// Un fallo al resolver el destino del enlace que NO sea «no existe» —aquí un
// componente de la ruta que es un fichero, no un directorio— debe propagarse
// como error, no confundirse con un enlace roto.
func TestResolveDefaultTargetStatError(t *testing.T) {
	home, cfg := azureSandbox(t)
	// Un fichero donde debería haber un directorio, y el enlace apunta a algo
	// por debajo de él: Stat falla con ENOTDIR, que no es IsNotExist.
	blocker := filepath.Join(t.TempDir(), "fichero")
	if err := os.WriteFile(blocker, nil, 0644); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	if err := os.Symlink(filepath.Join(blocker, "dentro"), azurePath(t, home)); err != nil {
		t.Fatalf("preparando enlace: %v", err)
	}
	_, err := config.ResolveDefault(cfg)
	if err == nil {
		t.Fatal("ResolveDefault no propagó el error de Stat del destino")
	}
}

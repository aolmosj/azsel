package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

const ts = "20260220-120000"

// assertLinkTo checks ~/.azure is a symlink pointing at want.
func assertLinkTo(t *testing.T, home, want string) {
	t.Helper()
	azure := filepath.Join(home, ".azure")
	fi, err := os.Lstat(azure)
	if err != nil {
		t.Fatalf("~/.azure no existe: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("~/.azure no es un enlace")
	}
	got, _ := os.Readlink(azure)
	if got != want {
		t.Errorf("~/.azure -> %q, quería %q", got, want)
	}
}

func TestSetDefaultFromNothing(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	res, err := config.SetDefault(cfg, "contoso", ts)
	if err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	if res.BackupPath != "" {
		t.Errorf("BackupPath = %q, no debía respaldar nada", res.BackupPath)
	}
	if res.Repointed {
		t.Error("Repointed = true en creación fresca")
	}
	assertLinkTo(t, home, cfg.FindTenant("contoso").ConfigDir)
}

// El caso que motiva #29: ~/.azure es un directorio real con contenido.
func TestSetDefaultBacksUpRealDirectory(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	azure := filepath.Join(home, ".azure")
	if err := os.MkdirAll(azure, 0755); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	marker := filepath.Join(azure, "msal_token_cache.json")
	if err := os.WriteFile(marker, []byte("sesión valiosa"), 0600); err != nil {
		t.Fatalf("preparando: %v", err)
	}

	res, err := config.SetDefault(cfg, "contoso", ts)
	if err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	if res.BackupPath == "" {
		t.Fatal("no se reportó backup pese a haber un ~/.azure real")
	}
	// La sesión se conserva en el backup, no se destruye.
	data, err := os.ReadFile(filepath.Join(res.BackupPath, "msal_token_cache.json"))
	if err != nil {
		t.Fatalf("el contenido no sobrevivió al backup: %v", err)
	}
	if string(data) != "sesión valiosa" {
		t.Errorf("contenido del backup = %q", data)
	}
	assertLinkTo(t, home, cfg.FindTenant("contoso").ConfigDir)
}

func TestSetDefaultRepointsOwnLink(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso", "fabrikam")
	if _, err := config.SetDefault(cfg, "contoso", ts); err != nil {
		t.Fatalf("primer SetDefault: %v", err)
	}
	res, err := config.SetDefault(cfg, "fabrikam", ts)
	if err != nil {
		t.Fatalf("segundo SetDefault: %v", err)
	}
	if !res.Repointed {
		t.Error("Repointed = false al mover un enlace existente")
	}
	if res.BackupPath != "" {
		t.Error("se respaldó al repuntar un enlace propio; no debía")
	}
	assertLinkTo(t, home, cfg.FindTenant("fabrikam").ConfigDir)
}

// Un enlace ajeno no se toca.
func TestSetDefaultRefusesForeignLink(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	outside := t.TempDir()
	azure := filepath.Join(home, ".azure")
	if err := os.Symlink(outside, azure); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	_, err := config.SetDefault(cfg, "contoso", ts)
	if err == nil {
		t.Fatal("SetDefault pisó un enlace ajeno")
	}
	// El enlace ajeno sigue intacto.
	got, _ := os.Readlink(azure)
	if got != outside {
		t.Errorf("el enlace ajeno cambió a %q", got)
	}
}

func TestSetDefaultRejectsUnknownTenant(t *testing.T) {
	_, cfg := azureSandbox(t, "contoso")
	if _, err := config.SetDefault(cfg, "fantasma", ts); err == nil {
		t.Fatal("SetDefault aceptó un tenant inexistente")
	}
}

// Reemplaza un enlace roto que era nuestro (apuntaba a un tenant borrado).
func TestSetDefaultReplacesOwnBrokenLink(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso", "fabrikam")
	azure := filepath.Join(home, ".azure")
	// Enlace a un tenant que luego borramos: queda colgando pero es nuestro.
	gone := cfg.FindTenant("fabrikam").ConfigDir
	if err := os.Symlink(gone, azure); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	if _, err := config.SetDefault(cfg, "contoso", ts); err != nil {
		t.Fatalf("SetDefault sobre enlace roto propio: %v", err)
	}
	assertLinkTo(t, home, cfg.FindTenant("contoso").ConfigDir)
}

func TestSetDefaultCreatesSharedExtensionsLink(t *testing.T) {
	_, cfg := azureSandbox(t, "contoso")
	if _, err := config.SetDefault(cfg, "contoso", ts); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	shared, _ := config.ExtensionsDir()
	link := filepath.Join(cfg.FindTenant("contoso").ConfigDir, "cliextensions")
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("cliextensions no es enlace: %v", err)
	}
	if got != shared {
		t.Errorf("cliextensions -> %q, quería el compartido %q", got, shared)
	}
}

// Un cliextensions real preexistente (restos) se aparta, no se borra.
func TestSetDefaultMovesAsideStaleExtensions(t *testing.T) {
	_, cfg := azureSandbox(t, "contoso")
	stale := filepath.Join(cfg.FindTenant("contoso").ConfigDir, "cliextensions")
	if err := os.MkdirAll(stale, 0755); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stale, "vieja.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	if _, err := config.SetDefault(cfg, "contoso", ts); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	if _, err := os.Stat(stale + ".bak"); err != nil {
		t.Errorf("los restos no se apartaron a .bak: %v", err)
	}
}

func TestClearDefault(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	if _, err := config.SetDefault(cfg, "contoso", ts); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	res, err := config.ClearDefault(cfg)
	if err != nil {
		t.Fatalf("ClearDefault: %v", err)
	}
	if !res.Cleared {
		t.Error("Cleared = false pese a haber un default")
	}
	if _, err := os.Lstat(filepath.Join(home, ".azure")); !os.IsNotExist(err) {
		t.Errorf("~/.azure sigue existiendo tras clear (err=%v)", err)
	}
}

func TestClearDefaultReportsLatestBackup(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	azure := filepath.Join(home, ".azure")
	if err := os.MkdirAll(azure, 0755); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	if _, err := config.SetDefault(cfg, "contoso", ts); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	res, err := config.ClearDefault(cfg)
	if err != nil {
		t.Fatalf("ClearDefault: %v", err)
	}
	if res.LatestBackup == "" {
		t.Error("clear no reportó el backup existente")
	}
}

func TestClearDefaultNoDefault(t *testing.T) {
	_, cfg := azureSandbox(t)
	res, err := config.ClearDefault(cfg)
	if err != nil {
		t.Fatalf("ClearDefault sin default: %v", err)
	}
	if res.Cleared {
		t.Error("Cleared = true sin default")
	}
}

// clear no debe tocar el ~/.azure nativo de az.
func TestClearDefaultLeavesNativeDirectory(t *testing.T) {
	home, cfg := azureSandbox(t)
	azure := filepath.Join(home, ".azure")
	if err := os.MkdirAll(azure, 0755); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	if _, err := config.ClearDefault(cfg); err != nil {
		t.Fatalf("ClearDefault: %v", err)
	}
	if _, err := os.Stat(azure); err != nil {
		t.Errorf("clear borró el ~/.azure nativo: %v", err)
	}
}

// La renombrada del enlace es atómica: nunca hay un instante sin ~/.azure.
// Aquí lo que se comprueba es que un enlace previo se reemplaza sin pasar por
// un estado inexistente observable — al menos que el resultado sea correcto.
func TestReplaceIsAtomicResult(t *testing.T) {
	home, cfg := azureSandbox(t, "a", "b")
	if _, err := config.SetDefault(cfg, "a", ts); err != nil {
		t.Fatalf("set a: %v", err)
	}
	if _, err := config.SetDefault(cfg, "b", ts); err != nil {
		t.Fatalf("set b: %v", err)
	}
	// No debe quedar ningún fichero temporal.
	azure := filepath.Join(home, ".azure")
	if _, err := os.Lstat(azure + ".azsel-tmp." + itoa(os.Getpid())); !os.IsNotExist(err) {
		t.Error("quedó un enlace temporal")
	}
	assertLinkTo(t, home, cfg.FindTenant("b").ConfigDir)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

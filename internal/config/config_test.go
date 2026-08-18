package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

// sandbox points azsel at a throwaway directory for the duration of a test.
// Without it these tests would read and overwrite the developer's real
// ~/.azsel/config.json.
func sandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.EnvHome, dir)
	return dir
}

func TestBaseDirHonoursEnvHome(t *testing.T) {
	dir := sandbox(t)
	got, err := config.BaseDir()
	if err != nil {
		t.Fatalf("BaseDir: %v", err)
	}
	if got != dir {
		t.Errorf("BaseDir = %q, quería %q", got, dir)
	}
}

func TestBaseDirFallsBackToHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("azsel solo se publica para linux y darwin")
	}
	home := t.TempDir()
	t.Setenv(config.EnvHome, "")
	t.Setenv("HOME", home)

	got, err := config.BaseDir()
	if err != nil {
		t.Fatalf("BaseDir: %v", err)
	}
	if want := filepath.Join(home, ".azsel"); got != want {
		t.Errorf("BaseDir = %q, quería %q", got, want)
	}
}

// Preguntar por una ruta no debe tener efectos en el disco. Antes sí los
// tenía: cada función de ruta hacía MkdirAll.
func TestPathFunctionsCreateNothing(t *testing.T) {
	dir := sandbox(t)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("preparando: %v", err)
	}

	for _, fn := range []struct {
		name string
		call func() (string, error)
	}{
		{"BaseDir", config.BaseDir},
		{"ConfigPath", config.ConfigPath},
		{"EnvFile", config.EnvFile},
		{"ExtensionsDir", config.ExtensionsDir},
		{"TenantsDir", config.TenantsDir},
		{"TenantDir", func() (string, error) { return config.TenantDir("acme") }},
	} {
		if _, err := fn.call(); err != nil {
			t.Fatalf("%s: %v", fn.name, err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("%s creó %s", fn.name, dir)
		}
	}
}

func TestPathsHangOffBaseDir(t *testing.T) {
	dir := sandbox(t)
	cases := []struct {
		name string
		call func() (string, error)
		want string
	}{
		{"ConfigPath", config.ConfigPath, filepath.Join(dir, "config.json")},
		{"EnvFile", config.EnvFile, filepath.Join(dir, ".switch")},
		{"ExtensionsDir", config.ExtensionsDir, filepath.Join(dir, "extensions")},
		{"TenantsDir", config.TenantsDir, filepath.Join(dir, "tenants")},
		{"TenantDir", func() (string, error) { return config.TenantDir("acme") },
			filepath.Join(dir, "tenants", "acme")},
	}
	for _, c := range cases {
		got, err := c.call()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s = %q, quería %q", c.name, got, c.want)
		}
	}
}

func TestEnsureBaseDirCreates(t *testing.T) {
	dir := sandbox(t)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	if _, err := config.EnsureBaseDir(); err != nil {
		t.Fatalf("EnsureBaseDir: %v", err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("no se creó %s: %v", dir, err)
	}
}

// El bool 'created' es lo que permitirá a #9 revertir un login fallido sin
// borrar un directorio preexistente con credenciales válidas.
func TestEnsureTenantDirReportsWhoCreatedIt(t *testing.T) {
	sandbox(t)

	dir, created, err := config.EnsureTenantDir("acme")
	if err != nil {
		t.Fatalf("EnsureTenantDir: %v", err)
	}
	if !created {
		t.Error("created = false en la primera llamada, quería true")
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("no se creó %s: %v", dir, err)
	}

	_, created, err = config.EnsureTenantDir("acme")
	if err != nil {
		t.Fatalf("EnsureTenantDir (segunda): %v", err)
	}
	if created {
		t.Error("created = true sobre un directorio preexistente, quería false")
	}
}

func TestEnsureExtensionsDirCreates(t *testing.T) {
	dir := sandbox(t)
	got, err := config.EnsureExtensionsDir()
	if err != nil {
		t.Fatalf("EnsureExtensionsDir: %v", err)
	}
	if want := filepath.Join(dir, "extensions"); got != want {
		t.Errorf("= %q, quería %q", got, want)
	}
	if fi, err := os.Stat(got); err != nil || !fi.IsDir() {
		t.Fatalf("no se creó %s: %v", got, err)
	}
}

func TestLoadMissingFileReturnsEmptyConfig(t *testing.T) {
	sandbox(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load sobre fichero inexistente devolvió error: %v", err)
	}
	if len(cfg.Tenants) != 0 {
		t.Errorf("Tenants = %v, quería vacío", cfg.Tenants)
	}
}

func TestLoadCorruptJSON(t *testing.T) {
	dir := sandbox(t)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{no json"), 0644); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	_, err := config.Load()
	if err == nil {
		t.Fatal("Load devolvió nil sobre JSON corrupto")
	}
	if !strings.Contains(err.Error(), "parsing config") {
		t.Errorf("error = %q, quería que mencionara «parsing config»", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	sandbox(t)
	want := &config.Config{Tenants: []config.Tenant{
		{Name: "acme", TenantID: "11111111-1111-1111-1111-111111111111", ConfigDir: "/tmp/acme"},
		{Name: "globex", TenantID: "22222222-2222-2222-2222-222222222222", ConfigDir: "/tmp/globex"},
	}}
	if err := config.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Tenants) != len(want.Tenants) {
		t.Fatalf("%d tenants, quería %d", len(got.Tenants), len(want.Tenants))
	}
	for i := range want.Tenants {
		if got.Tenants[i] != want.Tenants[i] {
			t.Errorf("tenant %d = %+v, quería %+v", i, got.Tenants[i], want.Tenants[i])
		}
	}
}

// Save debe crear el directorio base si no existe: es el primer contacto
// con el disco en una instalación nueva.
func TestSaveCreatesBaseDir(t *testing.T) {
	dir := sandbox(t)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	if err := config.Save(&config.Config{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Fatalf("no se escribió config.json: %v", err)
	}
}

func TestFindTenantIsCaseInsensitive(t *testing.T) {
	cfg := &config.Config{Tenants: []config.Tenant{{Name: "acme"}}}
	for _, q := range []string{"acme", "ACME", "AcMe"} {
		if got := cfg.FindTenant(q); got == nil {
			t.Errorf("FindTenant(%q) = nil, quería el tenant", q)
		}
	}
	if got := cfg.FindTenant("globex"); got != nil {
		t.Errorf("FindTenant(\"globex\") = %+v, quería nil", got)
	}
}

// FindTenant devuelve un puntero al elemento del slice, no una copia: quien
// lo recibe puede mutar la configuración.
func TestFindTenantReturnsPointerIntoSlice(t *testing.T) {
	cfg := &config.Config{Tenants: []config.Tenant{{Name: "acme", TenantID: "old"}}}
	cfg.FindTenant("acme").TenantID = "new"
	if cfg.Tenants[0].TenantID != "new" {
		t.Errorf("TenantID = %q, quería «new»", cfg.Tenants[0].TenantID)
	}
}

func TestAddTenantRejectsDuplicates(t *testing.T) {
	sandbox(t)
	cfg := &config.Config{}
	if err := cfg.AddTenant(config.Tenant{Name: "acme"}); err != nil {
		t.Fatalf("AddTenant: %v", err)
	}
	// El duplicado se detecta sin distinguir mayúsculas, igual que FindTenant.
	if err := cfg.AddTenant(config.Tenant{Name: "ACME"}); err == nil {
		t.Fatal("AddTenant aceptó un duplicado")
	}
	if len(cfg.Tenants) != 1 {
		t.Errorf("%d tenants, quería 1", len(cfg.Tenants))
	}
}

func TestAddTenantPersists(t *testing.T) {
	sandbox(t)
	cfg := &config.Config{}
	if err := cfg.AddTenant(config.Tenant{Name: "acme", TenantID: "x"}); err != nil {
		t.Fatalf("AddTenant: %v", err)
	}
	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.FindTenant("acme") == nil {
		t.Error("el tenant no se persistió")
	}
}

func TestRemoveTenant(t *testing.T) {
	sandbox(t)
	cfg := &config.Config{Tenants: []config.Tenant{{Name: "acme"}, {Name: "globex"}}}

	if err := cfg.RemoveTenant("ACME"); err != nil {
		t.Fatalf("RemoveTenant: %v", err)
	}
	if cfg.FindTenant("acme") != nil {
		t.Error("acme sigue presente")
	}
	if cfg.FindTenant("globex") == nil {
		t.Error("RemoveTenant se llevó por delante a globex")
	}

	if err := cfg.RemoveTenant("globex"); err != nil {
		t.Fatalf("RemoveTenant: %v", err)
	}
	if len(cfg.Tenants) != 0 {
		t.Errorf("%d tenants, quería 0", len(cfg.Tenants))
	}

	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reloaded.Tenants) != 0 {
		t.Errorf("en disco quedan %d tenants, quería 0", len(reloaded.Tenants))
	}
}

func TestRemoveTenantNotFound(t *testing.T) {
	sandbox(t)
	cfg := &config.Config{}
	if err := cfg.RemoveTenant("fantasma"); err == nil {
		t.Fatal("RemoveTenant devolvió nil sobre un tenant inexistente")
	}
}

func TestWriteEnv(t *testing.T) {
	dir := sandbox(t)
	const lines = "export AZURE_CONFIG_DIR=/tmp/acme\nexport AZURE_EXTENSION_DIR=/tmp/ext\n"
	if err := config.WriteEnv(lines); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}

	path := filepath.Join(dir, ".switch")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("leyendo .switch: %v", err)
	}
	if string(data) != lines {
		t.Errorf("contenido = %q, quería %q", data, lines)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// Documenta el comportamiento actual; #4 valora bajarlo a 0600.
	if perm := fi.Mode().Perm(); perm != 0644 {
		t.Errorf("permisos = %04o, quería 0644", perm)
	}
}

func TestWriteEnvCreatesBaseDir(t *testing.T) {
	dir := sandbox(t)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	if err := config.WriteEnv("export FOO=bar\n"); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".switch")); err != nil {
		t.Fatalf("no se escribió .switch: %v", err)
	}
}

// El formato en disco es API: la función de shell y cualquier edición manual
// dependen de estas claves.
func TestConfigJSONShape(t *testing.T) {
	sandbox(t)
	if err := config.Save(&config.Config{Tenants: []config.Tenant{
		{Name: "acme", TenantID: "tid", ConfigDir: "/tmp/acme"},
	}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	path, _ := config.ConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("leyendo: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tenants, ok := raw["tenants"].([]any)
	if !ok || len(tenants) != 1 {
		t.Fatalf("clave «tenants» = %v", raw["tenants"])
	}
	first := tenants[0].(map[string]any)
	for _, key := range []string{"name", "tenantId", "configDir"} {
		if _, ok := first[key]; !ok {
			t.Errorf("falta la clave %q en el JSON", key)
		}
	}
}

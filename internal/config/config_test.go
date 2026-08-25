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
		t.Errorf("BaseDir = %q, wanted %q", got, dir)
	}
}

func TestBaseDirFallsBackToHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("azsel is only published for linux and darwin")
	}
	home := t.TempDir()
	t.Setenv(config.EnvHome, "")
	t.Setenv("HOME", home)

	got, err := config.BaseDir()
	if err != nil {
		t.Fatalf("BaseDir: %v", err)
	}
	if want := filepath.Join(home, ".azsel"); got != want {
		t.Errorf("BaseDir = %q, wanted %q", got, want)
	}
}

// Asking for a path must have no effect on disk. It used to: every path
// function called MkdirAll.
func TestPathFunctionsCreateNothing(t *testing.T) {
	dir := sandbox(t)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("setup: %v", err)
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
			t.Fatalf("%s created %s", fn.name, dir)
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
			t.Errorf("%s = %q, wanted %q", c.name, got, c.want)
		}
	}
}

func TestEnsureBaseDirCreates(t *testing.T) {
	dir := sandbox(t)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := config.EnsureBaseDir(); err != nil {
		t.Fatalf("EnsureBaseDir: %v", err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("%s was not created: %v", dir, err)
	}
}

// The 'created' bool is what will let #9 revert a failed login without
// deleting a preexisting directory with valid credentials.
func TestEnsureTenantDirReportsWhoCreatedIt(t *testing.T) {
	sandbox(t)

	dir, created, err := config.EnsureTenantDir("acme")
	if err != nil {
		t.Fatalf("EnsureTenantDir: %v", err)
	}
	if !created {
		t.Error("created = false on the first call, wanted true")
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("%s was not created: %v", dir, err)
	}

	_, created, err = config.EnsureTenantDir("acme")
	if err != nil {
		t.Fatalf("EnsureTenantDir (second): %v", err)
	}
	if created {
		t.Error("created = true on a preexisting directory, wanted false")
	}
}

func TestEnsureExtensionsDirCreates(t *testing.T) {
	dir := sandbox(t)
	got, err := config.EnsureExtensionsDir()
	if err != nil {
		t.Fatalf("EnsureExtensionsDir: %v", err)
	}
	if want := filepath.Join(dir, "extensions"); got != want {
		t.Errorf("= %q, wanted %q", got, want)
	}
	if fi, err := os.Stat(got); err != nil || !fi.IsDir() {
		t.Fatalf("%s was not created: %v", got, err)
	}
}

func TestLoadMissingFileReturnsEmptyConfig(t *testing.T) {
	sandbox(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load on a nonexistent file returned an error: %v", err)
	}
	if len(cfg.Tenants) != 0 {
		t.Errorf("Tenants = %v, wanted empty", cfg.Tenants)
	}
}

func TestLoadCorruptJSON(t *testing.T) {
	dir := sandbox(t)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{no json"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := config.Load()
	if err == nil {
		t.Fatal("Load returned nil on corrupt JSON")
	}
	if !strings.Contains(err.Error(), "parsing config") {
		t.Errorf("error = %q, wanted it to mention 'parsing config'", err)
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
		t.Fatalf("%d tenants, wanted %d", len(got.Tenants), len(want.Tenants))
	}
	for i := range want.Tenants {
		if got.Tenants[i] != want.Tenants[i] {
			t.Errorf("tenant %d = %+v, wanted %+v", i, got.Tenants[i], want.Tenants[i])
		}
	}
}

// Save must create the base directory if it does not exist: it is the first
// contact with disk on a fresh install.
func TestSaveCreatesBaseDir(t *testing.T) {
	dir := sandbox(t)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := config.Save(&config.Config{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Fatalf("config.json was not written: %v", err)
	}
}

func TestFindTenantIsCaseInsensitive(t *testing.T) {
	cfg := &config.Config{Tenants: []config.Tenant{{Name: "acme"}}}
	for _, q := range []string{"acme", "ACME", "AcMe"} {
		if got := cfg.FindTenant(q); got == nil {
			t.Errorf("FindTenant(%q) = nil, wanted the tenant", q)
		}
	}
	if got := cfg.FindTenant("globex"); got != nil {
		t.Errorf("FindTenant(\"globex\") = %+v, wanted nil", got)
	}
}

// FindTenant returns a pointer into the slice, not a copy: the caller can
// mutate the config.
func TestFindTenantReturnsPointerIntoSlice(t *testing.T) {
	cfg := &config.Config{Tenants: []config.Tenant{{Name: "acme", TenantID: "old"}}}
	cfg.FindTenant("acme").TenantID = "new"
	if cfg.Tenants[0].TenantID != "new" {
		t.Errorf("TenantID = %q, wanted 'new'", cfg.Tenants[0].TenantID)
	}
}

func TestAddTenantRejectsDuplicates(t *testing.T) {
	sandbox(t)
	cfg := &config.Config{}
	if err := cfg.AddTenant(config.Tenant{Name: "acme"}); err != nil {
		t.Fatalf("AddTenant: %v", err)
	}
	// The duplicate is detected case-insensitively, just like FindTenant.
	if err := cfg.AddTenant(config.Tenant{Name: "ACME"}); err == nil {
		t.Fatal("AddTenant accepted a duplicate")
	}
	if len(cfg.Tenants) != 1 {
		t.Errorf("%d tenants, wanted 1", len(cfg.Tenants))
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
		t.Error("the tenant was not persisted")
	}
}

func TestRemoveTenant(t *testing.T) {
	sandbox(t)
	cfg := &config.Config{Tenants: []config.Tenant{{Name: "acme"}, {Name: "globex"}}}

	if err := cfg.RemoveTenant("ACME"); err != nil {
		t.Fatalf("RemoveTenant: %v", err)
	}
	if cfg.FindTenant("acme") != nil {
		t.Error("acme is still present")
	}
	if cfg.FindTenant("globex") == nil {
		t.Error("RemoveTenant took globex down with it")
	}

	if err := cfg.RemoveTenant("globex"); err != nil {
		t.Fatalf("RemoveTenant: %v", err)
	}
	if len(cfg.Tenants) != 0 {
		t.Errorf("%d tenants, wanted 0", len(cfg.Tenants))
	}

	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reloaded.Tenants) != 0 {
		t.Errorf("%d tenants left on disk, wanted 0", len(reloaded.Tenants))
	}
}

func TestRemoveTenantNotFound(t *testing.T) {
	sandbox(t)
	cfg := &config.Config{}
	if err := cfg.RemoveTenant("ghost"); err == nil {
		t.Fatal("RemoveTenant returned nil on a nonexistent tenant")
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
		t.Fatalf("reading .switch: %v", err)
	}
	if string(data) != lines {
		t.Errorf("content = %q, wanted %q", data, lines)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// 0600 since #4: the shell sources this file, so its contents run with the
	// user's permissions.
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Errorf("perms = %04o, wanted 0600", perm)
	}
}

func TestWriteEnvCreatesBaseDir(t *testing.T) {
	dir := sandbox(t)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := config.WriteEnv("export FOO=bar\n"); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".switch")); err != nil {
		t.Fatalf(".switch was not written: %v", err)
	}
}

// The on-disk format is API: the shell function and any manual editing depend
// on these keys.
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
		t.Fatalf("reading: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tenants, ok := raw["tenants"].([]any)
	if !ok || len(tenants) != 1 {
		t.Fatalf("'tenants' key = %v", raw["tenants"])
	}
	first := tenants[0].(map[string]any)
	for _, key := range []string{"name", "tenantId", "configDir"} {
		if _, ok := first[key]; !ok {
			t.Errorf("missing key %q in the JSON", key)
		}
	}
}

package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

// unusableHome apunta AZSEL_HOME a una ruta que cuelga de un fichero. Todo
// intento de crear directorios ahí falla con ENOTDIR. Es más fiable que jugar
// con permisos: root los ignoraría.
func unusableHome(t *testing.T) {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "soy-un-fichero")
	if err := os.WriteFile(blocker, nil, 0644); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	t.Setenv(config.EnvHome, filepath.Join(blocker, "azsel"))
}

func TestBaseDirFailsWithoutHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("azsel solo se publica para linux y darwin")
	}
	t.Setenv(config.EnvHome, "")
	t.Setenv("HOME", "")

	if _, err := config.BaseDir(); err == nil {
		t.Fatal("BaseDir devolvió nil sin HOME definido")
	} else if !strings.Contains(err.Error(), "home directory") {
		t.Errorf("error = %q, quería que mencionara el home", err)
	}
}

func TestEnsureFuncsFailOnUnusableBaseDir(t *testing.T) {
	cases := []struct {
		name string
		call func() error
		want string
	}{
		{"EnsureBaseDir", func() error {
			_, err := config.EnsureBaseDir()
			return err
		}, "base directory"},
		{"EnsureExtensionsDir", func() error {
			_, err := config.EnsureExtensionsDir()
			return err
		}, "extensions directory"},
		{"EnsureTenantDir", func() error {
			_, _, err := config.EnsureTenantDir("acme")
			return err
		}, "tenant directory"},
		{"Save", func() error { return config.Save(&config.Config{}) }, ""},
		{"WriteEnv", func() error { return config.WriteEnv("export FOO=bar\n") }, ""},
		{"AddTenant", func() error {
			return (&config.Config{}).AddTenant(config.Tenant{Name: "acme"})
		}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			unusableHome(t)
			err := c.call()
			if err == nil {
				t.Fatalf("%s devolvió nil sobre un directorio base inservible", c.name)
			}
			if c.want != "" && !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, quería que mencionara %q", err, c.want)
			}
		})
	}
}

// Un config.json que resulta ser un directorio no es «no existe»: es un error
// de lectura real y Load debe propagarlo en vez de devolver config vacía.
func TestLoadFailsWhenConfigPathIsADirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvHome, dir)
	if err := os.MkdirAll(filepath.Join(dir, "config.json"), 0755); err != nil {
		t.Fatalf("preparando: %v", err)
	}

	if _, err := config.Load(); err == nil {
		t.Fatal("Load devolvió nil con config.json siendo un directorio")
	} else if !strings.Contains(err.Error(), "reading config") {
		t.Errorf("error = %q, quería que mencionara «reading config»", err)
	}
}

// Documenta una arista del comportamiento actual: si la ruta del tenant ya
// está ocupada por un *fichero*, ensureDir ve que existe y devuelve
// created=false sin error. El fallo aparecería más tarde, al intentar az
// login dentro. Es un caso muy improbable y no se aborda aquí; el test
// existe para que el cambio no pase inadvertido si alguien lo modifica.
func TestEnsureTenantDirWhenPathIsTakenByAFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvHome, dir)
	tenants := filepath.Join(dir, "tenants")
	if err := os.MkdirAll(tenants, 0755); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	// acme existe pero es un fichero; pedir un directorio debajo debe fallar.
	if err := os.WriteFile(filepath.Join(tenants, "acme"), nil, 0644); err != nil {
		t.Fatalf("preparando: %v", err)
	}

	got, created, err := config.EnsureTenantDir("acme")
	if err != nil {
		t.Fatalf("EnsureTenantDir sobre un fichero existente: %v", err)
	}
	if created {
		t.Error("created = true, quería false: la ruta ya estaba ocupada")
	}
	// Lo importante: no se reporta como creado, así que un rollback futuro
	// (#9) no borrará algo que no puso él.
	if got != filepath.Join(tenants, "acme") {
		t.Errorf("dir = %q", got)
	}
}

func TestWriteEnvWithDebugEnabled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvHome, dir)
	t.Setenv("AZSEL_DEBUG", "1")

	if err := config.WriteEnv("export FOO=bar\n"); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".switch")); err != nil {
		t.Fatalf("no se escribió .switch: %v", err)
	}
}

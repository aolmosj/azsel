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

// Una ruta ocupada por un fichero no es un perfil existente. os.Stat tiene
// éxito sobre un fichero, así que sin comprobar IsDir se colaría como
// «ya existe» y el fallo aparecería mucho después, dentro de az.
func TestEnsureTenantDirRejectsPathTakenByAFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvHome, dir)
	tenants := filepath.Join(dir, "tenants")
	if err := os.MkdirAll(tenants, 0755); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tenants, "acme"), nil, 0644); err != nil {
		t.Fatalf("preparando: %v", err)
	}

	_, created, err := config.EnsureTenantDir("acme")
	if err == nil {
		t.Fatal("EnsureTenantDir devolvió nil sobre una ruta ocupada por un fichero")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error = %q, quería que mencionara «not a directory»", err)
	}
	// Nada creado: un rollback futuro (#9) no debe borrar lo que no puso él.
	if created {
		t.Error("created = true, quería false")
	}
}

// Lo mismo para el directorio base y el de extensiones.
func TestEnsureDirsRejectPathTakenByAFile(t *testing.T) {
	t.Run("base", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "azsel")
		if err := os.WriteFile(blocker, nil, 0644); err != nil {
			t.Fatalf("preparando: %v", err)
		}
		t.Setenv(config.EnvHome, blocker)
		if _, err := config.EnsureBaseDir(); err == nil {
			t.Fatal("EnsureBaseDir devolvió nil sobre un fichero")
		}
	})

	t.Run("extensions", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv(config.EnvHome, dir)
		if err := os.WriteFile(filepath.Join(dir, "extensions"), nil, 0644); err != nil {
			t.Fatalf("preparando: %v", err)
		}
		if _, err := config.EnsureExtensionsDir(); err == nil {
			t.Fatal("EnsureExtensionsDir devolvió nil sobre un fichero")
		}
	})
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

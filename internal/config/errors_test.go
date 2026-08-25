package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

// unusableHome points AZSEL_HOME at a path hanging off a file. Any attempt to
// create directories there fails with ENOTDIR. It is more reliable than
// playing with permissions: root would ignore them.
func unusableHome(t *testing.T) {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(blocker, nil, 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Setenv(config.EnvHome, filepath.Join(blocker, "azsel"))
}

func TestBaseDirFailsWithoutHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("azsel is only published for linux and darwin")
	}
	t.Setenv(config.EnvHome, "")
	t.Setenv("HOME", "")

	if _, err := config.BaseDir(); err == nil {
		t.Fatal("BaseDir returned nil with no HOME defined")
	} else if !strings.Contains(err.Error(), "home directory") {
		t.Errorf("error = %q, wanted it to mention the home", err)
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
				t.Fatalf("%s returned nil on an unusable base directory", c.name)
			}
			if c.want != "" && !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, wanted it to mention %q", err, c.want)
			}
		})
	}
}

// A config.json that turns out to be a directory is not "does not exist": it
// is a real read error and Load must propagate it instead of returning an
// empty config.
func TestLoadFailsWhenConfigPathIsADirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvHome, dir)
	if err := os.MkdirAll(filepath.Join(dir, "config.json"), 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if _, err := config.Load(); err == nil {
		t.Fatal("Load returned nil with config.json being a directory")
	} else if !strings.Contains(err.Error(), "reading config") {
		t.Errorf("error = %q, wanted it to mention 'reading config'", err)
	}
}

// A path taken by a file is not an existing profile. os.Stat succeeds on a
// file, so without checking IsDir it would slip through as "already exists"
// and the failure would surface much later, inside az.
func TestEnsureTenantDirRejectsPathTakenByAFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvHome, dir)
	tenants := filepath.Join(dir, "tenants")
	if err := os.MkdirAll(tenants, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tenants, "acme"), nil, 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, created, err := config.EnsureTenantDir("acme")
	if err == nil {
		t.Fatal("EnsureTenantDir returned nil on a path taken by a file")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error = %q, wanted it to mention 'not a directory'", err)
	}
	// Nothing created: a future rollback (#9) must not delete what it did not
	// put there.
	if created {
		t.Error("created = true, wanted false")
	}
}

// The same for the base directory and the extensions one.
func TestEnsureDirsRejectPathTakenByAFile(t *testing.T) {
	t.Run("base", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "azsel")
		if err := os.WriteFile(blocker, nil, 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		t.Setenv(config.EnvHome, blocker)
		if _, err := config.EnsureBaseDir(); err == nil {
			t.Fatal("EnsureBaseDir returned nil on a file")
		}
	})

	t.Run("extensions", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv(config.EnvHome, dir)
		if err := os.WriteFile(filepath.Join(dir, "extensions"), nil, 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if _, err := config.EnsureExtensionsDir(); err == nil {
			t.Fatal("EnsureExtensionsDir returned nil on a file")
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
		t.Fatalf(".switch was not written: %v", err)
	}
}

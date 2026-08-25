package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aolmosj/azsel/internal/config"
)

func TestEnvFileHonoursSwitchFileVar(t *testing.T) {
	sandbox(t)
	want := filepath.Join(t.TempDir(), ".switch.4242")
	t.Setenv(config.EnvSwitchFile, want)

	got, err := config.EnvFile()
	if err != nil {
		t.Fatalf("EnvFile: %v", err)
	}
	if got != want {
		t.Errorf("EnvFile = %q, wanted %q", got, want)
	}
}

// The fallback keeps working for wrappers installed before #4 and for the
// scripting pattern documented in the README.
func TestEnvFileFallsBackToLegacyPath(t *testing.T) {
	dir := sandbox(t)
	t.Setenv(config.EnvSwitchFile, "")

	got, err := config.EnvFile()
	if err != nil {
		t.Fatalf("EnvFile: %v", err)
	}
	if want := filepath.Join(dir, ".switch"); got != want {
		t.Errorf("EnvFile = %q, wanted %q", got, want)
	}
}

func TestWriteEnvUsesSwitchFileVar(t *testing.T) {
	base := sandbox(t)
	target := filepath.Join(t.TempDir(), "sub", ".switch.99")
	t.Setenv(config.EnvSwitchFile, target)

	const lines = "export AZURE_CONFIG_DIR=/fake/acme\n"
	if err := config.WriteEnv(lines); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading the target: %v", err)
	}
	if string(data) != lines {
		t.Errorf("content = %q, wanted %q", data, lines)
	}
	// It must not also write to the old path.
	if _, err := os.Stat(filepath.Join(base, ".switch")); !os.IsNotExist(err) {
		t.Error("WriteEnv also wrote to the legacy .switch")
	}
}

// The scenario behind #4: two shells switching tenant at once. They used to
// share a file, so one consumed the other's switch.
func TestConcurrentShellsDoNotShareSwitchFile(t *testing.T) {
	base := sandbox(t)

	write := func(pid, tenant string) string {
		path := filepath.Join(base, ".switch."+pid)
		t.Setenv(config.EnvSwitchFile, path)
		if err := config.WriteEnv("export AZURE_CONFIG_DIR=/fake/" + tenant + "\n"); err != nil {
			t.Fatalf("WriteEnv(%s): %v", pid, err)
		}
		return path
	}

	// Shell A writes its switch and has not sourced it yet.
	pathA := write("1001", "acme")
	// Shell B runs azsel in the meantime.
	pathB := write("1002", "globex")

	for _, c := range []struct{ path, want string }{
		{pathA, "/fake/acme"},
		{pathB, "/fake/globex"},
	} {
		data, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatalf("reading %s: %v", c.path, err)
		}
		if got := string(data); got != "export AZURE_CONFIG_DIR="+c.want+"\n" {
			t.Errorf("%s contains %q, wanted %s", c.path, got, c.want)
		}
	}
}

func TestWriteEnvPrunesStaleSwitchFiles(t *testing.T) {
	base := sandbox(t)
	if _, err := config.EnsureBaseDir(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	stale := filepath.Join(base, ".switch.1234")
	fresh := filepath.Join(base, ".switch.5678")
	legacy := filepath.Join(base, ".switch")
	for _, p := range []string{stale, fresh, legacy} {
		if err := os.WriteFile(p, []byte("export FOO=bar\n"), 0600); err != nil {
			t.Fatalf("setup %s: %v", p, err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	for _, p := range []string{stale, legacy} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatalf("aging %s: %v", p, err)
		}
	}

	t.Setenv(config.EnvSwitchFile, filepath.Join(base, ".switch.9999"))
	if err := config.WriteEnv("export AZURE_CONFIG_DIR=/fake/acme\n"); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("the orphan switch file was not pruned")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a recent switch file was pruned")
	}
	// The legacy .switch is respected: an old wrapper may be about to source
	// it.
	if _, err := os.Stat(legacy); err != nil {
		t.Error("the legacy .switch was pruned")
	}
}

// Pruning is best-effort: if it fails, the tenant switch must not fail.
func TestWriteEnvSucceedsWhenPruneCannotRead(t *testing.T) {
	target := filepath.Join(t.TempDir(), ".switch.1")
	t.Setenv(config.EnvHome, filepath.Join(t.TempDir(), "no", "existe"))
	t.Setenv(config.EnvSwitchFile, target)

	if err := config.WriteEnv("export AZURE_CONFIG_DIR=/fake/acme\n"); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("the switch was not written: %v", err)
	}
}

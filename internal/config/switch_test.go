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
		t.Errorf("EnvFile = %q, quería %q", got, want)
	}
}

// El fallback mantiene funcionando a los wrappers instalados antes de #4 y al
// patrón de scripts documentado en el README.
func TestEnvFileFallsBackToLegacyPath(t *testing.T) {
	dir := sandbox(t)
	t.Setenv(config.EnvSwitchFile, "")

	got, err := config.EnvFile()
	if err != nil {
		t.Fatalf("EnvFile: %v", err)
	}
	if want := filepath.Join(dir, ".switch"); got != want {
		t.Errorf("EnvFile = %q, quería %q", got, want)
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
		t.Fatalf("leyendo el destino: %v", err)
	}
	if string(data) != lines {
		t.Errorf("contenido = %q, quería %q", data, lines)
	}
	// No debe escribir además en la ruta antigua.
	if _, err := os.Stat(filepath.Join(base, ".switch")); !os.IsNotExist(err) {
		t.Error("WriteEnv escribió también en el .switch heredado")
	}
}

// El escenario que motiva #4: dos shells cambiando de tenant a la vez. Antes
// compartían fichero, así que una consumía el switch de la otra.
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

	// La shell A escribe su switch y todavía no lo ha sourceado.
	pathA := write("1001", "acme")
	// La shell B ejecuta azsel entretanto.
	pathB := write("1002", "globex")

	for _, c := range []struct{ path, want string }{
		{pathA, "/fake/acme"},
		{pathB, "/fake/globex"},
	} {
		data, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatalf("leyendo %s: %v", c.path, err)
		}
		if got := string(data); got != "export AZURE_CONFIG_DIR="+c.want+"\n" {
			t.Errorf("%s contiene %q, quería %s", c.path, got, c.want)
		}
	}
}

func TestWriteEnvPrunesStaleSwitchFiles(t *testing.T) {
	base := sandbox(t)
	if _, err := config.EnsureBaseDir(); err != nil {
		t.Fatalf("preparando: %v", err)
	}

	stale := filepath.Join(base, ".switch.1234")
	fresh := filepath.Join(base, ".switch.5678")
	legacy := filepath.Join(base, ".switch")
	for _, p := range []string{stale, fresh, legacy} {
		if err := os.WriteFile(p, []byte("export FOO=bar\n"), 0600); err != nil {
			t.Fatalf("preparando %s: %v", p, err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	for _, p := range []string{stale, legacy} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatalf("envejeciendo %s: %v", p, err)
		}
	}

	t.Setenv(config.EnvSwitchFile, filepath.Join(base, ".switch.9999"))
	if err := config.WriteEnv("export AZURE_CONFIG_DIR=/fake/acme\n"); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("no se podó el fichero de switch huérfano")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("se podó un fichero de switch reciente")
	}
	// El .switch heredado se respeta: un wrapper antiguo puede estar a punto
	// de sourcearlo.
	if _, err := os.Stat(legacy); err != nil {
		t.Error("se podó el .switch heredado")
	}
}

// La poda es best-effort: si falla, el cambio de tenant no debe fallar.
func TestWriteEnvSucceedsWhenPruneCannotRead(t *testing.T) {
	target := filepath.Join(t.TempDir(), ".switch.1")
	t.Setenv(config.EnvHome, filepath.Join(t.TempDir(), "no", "existe"))
	t.Setenv(config.EnvSwitchFile, target)

	if err := config.WriteEnv("export AZURE_CONFIG_DIR=/fake/acme\n"); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("no se escribió el switch: %v", err)
	}
}

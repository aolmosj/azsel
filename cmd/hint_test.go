package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

// El mensaje sugería `eval $(azsel use NAME)`, que no hace nada: use escribe
// el snippet en un fichero y reporta por stderr, así que la sustitución de
// comandos captura la cadena vacía y `eval ""` es un no-op.
func TestActivationHintNeverSuggestsEval(t *testing.T) {
	for _, active := range []bool{true, false} {
		got := activationHint("acme", active)
		if strings.Contains(got, "eval") {
			t.Errorf("integración=%v: el mensaje sigue sugiriendo eval:\n%s", active, got)
		}
		if !strings.Contains(got, "azsel use acme") {
			t.Errorf("integración=%v: falta la orden real:\n%s", active, got)
		}
	}
}

// Con la integración activa el mensaje debe ser una sola instrucción, sin
// ruido: el usuario ya lo tiene todo montado.
func TestActivationHintIsTerseWhenIntegrationActive(t *testing.T) {
	got := activationHint("acme", true)
	if want := "To activate: azsel use acme\n"; got != want {
		t.Errorf("mensaje = %q, quería %q", got, want)
	}
}

// Sin integración, `azsel use` no puede alcanzar el shell del usuario. Hay que
// decirlo, no dejar que lo descubra por su cuenta.
func TestActivationHintWarnsWithoutIntegration(t *testing.T) {
	got := activationHint("acme", false)
	if !strings.Contains(got, "azsel init") {
		t.Errorf("el aviso no menciona 'azsel init':\n%s", got)
	}
	if !strings.Contains(got, "AZURE_CONFIG_DIR") {
		t.Errorf("el aviso no explica la consecuencia:\n%s", got)
	}
}

// El nombre se interpola, no se pierde.
func TestActivationHintUsesTenantName(t *testing.T) {
	if got := activationHint("globex-prod", true); !strings.Contains(got, "globex-prod") {
		t.Errorf("mensaje = %q, quería el nombre del tenant", got)
	}
}

// `command azsel add` esquiva el wrapper a propósito, igual que cualquier
// script. Mirar solo AZSEL_SWITCH_FILE le decía a un usuario correctamente
// configurado que ejecutara `azsel init`, que respondería "already
// configured" y lo dejaría sin salida.
func TestShellIntegrationInstalled(t *testing.T) {
	t.Run("variable del wrapper presente", func(t *testing.T) {
		t.Setenv(config.EnvSwitchFile, "/tmp/.switch.1")
		if !shellIntegrationInstalled() {
			t.Error("no se detectó la integración con la variable definida")
		}
	})

	t.Run("wrapper esquivado pero rc configurado", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("SHELL", "/bin/zsh")
		t.Setenv(config.EnvSwitchFile, "")
		if err := os.WriteFile(filepath.Join(home, ".zshrc"),
			[]byte("\n"+initLine+"\n"), 0644); err != nil {
			t.Fatalf("preparando: %v", err)
		}
		if !shellIntegrationInstalled() {
			t.Error("no se detectó la integración leyendo el rc")
		}
	})

	t.Run("sin integración", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("SHELL", "/bin/zsh")
		t.Setenv(config.EnvSwitchFile, "")
		if shellIntegrationInstalled() {
			t.Error("se detectó integración donde no la hay")
		}
	})

	t.Run("shell no soportado", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("SHELL", "/usr/bin/fish")
		t.Setenv(config.EnvSwitchFile, "")
		if shellIntegrationInstalled() {
			t.Error("se detectó integración en un shell no soportado")
		}
	})
}

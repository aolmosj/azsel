package cmd

import (
	"strings"
	"testing"
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

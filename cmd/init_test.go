package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// shells devuelve los intérpretes en los que azsel instala su función
// (detectShellRC contempla zsh y bash), saltando los que no estén presentes.
// La función se instala en ambos, así que debe funcionar en ambos: #3 nació
// justo de una construcción exclusiva de zsh metida en un fichero que también
// acaba en .bashrc.
func shells(t *testing.T) []string {
	t.Helper()
	var found []string
	for _, name := range []string{"bash", "zsh"} {
		if path, err := exec.LookPath(name); err == nil {
			found = append(found, path)
		}
	}
	if len(found) == 0 {
		t.Skip("ni bash ni zsh disponibles")
	}
	return found
}

// runShellFunc evalúa la función de shell instalada por `azsel init --print`
// en un intérprete real, con un azsel de mentira en el PATH que escribe lo
// que se le indique en $AZSEL_SWITCH_FILE. Devuelve la salida del script.
//
// Es el único test que ejercita el mecanismo central de azsel de punta a
// punta: el binario no puede tocar el entorno del shell padre, así que todo
// depende de que esta función haga el source correctamente.
func runShellFunc(t *testing.T, shell, home, stubBody, script string, extraEnv ...string) string {
	t.Helper()

	binDir := t.TempDir()
	stub := filepath.Join(binDir, "azsel")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n"+stubBody), 0755); err != nil {
		t.Fatalf("escribiendo el stub: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".azsel"), 0755); err != nil {
		t.Fatalf("preparando el home: %v", err)
	}

	cmd := exec.Command(shell, "-c", shellFunc+"\n"+script)
	cmd.Env = append([]string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"HOME=" + home,
	}, extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s falló: %v\nsalida:\n%s", filepath.Base(shell), err, out)
	}
	return string(out)
}

// eachShell ejecuta fn una vez por intérprete disponible, como subtest.
func eachShell(t *testing.T, fn func(t *testing.T, shell string)) {
	t.Helper()
	for _, shell := range shells(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) { fn(t, shell) })
	}
}

// El caso base: azsel escribe el snippet, la función lo sourcea y la variable
// queda puesta en el shell.
func TestShellFuncSourcesSwitchFile(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		stub := `printf 'export AZURE_CONFIG_DIR=%s\n' /fake/tenants/acme > "$AZSEL_SWITCH_FILE"` + "\n"
		out := runShellFunc(t, shell, home, stub, `azsel use acme; echo "RESULT=$AZURE_CONFIG_DIR"`)

		if !strings.Contains(out, "RESULT=/fake/tenants/acme") {
			t.Errorf("la variable no llegó al shell.\nsalida:\n%s", out)
		}
	})
}

// Tras sourcear, el fichero debe desaparecer: si sobreviviera, la siguiente
// invocación lo volvería a aplicar.
func TestShellFuncRemovesSwitchFileAfterSourcing(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		stub := `printf 'export AZURE_CONFIG_DIR=%s\n' /fake/acme > "$AZSEL_SWITCH_FILE"` + "\n"
		out := runShellFunc(t, shell, home, stub,
			`azsel use acme; ls "$HOME/.azsel/" | grep -c switch || echo "SIN_RESTOS"`)

		if !strings.Contains(out, "SIN_RESTOS") {
			t.Errorf("quedaron ficheros de switch.\nsalida:\n%s", out)
		}
	})
}

// El corazón de #4: cada shell usa su propia ruta, derivada de su PID. Sin
// esto, una terminal consumía y borraba el switch pendiente de otra.
func TestShellFuncUsesPerShellSwitchFile(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		// El stub delata la ruta que le han pasado en vez de escribir nada.
		stub := `echo "PATH_USADO=$AZSEL_SWITCH_FILE"` + "\n"

		first := runShellFunc(t, shell, home, stub, `azsel list`)
		second := runShellFunc(t, shell, home, stub, `azsel list`)

		extract := func(out string) string {
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, "PATH_USADO=") {
					return strings.TrimPrefix(line, "PATH_USADO=")
				}
			}
			t.Fatalf("el stub no reportó ruta.\nsalida:\n%s", out)
			return ""
		}
		a, b := extract(first), extract(second)

		if a == b {
			t.Errorf("dos shells usaron la misma ruta (%s); deben diferir por PID", a)
		}
		for _, got := range []string{a, b} {
			if !strings.HasPrefix(got, filepath.Join(home, ".azsel", ".switch.")) {
				t.Errorf("ruta = %q, quería ~/.azsel/.switch.<pid>", got)
			}
		}
	})
}

// AZSEL_SWITCH_FILE es para el subproceso, no para la sesión: no debe quedar
// colgando en el entorno del usuario.
func TestShellFuncDoesNotLeakSwitchVar(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		out := runShellFunc(t, shell, home, "true\n",
			`azsel list; echo "FUGA=[${AZSEL_SWITCH_FILE:-}]"`)

		if !strings.Contains(out, "FUGA=[]") {
			t.Errorf("AZSEL_SWITCH_FILE quedó en el entorno.\nsalida:\n%s", out)
		}
	})
}

// La función respeta AZSEL_HOME, igual que el binario.
func TestShellFuncHonoursAzselHome(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		alt := t.TempDir()
		stub := `echo "PATH_USADO=$AZSEL_SWITCH_FILE"` + "\n"
		out := runShellFunc(t, shell, home, stub, `export AZSEL_HOME=`+alt+`; azsel list`)

		if !strings.Contains(out, "PATH_USADO="+filepath.Join(alt, ".switch.")) {
			t.Errorf("no se respetó AZSEL_HOME.\nsalida:\n%s", out)
		}
	})
}

// Sin fichero de switch, la función no debe romper ni ensuciar la salida.
func TestShellFuncWithoutSwitchFile(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		out := runShellFunc(t, shell, home, "true\n", `azsel list; echo "OK=$?"`)
		if !strings.Contains(out, "OK=0") {
			t.Errorf("la función falló sin fichero de switch.\nsalida:\n%s", out)
		}
	})
}

// #3: el modo depuración debe funcionar en los dos shells. La función usaba
// `whence -p`, un builtin exclusivo de zsh, y en bash imprimía
// «whence: command not found» dentro de la propia traza de depuración.
func TestShellFuncDebugIsPortable(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		stub := `printf 'export AZURE_CONFIG_DIR=%s\n' /fake/acme > "$AZSEL_SWITCH_FILE"` + "\n"
		out := runShellFunc(t, shell, home, stub, `azsel use acme`, "AZSEL_DEBUG=1")

		for _, bad := range []string{"command not found", "not found", "whence"} {
			if strings.Contains(out, bad) {
				t.Errorf("la traza de depuración contiene %q.\nsalida:\n%s", bad, out)
			}
		}
		// Y debe seguir siendo útil.
		for _, want := range []string{"args: use acme", "switch file:", "sourcing"} {
			if !strings.Contains(out, want) {
				t.Errorf("la traza no menciona %q.\nsalida:\n%s", want, out)
			}
		}
	})
}

// La función generada no debe contener construcciones exclusivas de zsh: se
// instala también en .bashrc.
func TestShellFuncHasNoZshOnlyBuiltins(t *testing.T) {
	for _, bad := range []string{"whence", "typeset", "setopt", "print -"} {
		if strings.Contains(shellFunc, bad) {
			t.Errorf("la función usa %q, que no existe en bash", bad)
		}
	}
}

// La línea que init escribe en el rc debe invocar --print; sin él, init
// modifica el fichero rc en vez de imprimir la función (ver #2).
func TestInitLineUsesPrint(t *testing.T) {
	if !strings.Contains(initLine, "--print") {
		t.Errorf("initLine = %q, quería que usara --print", initLine)
	}
}

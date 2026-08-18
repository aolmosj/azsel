package azure

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// capture holds the command handed to run, so a test can inspect what azsel
// built without anything being executed.
type capture struct{ cmd *exec.Cmd }

// stubRun replaces the executor for one test. fn may be nil to succeed.
func stubRun(t *testing.T, fn func(*exec.Cmd) error) *capture {
	t.Helper()
	c := &capture{}
	orig := run
	run = func(cmd *exec.Cmd) error {
		c.cmd = cmd
		if fn == nil {
			return nil
		}
		return fn(cmd)
	}
	t.Cleanup(func() { run = orig })
	return c
}

func stubLookPath(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	orig := lookPath
	lookPath = fn
	t.Cleanup(func() { lookPath = orig })
}

// envValue reports the value the subprocess will actually see. os/exec
// deduplicates Env keeping the *last* occurrence, so a key inherited from the
// parent and then overridden appears twice and the later one wins. Reading
// the first match instead is how this helper originally got it wrong: it
// passed on macOS, where nothing pre-sets these, and failed on a CI runner
// that ships with AZURE_EXTENSION_DIR already exported.
func envValue(cmd *exec.Cmd, key string) (string, bool) {
	value, found := "", false
	for _, kv := range cmd.Env {
		if name, v, ok := strings.Cut(kv, "="); ok && name == key {
			value, found = v, true
		}
	}
	return value, found
}

func TestLoginArguments(t *testing.T) {
	cases := []struct {
		name       string
		deviceCode bool
		want       []string
	}{
		{"navegador", false, []string{"az", "login", "--tenant", "TID"}},
		{"device code", true, []string{"az", "login", "--tenant", "TID", "--use-device-code"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stubRun(t, nil)
			if err := Login("TID", "/cfg", "/ext", c.deviceCode); err != nil {
				t.Fatalf("Login: %v", err)
			}
			if !slices.Equal(got.cmd.Args, c.want) {
				t.Errorf("args = %v, quería %v", got.cmd.Args, c.want)
			}
		})
	}
}

// El aislamiento entre tenants depende enteramente de estas dos variables.
func TestLoginScopesTheEnvironment(t *testing.T) {
	got := stubRun(t, nil)
	if err := Login("TID", "/cfg/acme", "/ext", false); err != nil {
		t.Fatalf("Login: %v", err)
	}

	for _, c := range []struct{ key, want string }{
		{"AZURE_CONFIG_DIR", "/cfg/acme"},
		{"AZURE_EXTENSION_DIR", "/ext"},
	} {
		if v, ok := envValue(got.cmd, c.key); !ok || v != c.want {
			t.Errorf("%s = %q (presente=%v), quería %q", c.key, v, ok, c.want)
		}
	}

	// El resto del entorno se hereda: az necesita proxy, locale, HOME...
	if _, ok := envValue(got.cmd, "PATH"); !ok {
		t.Error("no se heredó el entorno del proceso")
	}
}

// Dos comportamientos deliberados y fáciles de romper sin querer: el login es
// interactivo, así que stdin debe llegar a az; y la salida de az va a stderr
// porque azsel mantiene stdout limpio para el shell.
func TestLoginWiresStreams(t *testing.T) {
	got := stubRun(t, nil)
	if err := Login("TID", "/cfg", "/ext", false); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got.cmd.Stdin != os.Stdin {
		t.Error("stdin no está conectado: el login interactivo no funcionaría")
	}
	if got.cmd.Stdout != os.Stderr {
		t.Error("stdout de az no va a stderr: ensuciaría la salida estándar")
	}
	if got.cmd.Stderr != os.Stderr {
		t.Error("stderr de az no va a stderr")
	}
}

func TestLoginPropagatesFailure(t *testing.T) {
	want := errors.New("el usuario canceló")
	stubRun(t, func(*exec.Cmd) error { return want })

	err := Login("TID", "/cfg", "/ext", false)
	if !errors.Is(err, want) {
		t.Errorf("error = %v, quería %v", err, want)
	}
}

func TestAccountShowParsesJSON(t *testing.T) {
	const payload = `{
	  "tenantId": "11111111-1111-1111-1111-111111111111",
	  "name": "Contoso Production",
	  "user": {"name": "juan@contoso.com"}
	}`
	got := stubRun(t, func(cmd *exec.Cmd) error {
		_, err := cmd.Stdout.Write([]byte(payload))
		return err
	})

	info, err := AccountShow("/cfg", "/ext")
	if err != nil {
		t.Fatalf("AccountShow: %v", err)
	}
	if info.TenantID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("TenantID = %q", info.TenantID)
	}
	if info.Name != "Contoso Production" {
		t.Errorf("Name = %q", info.Name)
	}
	if info.User.Name != "juan@contoso.com" {
		t.Errorf("User.Name = %q", info.User.Name)
	}

	want := []string{"az", "account", "show", "--output", "json"}
	if !slices.Equal(got.cmd.Args, want) {
		t.Errorf("args = %v, quería %v", got.cmd.Args, want)
	}
}

func TestAccountShowRejectsInvalidJSON(t *testing.T) {
	stubRun(t, func(cmd *exec.Cmd) error {
		_, err := cmd.Stdout.Write([]byte("no soy json"))
		return err
	})

	_, err := AccountShow("/cfg", "/ext")
	if err == nil {
		t.Fatal("AccountShow devolvió nil sobre JSON inválido")
	}
	if !strings.Contains(err.Error(), "parsing account info") {
		t.Errorf("error = %q, quería que mencionara «parsing account info»", err)
	}
}

// az se explica por stderr; sin esto el usuario solo ve «exit status 1».
func TestAccountShowIncludesStderrInError(t *testing.T) {
	stubRun(t, func(cmd *exec.Cmd) error {
		fmt.Fprint(cmd.Stderr, "ERROR: Please run 'az login' to setup account.")
		return errors.New("exit status 1")
	})

	_, err := AccountShow("/cfg", "/ext")
	if err == nil {
		t.Fatal("AccountShow devolvió nil ante un fallo de az")
	}
	if !strings.Contains(err.Error(), "az login") {
		t.Errorf("error = %q, quería que incluyera lo que dijo az por stderr", err)
	}
}

func TestAccountShowWithoutStderr(t *testing.T) {
	want := errors.New("exit status 2")
	stubRun(t, func(*exec.Cmd) error { return want })

	_, err := AccountShow("/cfg", "/ext")
	if !errors.Is(err, want) {
		t.Errorf("error = %v, quería envolver a %v", err, want)
	}
}

func TestAvailable(t *testing.T) {
	t.Run("az presente", func(t *testing.T) {
		stubLookPath(t, func(string) (string, error) { return "/usr/local/bin/az", nil })
		if err := Available(); err != nil {
			t.Errorf("Available: %v", err)
		}
	})

	t.Run("az ausente", func(t *testing.T) {
		stubLookPath(t, func(name string) (string, error) {
			return "", fmt.Errorf("exec: %q: executable file not found in $PATH", name)
		})
		err := Available()
		if err == nil {
			t.Fatal("Available devolvió nil sin az en el PATH")
		}
		if !strings.Contains(err.Error(), "Azure CLI") {
			t.Errorf("error = %q, quería que nombrara Azure CLI", err)
		}
	})

	t.Run("se consulta el binario correcto", func(t *testing.T) {
		var asked string
		stubLookPath(t, func(name string) (string, error) {
			asked = name
			return "/usr/local/bin/az", nil
		})
		if err := Available(); err != nil {
			t.Fatalf("Available: %v", err)
		}
		if asked != "az" {
			t.Errorf("se buscó %q, quería «az»", asked)
		}
	})
}

// azsel's whole isolation model rests on overriding these two variables, and
// they may well already be exported — by azsel itself in an active session,
// or by an Azure CLI install like the one on GitHub's Linux runners. Whatever
// azsel appends has to win.
func TestCommandEnvOverridesInheritedValues(t *testing.T) {
	t.Setenv("AZURE_CONFIG_DIR", "/heredado/config")
	t.Setenv("AZURE_EXTENSION_DIR", "/heredado/extensions")

	got := stubRun(t, nil)
	if err := Login("TID", "/cfg/acme", "/ext/compartido", false); err != nil {
		t.Fatalf("Login: %v", err)
	}

	for _, c := range []struct{ key, want string }{
		{"AZURE_CONFIG_DIR", "/cfg/acme"},
		{"AZURE_EXTENSION_DIR", "/ext/compartido"},
	} {
		if v, _ := envValue(got.cmd, c.key); v != c.want {
			t.Errorf("%s efectivo = %q, quería %q", c.key, v, c.want)
		}
	}
}

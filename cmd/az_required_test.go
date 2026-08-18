package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
	"github.com/spf13/cobra"
)

// withoutAzureCLI points PATH at an empty directory, so exec.LookPath cannot
// find az. Real rather than stubbed: azure.Available's seam is unexported and
// this is what the user's machine actually looks like.
func withoutAzureCLI(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	t.Setenv("PATH", t.TempDir())
	return home
}

// quiet swallows the commands' direct writes to the process streams and
// returns a function reading back everything they wrote. The commands write
// straight to os.Stdout/os.Stderr rather than through cobra, so redirecting
// the process streams is the only way to see their output.
func quiet(t *testing.T) func() string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "output")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creando el fichero de salida: %v", err)
	}
	outOrig, errOrig := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = f, f
	t.Cleanup(func() {
		os.Stdout, os.Stderr = outOrig, errOrig
		f.Close()
	})
	return func() string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("leyendo la salida: %v", err)
		}
		return string(data)
	}
}

func run(t *testing.T, c *cobra.Command, args ...string) error {
	t.Helper()
	c.SilenceErrors, c.SilenceUsage = true, true
	c.SetOut(io.Discard)
	c.SetErr(io.Discard)
	c.SetArgs(args)
	return c.Execute()
}

// El coste de que az falte debe ser el mensaje y nada más. Antes el usuario
// escribía nombre y tenant ID, se creaba el directorio del tenant, y solo
// entonces reventaba con el error crudo de Go.
func TestAddFailsBeforeAskingAnything(t *testing.T) {
	home := withoutAzureCLI(t)
	quiet(t)

	// Lo que el usuario habría tecleado si se le hubiera preguntado.
	f := feedStdin(t, "acme\n11111111-1111-1111-1111-111111111111\n")

	err := run(t, newAddCmd())
	if err == nil {
		t.Fatal("add devolvió nil sin az instalado")
	}
	if !strings.Contains(err.Error(), "Azure CLI") {
		t.Errorf("error = %q, quería que nombrara Azure CLI", err)
	}
	if !strings.Contains(err.Error(), "install") {
		t.Errorf("error = %q, quería que dijera cómo instalarlo", err)
	}

	// Nada leído de stdin: no se llegó a preguntar.
	if pos, _ := f.Seek(0, io.SeekCurrent); pos != 0 {
		t.Errorf("se consumió stdin antes de comprobar az (offset %d)", pos)
	}
	// Y nada creado en disco.
	if entries, err := os.ReadDir(filepath.Join(home, "tenants")); err == nil && len(entries) > 0 {
		t.Errorf("se crearon %d directorios de tenant pese al fallo", len(entries))
	}
}

// Solo add necesita az. El resto debe seguir funcionando en una máquina sin
// Azure CLI — consultar tenants o cambiar de uno a otro no invoca nada.
func TestCommandsThatDoNotNeedAzureCLI(t *testing.T) {
	home := withoutAzureCLI(t)
	quiet(t)

	tenantDir := filepath.Join(home, "tenants", "acme")
	if err := os.MkdirAll(tenantDir, 0755); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	cfg := &config.Config{Tenants: []config.Tenant{
		{Name: "acme", TenantID: "11111111-1111-1111-1111-111111111111", ConfigDir: tenantDir},
	}}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("preparando: %v", err)
	}

	cases := []struct {
		name string
		cmd  *cobra.Command
		args []string
	}{
		{"list", newListCmd(), nil},
		{"use", newUseCmd(), []string{"acme"}},
		{"init --print", newInitCmd(), []string{"--print"}},
		{"remove -f", newRemoveCmd(), []string{"acme", "-f"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := run(t, c.cmd, c.args...); err != nil {
				t.Errorf("%s falló sin az instalado: %v", c.name, err)
			}
		})
	}
}

// feedStdin conecta os.Stdin a un fichero con lo que el usuario teclearía, y
// devuelve el descriptor para poder comprobar cuánto se ha llegado a leer.
func feedStdin(t *testing.T, content string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("preparando stdin: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("abriendo stdin falso: %v", err)
	}
	orig := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = orig
		f.Close()
	})
	return f
}

// fakeAzureCLI pone un az de mentira en el PATH con el cuerpo indicado. Deja
// ejercitar el camino real —Available lo encuentra, Login lo ejecuta— sin
// costuras y sin tocar Azure.
func fakeAzureCLI(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "az"), []byte("#!/bin/sh\n"+body+"\n"), 0755); err != nil {
		t.Fatalf("escribiendo el az falso: %v", err)
	}
	t.Setenv("PATH", dir)
}

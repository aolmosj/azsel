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

// quiet swallows the commands' direct writes to the process streams.
func quiet(t *testing.T) {
	t.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("abriendo %s: %v", os.DevNull, err)
	}
	outOrig, errOrig := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = devnull, devnull
	t.Cleanup(func() {
		os.Stdout, os.Stderr = outOrig, errOrig
		devnull.Close()
	})
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
	stdin := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(stdin, []byte("acme\n11111111-1111-1111-1111-111111111111\n"), 0644); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	f, err := os.Open(stdin)
	if err != nil {
		t.Fatalf("abriendo stdin falso: %v", err)
	}
	defer f.Close()
	orig := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = orig })

	err = run(t, newAddCmd())
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

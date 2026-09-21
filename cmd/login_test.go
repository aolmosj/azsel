package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

func TestLoginRequiresAzureCLI(t *testing.T) {
	withoutAzureCLI(t)
	quiet(t)
	err := run(t, newLoginCmd(), "acme")
	if err == nil {
		t.Fatal("login returned nil without az installed")
	}
	if !strings.Contains(err.Error(), "Azure CLI") {
		t.Errorf("error = %q, wanted it to name Azure CLI", err)
	}
}

// login runs `az login` scoped to the named tenant's config directory. The fake
// az records the AZURE_CONFIG_DIR it was invoked with.
func TestLoginRunsForNamedTenant(t *testing.T) {
	home := listSandbox(t, "contoso", "fabrikam")
	sentinel := filepath.Join(t.TempDir(), "login-config-dir")
	t.Setenv("AZSEL_TEST_SENTINEL", sentinel)
	fakeAzureCLI(t, `printf '%s' "$AZURE_CONFIG_DIR" > "$AZSEL_TEST_SENTINEL"`)

	quiet(t)
	if err := run(t, newLoginCmd(), "contoso"); err != nil {
		t.Fatalf("login: %v", err)
	}
	got, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("reading sentinel: %v", err)
	}
	want := filepath.Join(home, ".azsel", "tenants", "contoso")
	if string(got) != want {
		t.Errorf("login scoped to %q, wanted the tenant's config dir %q", got, want)
	}
}

func TestLoginResolvesDefaultWhenNoArg(t *testing.T) {
	home := listSandbox(t, "contoso", "fabrikam")
	cfg, _ := config.Load()
	if _, err := config.SetDefault(cfg, "fabrikam", "20260220-120000"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	sentinel := filepath.Join(t.TempDir(), "login-config-dir")
	t.Setenv("AZSEL_TEST_SENTINEL", sentinel)
	fakeAzureCLI(t, `printf '%s' "$AZURE_CONFIG_DIR" > "$AZSEL_TEST_SENTINEL"`)

	quiet(t)
	if err := run(t, newLoginCmd()); err != nil {
		t.Fatalf("login: %v", err)
	}
	got, _ := os.ReadFile(sentinel)
	want := filepath.Join(home, ".azsel", "tenants", "fabrikam")
	if string(got) != want {
		t.Errorf("login scoped to %q, wanted the default tenant %q", got, want)
	}
}

func TestLoginNoArgNoDefault(t *testing.T) {
	listSandbox(t, "contoso")
	fakeAzureCLI(t, "true")
	quiet(t)
	err := run(t, newLoginCmd())
	if err == nil || !strings.Contains(err.Error(), "default") {
		t.Errorf("error = %v, wanted it to mention the missing default", err)
	}
}

func TestLoginUnknownTenant(t *testing.T) {
	listSandbox(t, "contoso")
	fakeAzureCLI(t, "true")
	quiet(t)
	err := run(t, newLoginCmd(), "nope")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, wanted a not-found error", err)
	}
}

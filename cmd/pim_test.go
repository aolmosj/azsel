package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

const pimTwoRows = `{
  "value": [
    {
      "properties": {
        "status": "Provisioned",
        "endDateTime": null,
        "expandedProperties": {
          "scope": {"id": "/subscriptions/aaa", "displayName": "Prod Sub", "type": "subscription"},
          "roleDefinition": {"displayName": "Reader"}
        }
      }
    },
    {
      "properties": {
        "status": "Provisioned",
        "endDateTime": "2026-01-02T03:04:05Z",
        "expandedProperties": {
          "scope": {"id": "/subscriptions/aaa/resourceGroups/rg1", "displayName": "rg1", "type": "resourcegroup"},
          "roleDefinition": {"displayName": "Contributor"}
        }
      }
    }
  ]
}`

// pim list leans on `az rest`, so a missing Azure CLI must fail immediately with
// the install message — before loading config or resolving a tenant.
func TestPIMListRequiresAzureCLI(t *testing.T) {
	withoutAzureCLI(t)
	quiet(t)

	err := run(t, newPIMCmd(), "list", "acme")
	if err == nil {
		t.Fatal("pim list returned nil without az installed")
	}
	if !strings.Contains(err.Error(), "Azure CLI") {
		t.Errorf("error = %q, wanted it to name Azure CLI", err)
	}
}

func TestPIMListPrintsRows(t *testing.T) {
	listSandbox(t, "contoso")
	fakeAzureCLI(t, "cat <<'JSON'\n"+pimTwoRows+"\nJSON")

	out := quiet(t)
	if err := run(t, newPIMCmd(), "list", "contoso"); err != nil {
		t.Fatalf("pim list: %v", err)
	}
	got := out()
	for _, want := range []string{"Reader", "Contributor", "rg1", "Prod Sub"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	// A null end date shows as permanent; a bounded one shows its date.
	if !strings.Contains(got, "permanent") {
		t.Errorf("a permanent eligibility should read 'permanent':\n%s", got)
	}
	if !strings.Contains(got, "2026-01-02T03:04:05Z") {
		t.Errorf("a bounded eligibility should show its end date:\n%s", got)
	}
}

// With no name, pim list queries the default tenant. The fake az records the
// AZURE_CONFIG_DIR it was scoped to, proving the right tenant was used.
func TestPIMListResolvesDefaultWhenNoArg(t *testing.T) {
	home := listSandbox(t, "contoso", "fabrikam")
	cfg, _ := config.Load()
	if _, err := config.SetDefault(cfg, "fabrikam", "20260220-120000"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	sentinel := filepath.Join(t.TempDir(), "scoped-config-dir")
	t.Setenv("AZSEL_TEST_SENTINEL", sentinel)
	fakeAzureCLI(t, `printf '%s' "$AZURE_CONFIG_DIR" > "$AZSEL_TEST_SENTINEL"`+"\ncat <<'JSON'\n"+`{"value":[]}`+"\nJSON")

	quiet(t)
	if err := run(t, newPIMCmd(), "list"); err != nil {
		t.Fatalf("pim list: %v", err)
	}

	scoped, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("reading sentinel: %v", err)
	}
	want := filepath.Join(home, ".azsel", "tenants", "fabrikam")
	if string(scoped) != want {
		t.Errorf("queried config dir = %q, wanted the default tenant's %q", scoped, want)
	}
}

func TestPIMListNoArgNoDefault(t *testing.T) {
	listSandbox(t, "contoso")
	fakeAzureCLI(t, "true")
	quiet(t)

	err := run(t, newPIMCmd(), "list")
	if err == nil {
		t.Fatal("pim list returned nil with no arg and no default")
	}
	if !strings.Contains(err.Error(), "default") {
		t.Errorf("error = %q, wanted it to mention the missing default", err)
	}
}

func TestPIMListUnknownTenant(t *testing.T) {
	listSandbox(t, "contoso")
	fakeAzureCLI(t, "true")
	quiet(t)

	err := run(t, newPIMCmd(), "list", "nope")
	if err == nil {
		t.Fatal("pim list returned nil for an unknown tenant")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, wanted a not-found error", err)
	}
}

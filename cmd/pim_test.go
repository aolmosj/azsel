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
      "name": "inst-reader",
      "properties": {
        "status": "Provisioned",
        "endDateTime": null,
        "expandedProperties": {
          "scope": {"id": "/subscriptions/aaa", "displayName": "Prod Sub", "type": "subscription"},
          "roleDefinition": {"displayName": "Reader"},
          "principal": {"id": "grp-1", "displayName": "PlatformTeam", "type": "Group"}
        }
      }
    },
    {
      "name": "inst-contrib",
      "properties": {
        "status": "Provisioned",
        "endDateTime": "2026-01-02T03:04:05Z",
        "expandedProperties": {
          "scope": {"id": "/subscriptions/aaa/resourceGroups/rg1", "displayName": "rg1", "type": "resourcegroup"},
          "roleDefinition": {"displayName": "Contributor"},
          "principal": {"id": "me-id", "displayName": "Antonio", "type": "User"}
        }
      }
    }
  ]
}`

// fakeAzurePIM is a fake `az` answering the Strategy 1 calls ListEligible makes:
// the caller's Graph identity, their groups, and the atScopeAndBelow eligibility
// query (whose rows are assigned to that identity/group).
const fakeAzurePIM = `case "$*" in
  *"/v1.0/me?"*)
    echo '{"id":"me-id"}'
    ;;
  *transitiveMemberOf*)
    echo '{"value":[{"id":"grp-1"}]}'
    ;;
  *roleEligibilityScheduleInstances*)
    cat <<'JSON'
` + pimTwoRows + `
JSON
    ;;
  *)
    echo '{"value":[]}'
    ;;
esac`

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
	fakeAzureCLI(t, fakeAzurePIM)

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
	// A group-inherited row names the group; a direct one reads "direct".
	if !strings.Contains(got, "PlatformTeam") {
		t.Errorf("a group eligibility should name the group:\n%s", got)
	}
	if !strings.Contains(got, "direct") {
		t.Errorf("a direct eligibility should read 'direct':\n%s", got)
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
	// Record the config dir on the (single, non-concurrent) subscriptions call.
	fakeAzureCLI(t, `case "$*" in
  *"/subscriptions?api-version"*) printf '%s' "$AZURE_CONFIG_DIR" > "$AZSEL_TEST_SENTINEL" ;;
esac
echo '{"value":[]}'`)

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

// An expired session is reported with an actionable message before any PIM
// call, rather than surfacing a raw az error.
func TestPIMListExpiredSession(t *testing.T) {
	listSandbox(t, "contoso")
	fakeAzureCLI(t, `case "$*" in
  *get-access-token*) exit 1 ;;
  *) echo '{"value":[]}' ;;
esac`)
	quiet(t)
	err := run(t, newPIMCmd(), "list", "contoso")
	if err == nil {
		t.Fatal("pim list returned nil on an expired session")
	}
	if !strings.Contains(err.Error(), "expired") || !strings.Contains(err.Error(), "azsel login") {
		t.Errorf("error = %q, wanted an expired-session message pointing at 'azsel login'", err)
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

package pim

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// stubRestGet swaps the Azure CLI seam for one test, restoring it afterwards.
// fn receives the config dir and URL azsel built, so a test can both feed a
// canned body and assert on the request.
func stubRestGet(t *testing.T, fn func(configDir, url string) ([]byte, error)) {
	t.Helper()
	orig := restGet
	restGet = fn
	t.Cleanup(func() { restGet = orig })
}

const twoRows = `{
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

func TestListEligibleParses(t *testing.T) {
	stubRestGet(t, func(_, _ string) ([]byte, error) { return []byte(twoRows), nil })

	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, wanted 2", len(got))
	}

	// Sorted by scope type first: "resourcegroup" precedes "subscription".
	if got[0].RoleName != "Contributor" || got[1].RoleName != "Reader" {
		t.Fatalf("order = [%s, %s], wanted [Contributor, Reader]", got[0].RoleName, got[1].RoleName)
	}

	rg := got[0]
	if rg.ScopeName != "rg1" || rg.ScopeType != "resourcegroup" || rg.ScopeID != "/subscriptions/aaa/resourceGroups/rg1" {
		t.Errorf("scope fields = %+v", rg)
	}
	if rg.Status != "Provisioned" {
		t.Errorf("status = %q, wanted Provisioned", rg.Status)
	}
	if rg.End == nil || !rg.End.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("end = %v, wanted 2026-01-02T03:04:05Z", rg.End)
	}

	// A null endDateTime is a permanent eligibility.
	if got[1].End != nil {
		t.Errorf("Reader end = %v, wanted nil (permanent)", got[1].End)
	}
}

func TestListEligibleEmpty(t *testing.T) {
	stubRestGet(t, func(_, _ string) ([]byte, error) { return []byte(`{"value":[]}`), nil })
	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d rows, wanted 0", len(got))
	}
}

func TestListEligiblePropagatesRestError(t *testing.T) {
	want := errors.New("please run 'az login'")
	stubRestGet(t, func(_, _ string) ([]byte, error) { return nil, want })
	if _, err := ListEligible("/cfg", "TID"); !errors.Is(err, want) {
		t.Errorf("error = %v, wanted it to wrap %v", err, want)
	}
}

// An empty tenant ID is caught before any az call: a Tenant can be stored
// without one, and there is nothing to query.
func TestListEligibleEmptyTenantID(t *testing.T) {
	called := false
	stubRestGet(t, func(_, _ string) ([]byte, error) {
		called = true
		return nil, nil
	})
	_, err := ListEligible("/cfg", "   ")
	if err == nil {
		t.Fatal("ListEligible returned nil with an empty tenant ID")
	}
	if !strings.Contains(err.Error(), "tenant ID") {
		t.Errorf("error = %q, wanted it to name the tenant ID", err)
	}
	if called {
		t.Error("az was queried despite the empty tenant ID")
	}
}

// The scope decision — tenant root management group + asTarget() — is the whole
// point, so pin the URL against regressions.
func TestListEligibleURLUsesRootMG(t *testing.T) {
	var url string
	stubRestGet(t, func(_, u string) ([]byte, error) {
		url = u
		return []byte(`{"value":[]}`), nil
	})
	if _, err := ListEligible("/cfg", "the-tid"); err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	for _, want := range []string{
		"managementGroups/the-tid",
		"roleEligibilityScheduleInstances",
		"api-version=2020-10-01",
		"$filter=asTarget()",
	} {
		if !strings.Contains(url, want) {
			t.Errorf("URL %q missing %q", url, want)
		}
	}
}

func TestListEligibleMalformedJSON(t *testing.T) {
	stubRestGet(t, func(_, _ string) ([]byte, error) { return []byte("not json"), nil })
	if _, err := ListEligible("/cfg", "TID"); err == nil {
		t.Error("ListEligible accepted malformed JSON")
	}
}

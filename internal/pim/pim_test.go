package pim

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubRestGet swaps the Azure CLI seam for one test, routing by URL. fn must be
// safe for concurrent use: ListEligible queries scopes in parallel.
func stubRestGet(t *testing.T, fn func(url string) ([]byte, error)) {
	t.Helper()
	orig := restGet
	restGet = func(_, url string) ([]byte, error) { return fn(url) }
	t.Cleanup(func() { restGet = orig })
}

func subsBody(ids ...string) []byte {
	var b strings.Builder
	b.WriteString(`{"value":[`)
	for i, id := range ids {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"subscriptionId":%q}`, id)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

type inst struct {
	name, role, scopeName, scopeType, end string
	pType, pName                          string
}

func instBody(items ...inst) []byte {
	var b strings.Builder
	b.WriteString(`{"value":[`)
	for i, it := range items {
		if i > 0 {
			b.WriteString(",")
		}
		end := "null"
		if it.end != "" {
			end = fmt.Sprintf("%q", it.end)
		}
		pType, pName := it.pType, it.pName
		if pType == "" {
			pType, pName = "User", "Me"
		}
		fmt.Fprintf(&b, `{"name":%q,"properties":{"status":"Provisioned","endDateTime":%s,`+
			`"expandedProperties":{"scope":{"id":"/scope/%s","displayName":%q,"type":%q},`+
			`"roleDefinition":{"displayName":%q},"principal":{"displayName":%q,"type":%q}}}}`,
			it.name, end, it.name, it.scopeName, it.scopeType, it.role, pName, pType)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

func isSubsList(url string) bool { return strings.Contains(url, "/subscriptions?api-version") }
func isRootMG(url string) bool   { return strings.Contains(url, "/managementGroups/") }
func forSub(url, id string) bool { return strings.Contains(url, "/subscriptions/"+id+"/") }

// The same management-group eligibility surfaces through every subscription
// below it, so it must be deduplicated by instance id; subscription- and
// MG-scoped rows both appear, sorted deterministically.
func TestListEligibleAggregatesAndDedupes(t *testing.T) {
	mg := inst{name: "mg1", role: "Owner", scopeName: "AOC root", scopeType: "managementgroup", pType: "Group", pName: "Delivery"}
	stubRestGet(t, func(url string) ([]byte, error) {
		switch {
		case isSubsList(url):
			return subsBody("sub-1", "sub-2"), nil
		case forSub(url, "sub-1"):
			return instBody(mg, inst{name: "s1", role: "Reader", scopeName: "App1", scopeType: "subscription", end: "2027-01-02T03:04:05Z"}), nil
		case forSub(url, "sub-2"):
			return instBody(mg, inst{name: "s2", role: "Contributor", scopeName: "App2", scopeType: "subscription"}), nil
		case isRootMG(url):
			return []byte(`{"value":[]}`), nil
		}
		return nil, fmt.Errorf("unexpected url %q", url)
	})

	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	// mg1 seen via both subscriptions collapses to one row: 3 unique.
	if len(got) != 3 {
		t.Fatalf("got %d rows, wanted 3 (deduped): %+v", len(got), got)
	}
	// Sorted: managementgroup first, then subscription rows by role.
	want := []struct{ role, scope, typ string }{
		{"Owner", "AOC root", "managementgroup"},
		{"Contributor", "App2", "subscription"},
		{"Reader", "App1", "subscription"},
	}
	for i, w := range want {
		if got[i].RoleName != w.role || got[i].ScopeName != w.scope || got[i].ScopeType != w.typ {
			t.Errorf("row %d = %+v, wanted %v", i, got[i], w)
		}
	}
	// The bounded row keeps its end; the permanent ones stay nil.
	if got[2].End == nil || !got[2].End.Equal(time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("Reader end = %v, wanted 2027-01-02T03:04:05Z", got[2].End)
	}
	if got[0].End != nil {
		t.Errorf("MG Owner end = %v, wanted nil (permanent)", got[0].End)
	}
	// The group-inherited row reports the group it comes through; a direct one
	// does not.
	if got[0].ViaGroup() != "Delivery" {
		t.Errorf("MG Owner ViaGroup() = %q, wanted Delivery", got[0].ViaGroup())
	}
	if got[2].ViaGroup() != "" {
		t.Errorf("direct row ViaGroup() = %q, wanted empty", got[2].ViaGroup())
	}
}

// The subscriptions call is the auth gate; its failure is the whole result.
func TestListEligiblePropagatesSubscriptionsError(t *testing.T) {
	want := errors.New("please run 'az login'")
	stubRestGet(t, func(url string) ([]byte, error) {
		if isSubsList(url) {
			return nil, want
		}
		return []byte(`{"value":[]}`), nil
	})
	if _, err := ListEligible("/cfg", "TID"); !errors.Is(err, want) {
		t.Errorf("error = %v, wanted it to wrap %v", err, want)
	}
}

// A 403 on one subscription must not hide the eligibilities from the others.
func TestListEligibleSkipsPerScopeErrors(t *testing.T) {
	stubRestGet(t, func(url string) ([]byte, error) {
		switch {
		case isSubsList(url):
			return subsBody("sub-1", "sub-2"), nil
		case forSub(url, "sub-1"):
			return nil, errors.New("403 forbidden")
		case forSub(url, "sub-2"):
			return instBody(inst{name: "s2", role: "Reader", scopeName: "App2", scopeType: "subscription"}), nil
		default: // root MG
			return []byte(`{"value":[]}`), nil
		}
	})
	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 1 || got[0].RoleName != "Reader" {
		t.Errorf("got %+v, wanted the single Reader row from sub-2", got)
	}
}

// If every scope fails, surface the reason rather than a misleading empty list.
func TestListEligibleAllScopesFail(t *testing.T) {
	boom := errors.New("403 forbidden")
	stubRestGet(t, func(url string) ([]byte, error) {
		if isSubsList(url) {
			return subsBody("sub-1"), nil
		}
		return nil, boom
	})
	if _, err := ListEligible("/cfg", "TID"); !errors.Is(err, boom) {
		t.Errorf("error = %v, wanted it to wrap %v", err, boom)
	}
}

// No subscriptions and no tenant ID means nothing to query — empty, not an error.
func TestListEligibleEmptyWhenNoScopes(t *testing.T) {
	queried := false
	stubRestGet(t, func(url string) ([]byte, error) {
		if isSubsList(url) {
			return []byte(`{"value":[]}`), nil
		}
		queried = true
		return []byte(`{"value":[]}`), nil
	})
	got, err := ListEligible("/cfg", "")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d rows, wanted 0", len(got))
	}
	if queried {
		t.Error("a scope was queried despite no subscriptions and no tenant ID")
	}
}

// Without a tenant ID the subscriptions are still queried; only the root-MG
// safety net is skipped.
func TestListEligibleWorksWithoutTenantID(t *testing.T) {
	var (
		mu   sync.Mutex
		urls []string
	)
	stubRestGet(t, func(url string) ([]byte, error) {
		mu.Lock()
		urls = append(urls, url)
		mu.Unlock()
		switch {
		case isSubsList(url):
			return subsBody("sub-1"), nil
		case forSub(url, "sub-1"):
			return instBody(inst{name: "s1", role: "Reader", scopeName: "App1", scopeType: "subscription"}), nil
		}
		return []byte(`{"value":[]}`), nil
	})
	got, err := ListEligible("/cfg", "")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, wanted 1", len(got))
	}
	mu.Lock()
	defer mu.Unlock()
	for _, u := range urls {
		if isRootMG(u) {
			t.Errorf("the root management group was queried with no tenant ID: %q", u)
		}
	}
}

// The per-scope query is asTarget() at the subscription, plus the tenant root MG.
func TestListEligibleQueriesScopesWithAsTarget(t *testing.T) {
	var (
		mu   sync.Mutex
		urls []string
	)
	stubRestGet(t, func(url string) ([]byte, error) {
		mu.Lock()
		urls = append(urls, url)
		mu.Unlock()
		if isSubsList(url) {
			return subsBody("sub-1"), nil
		}
		return []byte(`{"value":[]}`), nil
	})
	if _, err := ListEligible("/cfg", "the-tid"); err != nil {
		t.Fatalf("ListEligible: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	var sawSub, sawMG bool
	for _, u := range urls {
		if forSub(u, "sub-1") {
			sawSub = true
			for _, want := range []string{"roleEligibilityScheduleInstances", "api-version=2020-10-01", "$filter=asTarget()"} {
				if !strings.Contains(u, want) {
					t.Errorf("subscription query %q missing %q", u, want)
				}
			}
		}
		if strings.Contains(u, "/managementGroups/the-tid/") {
			sawMG = true
		}
	}
	if !sawSub {
		t.Error("no per-subscription eligibility query was made")
	}
	if !sawMG {
		t.Error("the tenant root management group was not queried")
	}
}

func TestListEligibleMalformedSubscriptions(t *testing.T) {
	stubRestGet(t, func(url string) ([]byte, error) {
		if isSubsList(url) {
			return []byte("not json"), nil
		}
		return []byte(`{"value":[]}`), nil
	})
	if _, err := ListEligible("/cfg", "TID"); err == nil {
		t.Error("ListEligible accepted malformed subscriptions JSON")
	}
}

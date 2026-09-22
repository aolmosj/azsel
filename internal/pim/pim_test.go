package pim

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubPIM swaps the two Azure CLI seams for one test, restoring them after. The
// get func answers both the subscriptions list and the per-scope query, routed
// by URL.
func stubPIM(t *testing.T,
	token func(configDir, tenantID string) (string, error),
	get func(configDir, url, token string) ([]byte, error),
) {
	t.Helper()
	ot, og := tenantToken, restGet
	tenantToken, restGet = token, get
	t.Cleanup(func() { tenantToken, restGet = ot, og })
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
	name, role, scopeName, scopeType, end, pType, pName string
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
func forSub(url, id string) bool { return strings.Contains(url, "/subscriptions/"+id+"/") }

// The subscriptions are enumerated with the tenant token (not az account list),
// and each is queried with that same token via asTarget().
func TestListEligibleQueriesWithTenantToken(t *testing.T) {
	var (
		mu       sync.Mutex
		gotToken string
		queried  int
	)
	stubPIM(t,
		func(_, tenantID string) (string, error) { return "tok-" + tenantID, nil },
		func(_, url, token string) ([]byte, error) {
			mu.Lock()
			gotToken = token
			if !isSubsList(url) {
				queried++
			}
			mu.Unlock()
			switch {
			case isSubsList(url):
				return subsBody("s1", "s2"), nil
			case forSub(url, "s1"):
				return instBody(inst{name: "a", role: "Owner", scopeName: "Sub1", scopeType: "subscription"}), nil
			case forSub(url, "s2"):
				return instBody(inst{name: "b", role: "Reader", scopeName: "Sub2", scopeType: "subscription"}), nil
			}
			return nil, fmt.Errorf("unexpected url %q", url)
		},
	)

	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, wanted 2: %+v", len(got), got)
	}
	if gotToken != "tok-TID" {
		t.Errorf("queried with token %q, wanted the tenant token tok-TID", gotToken)
	}
	if queried != 2 {
		t.Errorf("queried %d scopes, wanted 2 (one per subscription)", queried)
	}
}

// A management-group eligibility seen through several subscriptions is
// deduplicated by instance id; rows sort deterministically.
func TestListEligibleDedupesAndSorts(t *testing.T) {
	mg := inst{name: "mg1", role: "Owner", scopeName: "Root MG", scopeType: "managementgroup", pType: "Group", pName: "Team"}
	stubPIM(t,
		func(_, _ string) (string, error) { return "tok", nil },
		func(_, url, _ string) ([]byte, error) {
			switch {
			case isSubsList(url):
				return subsBody("s1", "s2"), nil
			case forSub(url, "s1"):
				return instBody(mg, inst{name: "x", role: "Reader", scopeName: "Sub1", scopeType: "subscription", end: "2027-01-02T03:04:05Z"}), nil
			case forSub(url, "s2"):
				return instBody(mg, inst{name: "y", role: "Contributor", scopeName: "Sub2", scopeType: "subscription"}), nil
			}
			return []byte(`{"value":[]}`), nil
		},
	)
	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d rows, wanted 3 (deduped): %+v", len(got), got)
	}
	if got[0].RoleName != "Owner" || got[0].ViaGroup() != "Team" {
		t.Errorf("row0 = %+v, wanted the group-inherited MG Owner", got[0])
	}
	if got[2].End == nil || !got[2].End.Equal(time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("Reader end = %v, wanted 2027-01-02T03:04:05Z", got[2].End)
	}
}

func TestListEligibleTokenError(t *testing.T) {
	want := errors.New("session expired; run 'azsel login'")
	stubPIM(t,
		func(_, _ string) (string, error) { return "", want },
		func(_, _, _ string) ([]byte, error) { t.Fatal("queried despite token failure"); return nil, nil },
	)
	if _, err := ListEligible("/cfg", "TID"); !errors.Is(err, want) {
		t.Errorf("error = %v, wanted the token error", err)
	}
}

func TestListEligibleSubscriptionsError(t *testing.T) {
	want := errors.New("cannot list subscriptions")
	stubPIM(t,
		func(_, _ string) (string, error) { return "tok", nil },
		func(_, url, _ string) ([]byte, error) {
			if isSubsList(url) {
				return nil, want
			}
			return nil, nil
		},
	)
	if _, err := ListEligible("/cfg", "TID"); !errors.Is(err, want) {
		t.Errorf("error = %v, wanted the subscriptions error", err)
	}
}

func TestListEligibleNoSubscriptions(t *testing.T) {
	queried := false
	stubPIM(t,
		func(_, _ string) (string, error) { return "tok", nil },
		func(_, url, _ string) ([]byte, error) {
			if isSubsList(url) {
				return subsBody(), nil
			}
			queried = true
			return nil, nil
		},
	)
	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d rows, wanted 0", len(got))
	}
	if queried {
		t.Error("queried a scope despite no subscriptions")
	}
}

// A failure on one subscription must not hide the eligibilities from the others.
func TestListEligibleSkipsPerScopeErrors(t *testing.T) {
	stubPIM(t,
		func(_, _ string) (string, error) { return "tok", nil },
		func(_, url, _ string) ([]byte, error) {
			switch {
			case isSubsList(url):
				return subsBody("s1", "s2"), nil
			case forSub(url, "s1"):
				return nil, errors.New("403 forbidden")
			}
			return instBody(inst{name: "b", role: "Reader", scopeName: "Sub2", scopeType: "subscription"}), nil
		},
	)
	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 1 || got[0].RoleName != "Reader" {
		t.Errorf("got %+v, wanted the single Reader row from s2", got)
	}
}

func TestListEligibleEmptyTenantID(t *testing.T) {
	called := false
	stubPIM(t,
		func(_, _ string) (string, error) { called = true; return "", nil },
		func(_, _, _ string) ([]byte, error) { return nil, nil },
	)
	_, err := ListEligible("/cfg", "  ")
	if err == nil || !strings.Contains(err.Error(), "tenant ID") {
		t.Errorf("error = %v, wanted it to name the missing tenant ID", err)
	}
	if called {
		t.Error("acquired a token despite an empty tenant ID")
	}
}

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
// safe for concurrent use: Strategy 2 queries scopes in parallel.
func stubRestGet(t *testing.T, fn func(url string) ([]byte, error)) {
	t.Helper()
	orig := restGet
	restGet = func(_, url string) ([]byte, error) { return fn(url) }
	t.Cleanup(func() { restGet = orig })
}

func isMe(u string) bool            { return strings.Contains(u, "/v1.0/me?") }
func isMyGroups(u string) bool      { return strings.Contains(u, "transitiveMemberOf") }
func isScopeAndBelow(u string) bool { return strings.Contains(u, "atScopeAndBelow") }
func isSubsList(u string) bool      { return strings.Contains(u, "/subscriptions?api-version") }
func forSub(u, id string) bool      { return strings.Contains(u, "/subscriptions/"+id+"/") }

func meBody(id string) []byte { return []byte(fmt.Sprintf(`{"id":%q}`, id)) }

func groupsBody(ids ...string) []byte {
	var b strings.Builder
	b.WriteString(`{"value":[`)
	for i, id := range ids {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":%q}`, id)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
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
	pType, pName, pID                     string
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
			`"roleDefinition":{"displayName":%q},`+
			`"principal":{"id":%q,"displayName":%q,"type":%q}}}}`,
			it.name, end, it.name, it.scopeName, it.scopeType, it.role, it.pID, pName, pType)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

// Strategy 1: one atScopeAndBelow() query, filtered client-side to the caller
// and the groups they belong to — no per-subscription fan-out.
func TestStrategy1FiltersByPrincipalAndGroups(t *testing.T) {
	var (
		mu   sync.Mutex
		urls []string
	)
	stubRestGet(t, func(url string) ([]byte, error) {
		mu.Lock()
		urls = append(urls, url)
		mu.Unlock()
		switch {
		case isMe(url):
			return meBody("me-id"), nil
		case isMyGroups(url):
			return groupsBody("grp-1"), nil
		case isScopeAndBelow(url):
			return instBody(
				inst{name: "a", role: "Owner", scopeName: "Sub", scopeType: "subscription", pType: "User", pName: "Me", pID: "me-id"},
				inst{name: "b", role: "Reader", scopeName: "MG", scopeType: "managementgroup", pType: "Group", pName: "Team", pID: "grp-1"},
				inst{name: "c", role: "Owner", scopeName: "Sub", scopeType: "subscription", pType: "User", pName: "Someone", pID: "other-user"},
				inst{name: "d", role: "Owner", scopeName: "MG", scopeType: "managementgroup", pType: "Group", pName: "OtherTeam", pID: "grp-99"},
			), nil
		}
		return nil, fmt.Errorf("unexpected url %q", url)
	})

	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	// Only the caller's own (a) and their group's (b) survive; c (another user)
	// and d (a group they are not in) are dropped.
	if len(got) != 2 {
		t.Fatalf("got %d rows, wanted 2 (mine + my group): %+v", len(got), got)
	}
	if got[0].RoleName != "Reader" || got[0].ViaGroup() != "Team" {
		t.Errorf("row0 = %+v, wanted the group-inherited Reader", got[0])
	}
	if got[1].RoleName != "Owner" || got[1].ViaGroup() != "" {
		t.Errorf("row1 = %+v, wanted the direct Owner", got[1])
	}
	// Strategy 1 must not fan out over subscriptions.
	mu.Lock()
	defer mu.Unlock()
	for _, u := range urls {
		if isSubsList(u) || strings.Contains(u, "asTarget()") {
			t.Errorf("Strategy 1 made a per-subscription call: %q", u)
		}
	}
}

// Without a Graph identity (e.g. a service-principal profile), Strategy 1 cannot
// filter, so it falls back to the per-subscription path.
func TestFallbackWhenNoGraphIdentity(t *testing.T) {
	stubRestGet(t, func(url string) ([]byte, error) {
		switch {
		case isMe(url):
			return nil, errors.New("Insufficient privileges to complete the operation")
		case isSubsList(url):
			return subsBody("sub-1"), nil
		case forSub(url, "sub-1"):
			return instBody(inst{name: "s1", role: "Reader", scopeName: "App1", scopeType: "subscription"}), nil
		}
		return []byte(`{"value":[]}`), nil
	})
	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 1 || got[0].RoleName != "Reader" {
		t.Errorf("got %+v, wanted the Strategy 2 Reader row", got)
	}
}

// No tenant id means Strategy 1 cannot address the root MG; it falls back.
func TestFallbackWhenNoTenantID(t *testing.T) {
	called := false
	stubRestGet(t, func(url string) ([]byte, error) {
		if isMe(url) || isScopeAndBelow(url) {
			called = true
		}
		switch {
		case isSubsList(url):
			return subsBody("sub-1"), nil
		case forSub(url, "sub-1"):
			return instBody(inst{name: "s1", role: "Owner", scopeName: "App1", scopeType: "subscription"}), nil
		}
		return []byte(`{"value":[]}`), nil
	})
	got, err := ListEligible("/cfg", "")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, wanted 1 from Strategy 2", len(got))
	}
	if called {
		t.Error("Strategy 1 was attempted despite an empty tenant ID")
	}
}

// atScopeAndBelow deduplicates by instance id like the per-subscription path.
func TestStrategy1Dedupes(t *testing.T) {
	stubRestGet(t, func(url string) ([]byte, error) {
		switch {
		case isMe(url):
			return meBody("me-id"), nil
		case isMyGroups(url):
			return groupsBody(), nil
		case isScopeAndBelow(url):
			return instBody(
				inst{name: "dup", role: "Owner", scopeName: "MG", scopeType: "managementgroup", pID: "me-id"},
				inst{name: "dup", role: "Owner", scopeName: "MG", scopeType: "managementgroup", pID: "me-id"},
			), nil
		}
		return nil, fmt.Errorf("unexpected url %q", url)
	})
	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d rows, wanted 1 after dedup", len(got))
	}
}

// Throttling on Strategy 1's ARM query is surfaced as a clear message, and does
// NOT fall back (which would fan out more calls into the same throttle).
func TestThrottleOnStrategy1IsSurfaced(t *testing.T) {
	fannedOut := false
	stubRestGet(t, func(url string) ([]byte, error) {
		switch {
		case isMe(url):
			return meBody("me-id"), nil
		case isMyGroups(url):
			return groupsBody(), nil
		case isScopeAndBelow(url):
			return nil, errors.New(`Bad Request({"error":{"code":"AadPremiumLicenseRequired"}})`)
		case isSubsList(url):
			fannedOut = true
			return subsBody("sub-1"), nil
		}
		return []byte(`{"value":[]}`), nil
	})
	_, err := ListEligible("/cfg", "TID")
	if err == nil {
		t.Fatal("ListEligible returned nil on a throttled tenant")
	}
	if !strings.Contains(err.Error(), "rate-limited") {
		t.Errorf("error = %q, wanted a clear rate-limit message", err)
	}
	if fannedOut {
		t.Error("throttling on Strategy 1 fell back to the per-subscription fan-out")
	}
}

// The per-subscription path (reached via fallback) aggregates and dedupes the
// same management-group eligibility seen through several subscriptions.
func TestStrategy2AggregatesAndDedupes(t *testing.T) {
	mg := inst{name: "mg1", role: "Owner", scopeName: "AOC root", scopeType: "managementgroup", pType: "Group", pName: "Delivery"}
	stubRestGet(t, func(url string) ([]byte, error) {
		switch {
		case isMe(url):
			return nil, errors.New("no graph") // force fallback
		case isSubsList(url):
			return subsBody("sub-1", "sub-2"), nil
		case forSub(url, "sub-1"):
			return instBody(mg, inst{name: "s1", role: "Reader", scopeName: "App1", scopeType: "subscription", end: "2027-01-02T03:04:05Z"}), nil
		case forSub(url, "sub-2"):
			return instBody(mg, inst{name: "s2", role: "Contributor", scopeName: "App2", scopeType: "subscription"}), nil
		}
		return []byte(`{"value":[]}`), nil
	})
	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d rows, wanted 3 (deduped): %+v", len(got), got)
	}
	if got[0].RoleName != "Owner" || got[0].ViaGroup() != "Delivery" {
		t.Errorf("row0 = %+v, wanted the group-inherited MG Owner", got[0])
	}
	if got[2].End == nil || !got[2].End.Equal(time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("Reader end = %v, wanted 2027-01-02T03:04:05Z", got[2].End)
	}
}

// A 403 on one subscription must not hide the eligibilities from the others.
func TestStrategy2SkipsPerScopeErrors(t *testing.T) {
	stubRestGet(t, func(url string) ([]byte, error) {
		switch {
		case isMe(url):
			return nil, errors.New("no graph")
		case isSubsList(url):
			return subsBody("sub-1", "sub-2"), nil
		case forSub(url, "sub-1"):
			return nil, errors.New("403 forbidden")
		case forSub(url, "sub-2"):
			return instBody(inst{name: "s2", role: "Reader", scopeName: "App2", scopeType: "subscription"}), nil
		}
		return []byte(`{"value":[]}`), nil
	})
	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 1 || got[0].RoleName != "Reader" {
		t.Errorf("got %+v, wanted the single Reader row from sub-2", got)
	}
}

// If every subscription throttles, the clear rate-limit message is surfaced.
func TestStrategy2AllThrottled(t *testing.T) {
	stubRestGet(t, func(url string) ([]byte, error) {
		switch {
		case isMe(url):
			return nil, errors.New("no graph")
		case isSubsList(url):
			return subsBody("sub-1"), nil
		}
		return nil, errors.New(`Bad Request({"error":{"code":"AadPremiumLicenseRequired"}})`)
	})
	_, err := ListEligible("/cfg", "TID")
	if err == nil || !strings.Contains(err.Error(), "rate-limited") {
		t.Errorf("error = %v, wanted a clear rate-limit message", err)
	}
}

// Graph and ARM paginate; both nextLink conventions are followed.
func TestStrategy1FollowsPagination(t *testing.T) {
	stubRestGet(t, func(url string) ([]byte, error) {
		switch {
		case isMe(url):
			return meBody("me-id"), nil
		case isMyGroups(url) && !strings.Contains(url, "page2"):
			return []byte(`{"value":[{"id":"grp-1"}],"@odata.nextLink":"https://graph.microsoft.com/page2"}`), nil
		case strings.Contains(url, "graph.microsoft.com/page2"):
			return []byte(`{"value":[{"id":"grp-2"}]}`), nil
		case isScopeAndBelow(url) && !strings.Contains(url, "arm-page2"):
			return []byte(`{"value":[{"name":"a","properties":{"status":"Provisioned","expandedProperties":{"scope":{"displayName":"S","type":"subscription"},"roleDefinition":{"displayName":"Owner"},"principal":{"id":"grp-2"}}}}],"nextLink":"https://management.azure.com/arm-page2"}`), nil
		case strings.Contains(url, "arm-page2"):
			return []byte(`{"value":[{"name":"b","properties":{"status":"Provisioned","expandedProperties":{"scope":{"displayName":"S2","type":"subscription"},"roleDefinition":{"displayName":"Reader"},"principal":{"id":"me-id"}}}}]}`), nil
		}
		return nil, fmt.Errorf("unexpected url %q", url)
	})
	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	// "a" matches grp-2 (from the second Graph page); "b" matches me — both need
	// pagination to be seen.
	if len(got) != 2 {
		t.Fatalf("got %d rows, wanted 2 across paginated pages: %+v", len(got), got)
	}
}

func TestListEligibleMalformedSubscriptions(t *testing.T) {
	stubRestGet(t, func(url string) ([]byte, error) {
		if isMe(url) {
			return nil, errors.New("no graph")
		}
		if isSubsList(url) {
			return []byte("not json"), nil
		}
		return []byte(`{"value":[]}`), nil
	})
	if _, err := ListEligible("/cfg", "TID"); err == nil {
		t.Error("ListEligible accepted malformed subscriptions JSON")
	}
}

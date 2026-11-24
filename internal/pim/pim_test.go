package pim

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubPIM swaps the two read seams for one test, restoring them after. The get
// func answers both the subscriptions list and the per-scope query, routed by
// URL. The token func takes the resource (ARM or Graph).
func stubPIM(t *testing.T,
	token func(configDir, tenantID, resource string) (string, error),
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
func isElig(url string) bool     { return strings.Contains(url, "roleEligibilityScheduleInstances") }
func isActive(url string) bool   { return strings.Contains(url, "roleAssignmentScheduleInstances") }

// The subscriptions are enumerated with the tenant token (not az account list),
// and each is queried with that same token via asTarget().
func TestListEligibleQueriesWithTenantToken(t *testing.T) {
	var (
		mu       sync.Mutex
		gotToken string
		queried  int
	)
	stubPIM(t,
		func(_, tenantID, _ string) (string, error) { return "tok-" + tenantID, nil },
		func(_, url, token string) ([]byte, error) {
			mu.Lock()
			gotToken = token
			if isElig(url) {
				queried++
			}
			mu.Unlock()
			switch {
			case isSubsList(url):
				return subsBody("s1", "s2"), nil
			case isActive(url):
				return []byte(`{"value":[]}`), nil
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
		func(_, _, _ string) (string, error) { return "tok", nil },
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
		func(_, _, _ string) (string, error) { return "", want },
		func(_, _, _ string) ([]byte, error) { t.Fatal("queried despite token failure"); return nil, nil },
	)
	if _, err := ListEligible("/cfg", "TID"); !errors.Is(err, want) {
		t.Errorf("error = %v, wanted the token error", err)
	}
}

func TestListEligibleSubscriptionsError(t *testing.T) {
	want := errors.New("cannot list subscriptions")
	stubPIM(t,
		func(_, _, _ string) (string, error) { return "tok", nil },
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
		func(_, _, _ string) (string, error) { return "tok", nil },
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
		func(_, _, _ string) (string, error) { return "tok", nil },
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
		func(_, _, _ string) (string, error) { called = true; return "", nil },
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

// --- activation ---

// stubActivate swaps every seam Activate uses (token, get, put, clock, guid).
func stubActivate(t *testing.T,
	token func(configDir, tenantID, resource string) (string, error),
	get func(configDir, url, token string) ([]byte, error),
	put func(configDir, url, token, body string) ([]byte, error),
) {
	t.Helper()
	ot, og, op, on, ou := tenantToken, restGet, restPut, now, newGUID
	tenantToken, restGet, restPut = token, get, put
	now = func() time.Time { return time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC) }
	newGUID = func() string { return "req-guid" }
	t.Cleanup(func() { tenantToken, restGet, restPut, now, newGUID = ot, og, op, on, ou })
}

func policyBody(maxDur string) []byte {
	return []byte(`{"value":[{"properties":{"effectiveRules":[` +
		`{"id":"/x/Expiration_EndUser_Assignment","maximumDuration":"` + maxDur + `"}]}}]}`)
}

func sampleEligible() Eligible {
	return Eligible{
		RoleName: "Owner", ScopeName: "AOC root",
		ScopeID:          "/providers/Microsoft.Management/managementGroups/aocroot",
		RoleDefinitionID: "/subscriptions/s/providers/Microsoft.Authorization/roleDefinitions/abc",
		ScheduleID:       "/providers/Microsoft.Management/managementGroups/aocroot/providers/Microsoft.Authorization/roleEligibilitySchedules/xyz",
	}
}

// Activate issues a SelfActivate PUT with MY user id (not the group's), the
// linked schedule, the policy's max duration and the fixed justification.
func TestActivateBuildsSelfActivateRequest(t *testing.T) {
	var (
		putURL, putToken, putBody string
	)
	stubActivate(t,
		func(_, _, resource string) (string, error) {
			if strings.Contains(resource, "graph") {
				return "graph-tok", nil
			}
			return "arm-tok", nil
		},
		func(_, url, token string) ([]byte, error) {
			switch {
			case strings.Contains(url, "/v1.0/me"):
				if token != "graph-tok" {
					t.Errorf("/me queried with %q, wanted the graph token", token)
				}
				return []byte(`{"id":"my-id"}`), nil
			case strings.Contains(url, "roleManagementPolicyAssignments"):
				return policyBody("PT8H"), nil
			}
			return nil, fmt.Errorf("unexpected GET %q", url)
		},
		func(_, url, token, body string) ([]byte, error) {
			putURL, putToken, putBody = url, token, body
			return []byte(`{"properties":{"status":"Provisioned"}}`), nil
		},
	)

	summary, err := Activate("/cfg", "TID", sampleEligible())
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	for _, want := range []string{"Owner", "AOC root", "8h"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q missing %q", summary, want)
		}
	}
	if putToken != "arm-tok" {
		t.Errorf("PUT used token %q, wanted the ARM token", putToken)
	}
	for _, want := range []string{
		"/providers/Microsoft.Management/managementGroups/aocroot/providers/Microsoft.Authorization/roleAssignmentScheduleRequests/req-guid",
		"api-version=2020-10-01",
	} {
		if !strings.Contains(putURL, want) {
			t.Errorf("PUT url %q missing %q", putURL, want)
		}
	}
	var req struct {
		Properties struct {
			PrincipalID      string `json:"principalId"`
			RoleDefinitionID string `json:"roleDefinitionId"`
			RequestType      string `json:"requestType"`
			Linked           string `json:"linkedRoleEligibilityScheduleId"`
			Justification    string `json:"justification"`
			ScheduleInfo     struct {
				Expiration struct {
					Type     string `json:"type"`
					Duration string `json:"duration"`
				} `json:"expiration"`
			} `json:"scheduleInfo"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(putBody), &req); err != nil {
		t.Fatalf("PUT body is not valid JSON: %v\n%s", err, putBody)
	}
	p := req.Properties
	if p.PrincipalID != "my-id" {
		t.Errorf("principalId = %q, wanted my own id (not the group)", p.PrincipalID)
	}
	if p.RequestType != "SelfActivate" {
		t.Errorf("requestType = %q", p.RequestType)
	}
	if p.Linked != sampleEligible().ScheduleID {
		t.Errorf("linkedRoleEligibilityScheduleId = %q", p.Linked)
	}
	if p.Justification != "Activated via azsel" {
		t.Errorf("justification = %q", p.Justification)
	}
	if p.ScheduleInfo.Expiration.Type != "AfterDuration" || p.ScheduleInfo.Expiration.Duration != "PT8H" {
		t.Errorf("expiration = %+v, wanted AfterDuration PT8H", p.ScheduleInfo.Expiration)
	}
}

func TestActivateUsesPolicyMaxDuration(t *testing.T) {
	var dur string
	stubActivate(t,
		func(_, _, _ string) (string, error) { return "tok", nil },
		func(_, url, _ string) ([]byte, error) {
			if strings.Contains(url, "/v1.0/me") {
				return []byte(`{"id":"my-id"}`), nil
			}
			return policyBody("PT4H"), nil
		},
		func(_, _, _, body string) ([]byte, error) {
			var r struct {
				Properties struct {
					ScheduleInfo struct {
						Expiration struct{ Duration string } `json:"expiration"`
					} `json:"scheduleInfo"`
				} `json:"properties"`
			}
			_ = json.Unmarshal([]byte(body), &r)
			dur = r.Properties.ScheduleInfo.Expiration.Duration
			return []byte(`{}`), nil
		},
	)
	if _, err := Activate("/cfg", "TID", sampleEligible()); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if dur != "PT4H" {
		t.Errorf("duration = %q, wanted the policy max PT4H", dur)
	}
}

func TestActivateFallsBackTo8hWhenPolicyUnavailable(t *testing.T) {
	var dur string
	stubActivate(t,
		func(_, _, _ string) (string, error) { return "tok", nil },
		func(_, url, _ string) ([]byte, error) {
			if strings.Contains(url, "/v1.0/me") {
				return []byte(`{"id":"my-id"}`), nil
			}
			return nil, errors.New("policy read forbidden")
		},
		func(_, _, _, body string) ([]byte, error) {
			var r struct {
				Properties struct {
					ScheduleInfo struct {
						Expiration struct{ Duration string } `json:"expiration"`
					} `json:"scheduleInfo"`
				} `json:"properties"`
			}
			_ = json.Unmarshal([]byte(body), &r)
			dur = r.Properties.ScheduleInfo.Expiration.Duration
			return []byte(`{}`), nil
		},
	)
	if _, err := Activate("/cfg", "TID", sampleEligible()); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if dur != "PT8H" {
		t.Errorf("duration = %q, wanted the PT8H fallback", dur)
	}
}

func TestActivatePropagatesPutError(t *testing.T) {
	want := errors.New("RoleAssignmentRequestPolicyValidationFailed: MFA required")
	stubActivate(t,
		func(_, _, _ string) (string, error) { return "tok", nil },
		func(_, url, _ string) ([]byte, error) {
			if strings.Contains(url, "/v1.0/me") {
				return []byte(`{"id":"my-id"}`), nil
			}
			return policyBody("PT8H"), nil
		},
		func(_, _, _, _ string) ([]byte, error) { return nil, want },
	)
	if _, err := Activate("/cfg", "TID", sampleEligible()); !errors.Is(err, want) {
		t.Errorf("error = %v, wanted the PUT error surfaced", err)
	}
}

func TestActivateMissingIdentifiers(t *testing.T) {
	called := false
	stubActivate(t,
		func(_, _, _ string) (string, error) { called = true; return "tok", nil },
		func(_, _, _ string) ([]byte, error) { return nil, nil },
		func(_, _, _, _ string) ([]byte, error) { return nil, nil },
	)
	e := sampleEligible()
	e.ScheduleID = ""
	if _, err := Activate("/cfg", "TID", e); err == nil || !strings.Contains(err.Error(), "identifiers") {
		t.Errorf("error = %v, wanted a missing-identifiers error", err)
	}
	if called {
		t.Error("acquired a token despite missing identifiers")
	}
}

// An eligibility whose role+scope has a currently-"Activated" assignment is
// marked ActiveUntil; a permanent "Assigned" one, or a non-matching role, is not.
func TestListEligibleMarksActivated(t *testing.T) {
	const eligRows = `{"value":[
      {"name":"a","properties":{"status":"Provisioned","roleDefinitionId":"rd-active","scope":"/scope/x",
        "expandedProperties":{"scope":{"id":"/scope/x","displayName":"X","type":"subscription"},"roleDefinition":{"displayName":"Owner"},"principal":{"displayName":"Me","type":"User"}}}},
      {"name":"b","properties":{"status":"Provisioned","roleDefinitionId":"rd-idle","scope":"/scope/y",
        "expandedProperties":{"scope":{"id":"/scope/y","displayName":"Y","type":"subscription"},"roleDefinition":{"displayName":"Reader"},"principal":{"displayName":"Me","type":"User"}}}}]}`
	const activeRows = `{"value":[
      {"properties":{"roleDefinitionId":"rd-active","scope":"/scope/x","assignmentType":"Activated","endDateTime":"2027-01-02T03:04:05Z"}},
      {"properties":{"roleDefinitionId":"rd-idle","scope":"/scope/y","assignmentType":"Assigned","endDateTime":null}}]}`

	stubPIM(t,
		func(_, _, _ string) (string, error) { return "tok", nil },
		func(_, url, _ string) ([]byte, error) {
			switch {
			case isSubsList(url):
				return subsBody("s1"), nil
			case isActive(url):
				return []byte(activeRows), nil
			case isElig(url):
				return []byte(eligRows), nil
			}
			return []byte(`{"value":[]}`), nil
		},
	)
	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, wanted 2", len(got))
	}
	var owner, reader Eligible
	for _, e := range got {
		switch e.RoleName {
		case "Owner":
			owner = e
		case "Reader":
			reader = e
		}
	}
	if owner.ActiveUntil == nil || !owner.ActiveUntil.Equal(time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("Owner ActiveUntil = %v, wanted 2027-01-02T03:04:05Z (it is activated)", owner.ActiveUntil)
	}
	if reader.ActiveUntil != nil {
		t.Errorf("Reader ActiveUntil = %v, wanted nil (permanent Assigned, not activated)", reader.ActiveUntil)
	}
}

// Regression (the real consorci-aoc shape): a management-group eligibility is
// discovered through a subscription, so its roleDefinitionId is subscription-
// prefixed and its scope is the MG. The "Activated" instance exists only at the
// MG scope — a subscription query reports it merely as inherited "Assigned" —
// and carries the tenant-level roleDefinitionId. It must still be matched, which
// needs both the MG-scope active query and GUID-based keying.
func TestListEligibleMarksActivatedAtManagementGroup(t *testing.T) {
	const mgScope = "/providers/Microsoft.Management/managementGroups/aocroot"
	// Eligibility: MG scope, roleDefinitionId prefixed by the subscription.
	eligRows := `{"value":[{"name":"kv","properties":{"status":"Provisioned",` +
		`"roleDefinitionId":"/subscriptions/sub1/providers/Microsoft.Authorization/roleDefinitions/00482a5a",` +
		`"scope":"` + mgScope + `",` +
		`"expandedProperties":{"scope":{"id":"` + mgScope + `","displayName":"AOC root","type":"managementgroup"},` +
		`"roleDefinition":{"displayName":"Key Vault Administrator"},"principal":{"displayName":"Team","type":"Group"}}}}]}`
	// A subscription active query returns the MG role only as inherited "Assigned".
	const subActive = `{"value":[{"properties":{"roleDefinitionId":"/providers/Microsoft.Authorization/roleDefinitions/00482a5a",` +
		`"scope":"` + mgScope + `","assignmentType":"Assigned","endDateTime":null}}]}`
	// The MG-scope active query returns the temporary "Activated" instance, with
	// the tenant-level roleDefinitionId (no subscription prefix).
	const mgActive = `{"value":[{"properties":{"roleDefinitionId":"/providers/Microsoft.Authorization/roleDefinitions/00482a5a",` +
		`"scope":"` + mgScope + `","assignmentType":"Activated","endDateTime":"2026-11-24T17:31:24Z"}}]}`

	stubPIM(t,
		func(_, _, _ string) (string, error) { return "tok", nil },
		func(_, url, _ string) ([]byte, error) {
			switch {
			case isSubsList(url):
				return subsBody("sub1"), nil
			case isActive(url) && strings.Contains(url, "managementGroups/aocroot"):
				return []byte(mgActive), nil
			case isActive(url):
				return []byte(subActive), nil
			case isElig(url):
				return []byte(eligRows), nil
			}
			return []byte(`{"value":[]}`), nil
		},
	)
	got, err := ListEligible("/cfg", "TID")
	if err != nil {
		t.Fatalf("ListEligible: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, wanted 1: %+v", len(got), got)
	}
	if got[0].ActiveUntil == nil || !got[0].ActiveUntil.Equal(time.Date(2026, 11, 24, 17, 31, 24, 0, time.UTC)) {
		t.Errorf("ActiveUntil = %v, wanted 2026-11-24T17:31:24Z (activated at the MG, GUID-matched)", got[0].ActiveUntil)
	}
}

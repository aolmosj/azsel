// Package pim reads and activates a user's eligible Azure resource-role PIM
// assignments (Azure RBAC — Microsoft.Authorization) through the Azure CLI, so
// azsel can show, per tenant, the roles the user could activate — and activate
// one — without the portal.
package pim

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aolmosj/azsel/internal/azure"
)

const (
	armResource   = "https://management.azure.com"
	graphResource = "https://graph.microsoft.com"
)

// Eligible is one eligible resource-role assignment for the current user: a role
// they may activate, at a scope, until some time (or permanently).
type Eligible struct {
	RoleName  string
	ScopeName string
	ScopeType string
	ScopeID   string
	Status    string
	End       *time.Time // nil = permanent eligibility (no end date)

	// ActiveUntil is set when this eligibility is currently activated (a PIM
	// "Activated" assignment exists for the same role and scope) and names when
	// that activation expires; nil when the role is not currently active.
	ActiveUntil *time.Time

	// The principal the eligibility is assigned to. When PrincipalType is
	// "Group" the eligibility is inherited through membership of that group
	// (PrincipalName), which is what the user activates through; otherwise it is
	// assigned to the user directly.
	PrincipalType string
	PrincipalName string

	// Identifiers needed to activate: the role definition, the linked eligibility
	// schedule, and the authoritative scope for the activation request.
	RoleDefinitionID string
	ScheduleID       string
}

// ViaGroup reports the group the eligibility comes through, or "" when it is
// assigned to the user directly.
func (e Eligible) ViaGroup() string {
	if strings.EqualFold(e.PrincipalType, "Group") {
		return e.PrincipalName
	}
	return ""
}

// Seams onto the Azure CLI (and the clock / id generator), package variables so
// tests can run without az.
var (
	restGet     = azure.RestGET
	restPut     = azure.RestPUT
	tenantToken = azure.TenantToken
	now         = time.Now
	newGUID     = randomGUID
)

// scopeQueryConcurrency bounds how many `az rest` processes run at once.
const scopeQueryConcurrency = 4

// subscriptionsURL lists the subscriptions the caller can see. Queried with the
// tenant token, it returns exactly the subscriptions in that tenant — the
// authoritative set. `az account list` is not used: it reports a misleading
// tenantId for guest/cross-tenant subscriptions (a consorci subscription can
// show up under another tenant's id), which would wrongly exclude them.
const subscriptionsURL = "https://management.azure.com/subscriptions?api-version=2020-01-01"

// eligibleURL is the eligibility query for the current user (asTarget) at one
// scope. Querying a subscription returns eligibilities at that subscription and
// inherited from its ancestor management groups, so subscription queries surface
// management-group-level eligibilities too (deduplicated by instance id).
func eligibleURL(scope string) string {
	return "https://management.azure.com" + scope +
		"/providers/Microsoft.Authorization/roleEligibilityScheduleInstances" +
		"?api-version=2020-10-01&$filter=asTarget()"
}

// ListEligible returns the eligible resource roles for the tenant whose az
// config lives in configDir.
//
// A profile can hold subscriptions from several tenants (a guest identity), and
// its active subscription may belong to a different one — so this acquires a
// token for the profile's own tenant and queries only the subscriptions in that
// tenant with it. Without that, az rest would use the active (possibly
// wrong-tenant) token, which the PIM API rejects with a misleading
// AadPremiumLicenseRequired.
//
// Known gaps: eligibilities scoped only to a resource group are not returned (a
// subscription query does not descend into its resource groups), nor are ones on
// a subscription in this tenant that the caller cannot otherwise see.
func ListEligible(configDir, tenantID string) ([]Eligible, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("tenant has no tenant ID configured; cannot query PIM")
	}

	token, err := tenantToken(configDir, tenantID, armResource)
	if err != nil {
		return nil, err
	}
	scopes, err := tenantSubscriptions(configDir, token)
	if err != nil {
		return nil, err
	}
	if len(scopes) == 0 {
		return nil, nil
	}

	// Phase 1: eligibilities across the subscription scopes (which also surface
	// management-group eligibilities each subscription inherits).
	byName, anyOK, lastErr := fetchEligibleScopes(configDir, scopes, token)
	if !anyOK {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, nil
	}

	// Phase 2: which of those eligibilities are currently activated. The active
	// query must run at the exact scope each eligibility lives at — management
	// groups included: a subscription query reports a management-group
	// activation only as an inherited "Assigned", hiding the temporary
	// "Activated" instance, which is returned solely at the MG scope itself.
	activeBy := fetchActiveScopes(configDir, distinctScopes(byName), token)

	return sortEligibles(byName, activeBy), nil
}

// fetchEligibleScopes queries eligibilities at each scope concurrently and
// deduplicates by instance id. anyOK reports whether at least one scope
// answered; lastErr carries a failure only relevant when none did.
func fetchEligibleScopes(configDir string, scopes []string, token string) (map[string]eligibleInstance, bool, error) {
	var (
		mu      sync.Mutex
		byName  = make(map[string]eligibleInstance)
		anyOK   bool
		lastErr error
	)
	sem := make(chan struct{}, scopeQueryConcurrency)
	var wg sync.WaitGroup
	for _, scope := range scopes {
		wg.Add(1)
		sem <- struct{}{}
		go func(scope string) {
			defer wg.Done()
			defer func() { <-sem }()
			insts, err := fetchInstances(configDir, eligibleURL(scope), token)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				lastErr = err
				return
			}
			anyOK = true
			for _, it := range insts {
				if it.Name != "" {
					byName[it.Name] = it
				}
			}
		}(scope)
	}
	wg.Wait()
	return byName, anyOK, lastErr
}

// fetchActiveScopes gathers, across the given scopes concurrently, the caller's
// currently-activated assignments keyed by role+scope. Best-effort throughout:
// a scope that fails contributes nothing and never breaks the listing.
func fetchActiveScopes(configDir string, scopes []string, token string) map[string]time.Time {
	var (
		mu       sync.Mutex
		activeBy = make(map[string]time.Time)
	)
	sem := make(chan struct{}, scopeQueryConcurrency)
	var wg sync.WaitGroup
	for _, scope := range scopes {
		wg.Add(1)
		sem <- struct{}{}
		go func(scope string) {
			defer wg.Done()
			defer func() { <-sem }()
			active := fetchActive(configDir, scope, token)
			mu.Lock()
			defer mu.Unlock()
			for k, until := range active {
				activeBy[k] = until
			}
		}(scope)
	}
	wg.Wait()
	return activeBy
}

// distinctScopes is the set of scopes the eligibilities live at (subscriptions
// and management groups) — exactly where an activation of any of them is found.
func distinctScopes(byName map[string]eligibleInstance) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, it := range byName {
		s := scopeOf(it)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// scopeOf is an eligibility's authoritative scope id: properties.scope, falling
// back to the expanded scope id used for display.
func scopeOf(it eligibleInstance) string {
	if s := it.Properties.Scope; s != "" {
		return s
	}
	return it.Properties.ExpandedProperties.Scope.ID
}

// activeKey correlates an active assignment with an eligibility by role and
// scope. It keys on the role-definition GUID (the trailing path segment), not
// the full id: the same role carries a different prefix on an eligibility (often
// scoped to the subscription it was discovered through, e.g.
// /subscriptions/<id>/.../roleDefinitions/<guid>) than on the activation
// (tenant-level, /providers/.../roleDefinitions/<guid>), while the GUID is
// stable across both.
func activeKey(roleDefinitionID, scope string) string {
	return roleDefGUID(roleDefinitionID) + "\x00" + scope
}

// roleDefGUID is the trailing segment of a role-definition id.
func roleDefGUID(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// eligibleResponse mirrors the ARM PIM payload; name is the instance GUID,
// unique per eligibility. nextLink drives ARM pagination.
type eligibleResponse struct {
	Value    []eligibleInstance `json:"value"`
	NextLink string             `json:"nextLink"`
}

type eligibleInstance struct {
	Name       string `json:"name"`
	Properties struct {
		Status                    string  `json:"status"`
		EndDateTime               *string `json:"endDateTime"`
		Scope                     string  `json:"scope"`
		RoleDefinitionID          string  `json:"roleDefinitionId"`
		RoleEligibilityScheduleID string  `json:"roleEligibilityScheduleId"`
		ExpandedProperties        struct {
			Scope struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
				Type        string `json:"type"`
			} `json:"scope"`
			RoleDefinition struct {
				DisplayName string `json:"displayName"`
			} `json:"roleDefinition"`
			Principal struct {
				DisplayName string `json:"displayName"`
				Type        string `json:"type"`
			} `json:"principal"`
		} `json:"expandedProperties"`
	} `json:"properties"`
}

// tenantSubscriptions lists the subscription scopes visible with the tenant
// token (i.e. the subscriptions in that tenant), following ARM pagination.
func tenantSubscriptions(configDir, token string) ([]string, error) {
	var scopes []string
	url := subscriptionsURL
	for url != "" {
		body, err := restGet(configDir, url, token)
		if err != nil {
			return nil, err
		}
		var resp struct {
			Value []struct {
				SubscriptionID string `json:"subscriptionId"`
			} `json:"value"`
			NextLink string `json:"nextLink"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parsing subscriptions: %w", err)
		}
		for _, s := range resp.Value {
			if s.SubscriptionID != "" {
				scopes = append(scopes, "/subscriptions/"+s.SubscriptionID)
			}
		}
		url = resp.NextLink
	}
	return scopes, nil
}

// activeURL queries the caller's active role-assignment instances at one scope.
func activeURL(scope string) string {
	return "https://management.azure.com" + scope +
		"/providers/Microsoft.Authorization/roleAssignmentScheduleInstances" +
		"?api-version=2020-10-01&$filter=asTarget()"
}

type activeResponse struct {
	Value []struct {
		Properties struct {
			RoleDefinitionID string  `json:"roleDefinitionId"`
			Scope            string  `json:"scope"`
			AssignmentType   string  `json:"assignmentType"`
			EndDateTime      *string `json:"endDateTime"`
		} `json:"properties"`
	} `json:"value"`
	NextLink string `json:"nextLink"`
}

// fetchActive returns, keyed by role+scope, the expiry of the caller's currently
// *activated* PIM assignments at the scope — only assignmentType "Activated"
// (temporary activations), not permanent "Assigned" access. Best-effort: any
// error yields an empty map so the eligibility listing still works.
func fetchActive(configDir, scope, token string) map[string]time.Time {
	out := make(map[string]time.Time)
	url := activeURL(scope)
	for url != "" {
		body, err := restGet(configDir, url, token)
		if err != nil {
			return out
		}
		var resp activeResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return out
		}
		for _, a := range resp.Value {
			p := a.Properties
			if !strings.EqualFold(p.AssignmentType, "Activated") || p.EndDateTime == nil {
				continue
			}
			if t, err := time.Parse(time.RFC3339, *p.EndDateTime); err == nil {
				out[activeKey(p.RoleDefinitionID, p.Scope)] = t
			}
		}
		url = resp.NextLink
	}
	return out
}

// fetchInstances follows ARM's nextLink pagination for one scope's query.
func fetchInstances(configDir, url, token string) ([]eligibleInstance, error) {
	var out []eligibleInstance
	for url != "" {
		body, err := restGet(configDir, url, token)
		if err != nil {
			return nil, err
		}
		var resp eligibleResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parsing PIM response: %w", err)
		}
		out = append(out, resp.Value...)
		url = resp.NextLink
	}
	return out, nil
}

// sortEligibles maps deduplicated instances to Eligible, marks the ones that are
// currently activated (from activeBy, keyed by role+scope), and sorts stably.
func sortEligibles(byName map[string]eligibleInstance, activeBy map[string]time.Time) []Eligible {
	out := make([]Eligible, 0, len(byName))
	for _, it := range byName {
		p := it.Properties
		// The authoritative scope id, used both to match activations and as the
		// target of an activation request.
		scopeID := scopeOf(it)
		e := Eligible{
			RoleName:         p.ExpandedProperties.RoleDefinition.DisplayName,
			ScopeName:        p.ExpandedProperties.Scope.DisplayName,
			ScopeType:        p.ExpandedProperties.Scope.Type,
			ScopeID:          scopeID,
			Status:           p.Status,
			PrincipalType:    p.ExpandedProperties.Principal.Type,
			PrincipalName:    p.ExpandedProperties.Principal.DisplayName,
			RoleDefinitionID: p.RoleDefinitionID,
			ScheduleID:       p.RoleEligibilityScheduleID,
		}
		// A missing or unparseable end date leaves End nil (treated as
		// permanent) rather than failing the whole listing.
		if p.EndDateTime != nil {
			if t, err := time.Parse(time.RFC3339, *p.EndDateTime); err == nil {
				e.End = &t
			}
		}
		if until, ok := activeBy[activeKey(e.RoleDefinitionID, e.ScopeID)]; ok {
			u := until
			e.ActiveUntil = &u
		}
		out = append(out, e)
	}

	slices.SortFunc(out, func(a, b Eligible) int {
		if c := strings.Compare(a.ScopeType, b.ScopeType); c != 0 {
			return c
		}
		if c := strings.Compare(a.RoleName, b.RoleName); c != 0 {
			return c
		}
		if c := strings.Compare(a.ScopeName, b.ScopeName); c != 0 {
			return c
		}
		return strings.Compare(a.PrincipalName, b.PrincipalName)
	})
	return out
}

// defaultDuration is used when the policy's maximum activation duration cannot
// be read — a conservative 8 hours.
const defaultDuration = "PT8H"

// Activate self-activates one eligible role for the maximum duration the policy
// allows (falling back to 8 hours) with a fixed justification. It returns a
// short human summary on success. A policy that requires MFA or a ticket at
// activation makes the underlying PUT fail; that ARM error is returned as-is.
func Activate(configDir, tenantID string, e Eligible) (string, error) {
	if strings.TrimSpace(tenantID) == "" {
		return "", fmt.Errorf("tenant has no tenant ID configured; cannot activate PIM")
	}
	if e.ScopeID == "" || e.RoleDefinitionID == "" || e.ScheduleID == "" {
		return "", fmt.Errorf("this eligibility is missing the identifiers needed to activate")
	}

	armToken, err := tenantToken(configDir, tenantID, armResource)
	if err != nil {
		return "", err
	}
	principalID, err := signedInUserID(configDir, tenantID)
	if err != nil {
		return "", err
	}
	duration := policyMaxDuration(configDir, e.ScopeID, e.RoleDefinitionID, armToken)

	body, err := json.Marshal(map[string]any{
		"properties": map[string]any{
			"principalId":                     principalID,
			"roleDefinitionId":                e.RoleDefinitionID,
			"requestType":                     "SelfActivate",
			"linkedRoleEligibilityScheduleId": e.ScheduleID,
			"scheduleInfo": map[string]any{
				"startDateTime": now().UTC().Format(time.RFC3339),
				"expiration": map[string]any{
					"type":     "AfterDuration",
					"duration": duration,
				},
			},
			"justification": "Activated via azsel",
		},
	})
	if err != nil {
		return "", fmt.Errorf("building activation request: %w", err)
	}

	url := armResource + e.ScopeID +
		"/providers/Microsoft.Authorization/roleAssignmentScheduleRequests/" +
		newGUID() + "?api-version=2020-10-01"
	if _, err := restPut(configDir, url, armToken, string(body)); err != nil {
		return "", err
	}

	role := e.RoleName
	if role == "" {
		role = "the role"
	}
	return fmt.Sprintf("Activated %s at %s for %s.", role, e.ScopeName, humanDuration(duration)), nil
}

// signedInUserID returns the caller's object id in the tenant — the principalId
// for a self-activation — via Microsoft Graph /me with a tenant Graph token. A
// guest's id differs per tenant, and the eligibility's own principalId is the
// group for group-inherited roles, not the user, so it cannot be reused here.
func signedInUserID(configDir, tenantID string) (string, error) {
	graphToken, err := tenantToken(configDir, tenantID, graphResource)
	if err != nil {
		return "", err
	}
	body, err := restGet(configDir, graphResource+"/v1.0/me?$select=id", graphToken)
	if err != nil {
		return "", err
	}
	var me struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &me); err != nil || me.ID == "" {
		return "", fmt.Errorf("could not determine the signed-in user id for the activation")
	}
	return me.ID, nil
}

// policyMaxDuration reads the maximum activation duration the PIM policy allows
// for the role at the scope, falling back to defaultDuration when it cannot be
// read.
func policyMaxDuration(configDir, scope, roleDefinitionID, token string) string {
	url := armResource + scope +
		"/providers/Microsoft.Authorization/roleManagementPolicyAssignments" +
		"?api-version=2020-10-01&$filter=roleDefinitionId+eq+'" + roleDefinitionID + "'"
	body, err := restGet(configDir, url, token)
	if err != nil {
		return defaultDuration
	}
	var resp struct {
		Value []struct {
			Properties struct {
				EffectiveRules []struct {
					ID              string `json:"id"`
					MaximumDuration string `json:"maximumDuration"`
				} `json:"effectiveRules"`
			} `json:"properties"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return defaultDuration
	}
	for _, a := range resp.Value {
		for _, r := range a.Properties.EffectiveRules {
			if strings.Contains(r.ID, "Expiration_EndUser_Assignment") && r.MaximumDuration != "" {
				return r.MaximumDuration
			}
		}
	}
	return defaultDuration
}

// humanDuration turns an ISO-8601 duration like "PT8H" into "8h" for a message,
// falling back to the raw value for shapes it does not recognise.
func humanDuration(d string) string {
	if s := strings.TrimPrefix(d, "PT"); s != d && s != "" {
		return strings.ToLower(s)
	}
	return d
}

// randomGUID returns a random v4 UUID for a roleAssignmentScheduleRequest name.
func randomGUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

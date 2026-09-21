// Package pim reads a user's eligible Azure resource-role PIM assignments
// (Azure RBAC — Microsoft.Authorization) through the Azure CLI, so azsel can
// show, per tenant, the roles the user could activate without the portal.
package pim

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aolmosj/azsel/internal/azure"
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

	// The principal the eligibility is assigned to. When PrincipalType is
	// "Group" the eligibility is inherited through membership of that group
	// (PrincipalName), which is what the user activates through; otherwise it is
	// assigned to the user directly.
	PrincipalType string
	PrincipalName string
}

// ViaGroup reports the group the eligibility comes through, or "" when it is
// assigned to the user directly.
func (e Eligible) ViaGroup() string {
	if strings.EqualFold(e.PrincipalType, "Group") {
		return e.PrincipalName
	}
	return ""
}

// restGet is the seam onto the Azure CLI, a package variable so tests can feed
// canned JSON without az or a network (mirrors azure.run).
var restGet = azure.RestGET

const (
	// subscriptionsURL lists the subscriptions the caller can see (Strategy 2).
	subscriptionsURL = "https://management.azure.com/subscriptions?api-version=2020-01-01"

	// meURL and myGroupsURL identify the caller for Strategy 1's client-side
	// filter: their own object id and the groups they belong to (an eligibility
	// can be assigned to a group the user is a member of).
	meURL       = "https://graph.microsoft.com/v1.0/me?$select=id"
	myGroupsURL = "https://graph.microsoft.com/v1.0/me/transitiveMemberOf?$select=id"

	// scopeQueryConcurrency bounds Strategy 2's `az rest` fan-out. Kept low to
	// avoid tripping PIM's per-identity throttling on tenants with many
	// subscriptions.
	scopeQueryConcurrency = 4
)

// errFallback signals that Strategy 1 cannot run (no tenant id, no Graph
// identity, or the scope is not readable) and Strategy 2 should be tried.
var errFallback = errors.New("root-management-group listing unavailable")

// eligibleURL is the eligibility query for one scope and filter.
func eligibleURL(scope, filter string) string {
	return "https://management.azure.com" + scope +
		"/providers/Microsoft.Authorization/roleEligibilityScheduleInstances" +
		"?api-version=2020-10-01&$filter=" + filter
}

func rootMGScope(tenantID string) string {
	return "/providers/Microsoft.Management/managementGroups/" + tenantID
}

// ListEligible returns the eligible resource roles for the tenant whose az
// config lives in configDir.
//
// Strategy 1 makes a single atScopeAndBelow() query at the tenant root
// management group and filters it to the caller and their groups — a handful of
// calls, which avoids PIM's throttling on tenants with many subscriptions.
// When that is not possible (no tenant id, no Graph identity, or the scope is
// not readable) it falls back to Strategy 2, one asTarget() query per
// subscription.
func ListEligible(configDir, tenantID string) ([]Eligible, error) {
	rows, err := listViaRootMG(configDir, tenantID)
	if err == nil {
		return rows, nil
	}
	if !errors.Is(err, errFallback) {
		// A definitive failure (throttling above all) — do not fall back, which
		// would only fan out more calls into the same throttle.
		return nil, err
	}
	return listViaSubscriptions(configDir, tenantID)
}

// listViaRootMG is Strategy 1.
func listViaRootMG(configDir, tenantID string) ([]Eligible, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, errFallback
	}

	// Identify the caller. Graph lives on a different endpoint than ARM PIM, so
	// these usually succeed even when PIM is throttling; a genuine failure here
	// (e.g. a service-principal profile with no /me) just means fall back.
	meBody, err := restGet(configDir, meURL)
	if err != nil {
		if isThrottle(err) {
			return nil, throttleError(err)
		}
		return nil, errFallback
	}
	var me struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(meBody, &me); err != nil || me.ID == "" {
		return nil, errFallback
	}

	groups, err := fetchIDs(configDir, myGroupsURL)
	if err != nil {
		if isThrottle(err) {
			return nil, throttleError(err)
		}
		return nil, errFallback
	}
	principals := map[string]bool{me.ID: true}
	for _, g := range groups {
		principals[g] = true
	}

	instances, err := fetchInstances(configDir, eligibleURL(rootMGScope(tenantID), "atScopeAndBelow()"))
	if err != nil {
		if isThrottle(err) {
			return nil, throttleError(err)
		}
		// Not authorized to read at the root MG, or any other ARM error: fall
		// back to the per-subscription path.
		return nil, errFallback
	}

	// atScopeAndBelow() returns every principal's eligibility below the scope;
	// keep only those assigned to the caller or a group they belong to.
	byName := make(map[string]eligibleInstance)
	for _, it := range instances {
		if it.Name != "" && principals[it.Properties.ExpandedProperties.Principal.ID] {
			byName[it.Name] = it
		}
	}
	return sortEligibles(byName), nil
}

// listViaSubscriptions is Strategy 2: one asTarget() query per visible
// subscription plus the tenant root management group, deduplicated by instance
// id. Correct but call-heavy, so it can be throttled on large tenants.
func listViaSubscriptions(configDir, tenantID string) ([]Eligible, error) {
	body, err := restGet(configDir, subscriptionsURL)
	if err != nil {
		if isThrottle(err) {
			return nil, throttleError(err)
		}
		return nil, err
	}
	var subs struct {
		Value []struct {
			SubscriptionID string `json:"subscriptionId"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &subs); err != nil {
		return nil, fmt.Errorf("parsing subscriptions: %w", err)
	}

	scopes := make([]string, 0, len(subs.Value)+1)
	for _, s := range subs.Value {
		if s.SubscriptionID != "" {
			scopes = append(scopes, "/subscriptions/"+s.SubscriptionID)
		}
	}
	if tid := strings.TrimSpace(tenantID); tid != "" {
		scopes = append(scopes, rootMGScope(tid))
	}
	if len(scopes) == 0 {
		return nil, nil
	}

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

			b, err := restGet(configDir, eligibleURL(scope, "asTarget()"))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				lastErr = err
				return
			}
			var resp eligibleResponse
			if err := json.Unmarshal(b, &resp); err != nil {
				lastErr = err
				return
			}
			anyOK = true
			for _, it := range resp.Value {
				if it.Name != "" {
					byName[it.Name] = it
				}
			}
		}(scope)
	}
	wg.Wait()

	if !anyOK {
		if isThrottle(lastErr) {
			return nil, throttleError(lastErr)
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, nil
	}
	return sortEligibles(byName), nil
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
		Status             string  `json:"status"`
		EndDateTime        *string `json:"endDateTime"`
		ExpandedProperties struct {
			Scope struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
				Type        string `json:"type"`
			} `json:"scope"`
			RoleDefinition struct {
				DisplayName string `json:"displayName"`
			} `json:"roleDefinition"`
			Principal struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
				Type        string `json:"type"`
			} `json:"principal"`
		} `json:"expandedProperties"`
	} `json:"properties"`
}

// fetchInstances follows ARM's nextLink pagination for eligibility queries.
func fetchInstances(configDir, url string) ([]eligibleInstance, error) {
	var out []eligibleInstance
	for url != "" {
		body, err := restGet(configDir, url)
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

// fetchIDs collects object ids from a Graph collection, following Graph's
// @odata.nextLink pagination.
func fetchIDs(configDir, url string) ([]string, error) {
	var ids []string
	for url != "" {
		body, err := restGet(configDir, url)
		if err != nil {
			return nil, err
		}
		var resp struct {
			Value []struct {
				ID string `json:"id"`
			} `json:"value"`
			NextLink string `json:"@odata.nextLink"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parsing Graph response: %w", err)
		}
		for _, v := range resp.Value {
			if v.ID != "" {
				ids = append(ids, v.ID)
			}
		}
		url = resp.NextLink
	}
	return ids, nil
}

// sortEligibles maps deduplicated instances to Eligible and sorts them stably.
func sortEligibles(byName map[string]eligibleInstance) []Eligible {
	out := make([]Eligible, 0, len(byName))
	for _, it := range byName {
		p := it.Properties
		e := Eligible{
			RoleName:      p.ExpandedProperties.RoleDefinition.DisplayName,
			ScopeName:     p.ExpandedProperties.Scope.DisplayName,
			ScopeType:     p.ExpandedProperties.Scope.Type,
			ScopeID:       p.ExpandedProperties.Scope.ID,
			Status:        p.Status,
			PrincipalType: p.ExpandedProperties.Principal.Type,
			PrincipalName: p.ExpandedProperties.Principal.DisplayName,
		}
		// A missing or unparseable end date leaves End nil (treated as
		// permanent) rather than failing the whole listing.
		if p.EndDateTime != nil {
			if t, err := time.Parse(time.RFC3339, *p.EndDateTime); err == nil {
				e.End = &t
			}
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

// isThrottle reports whether err looks like PIM rate-limiting. The PIM endpoint
// misleadingly returns AadPremiumLicenseRequired when throttling a caller whose
// tenant does have the license, alongside the usual 429/TooManyRequests.
func isThrottle(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "AadPremiumLicenseRequired") ||
		strings.Contains(s, "TooManyRequests") ||
		strings.Contains(s, "429")
}

func throttleError(err error) error {
	return fmt.Errorf("Azure PIM rate-limited the request. It returned "+
		"AadPremiumLicenseRequired, which on a tenant that has Entra ID P2 or "+
		"Governance means throttling, not a missing license — wait a minute and "+
		"try again.\nunderlying: %w", err)
}

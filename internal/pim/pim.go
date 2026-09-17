// Package pim reads a user's eligible Azure resource-role PIM assignments
// (Azure RBAC — Microsoft.Authorization) through the Azure CLI, so azsel can
// show, per tenant, the roles the user could activate without the portal.
package pim

import (
	"encoding/json"
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
	// subscriptionsURL lists the subscriptions the caller can see. ARM's live
	// list is used rather than `az account list`, which reads the local profile
	// cache and can be a strict subset.
	subscriptionsURL = "https://management.azure.com/subscriptions?api-version=2020-01-01"

	// eligibleURLTmpl queries the eligible role instances for the current user at
	// one scope. A query at a scope returns eligibilities at that scope and
	// inherited from its ancestors (management groups, tenant), so querying every
	// subscription also surfaces management-group-level eligibilities. asTarget()
	// restricts the result to the caller and the groups they belong to.
	eligibleURLTmpl = "https://management.azure.com%s/providers/Microsoft.Authorization/roleEligibilityScheduleInstances?api-version=2020-10-01&$filter=asTarget()"

	// rootMGScopeTmpl is the tenant root management group, a safety net for
	// eligibilities scoped there directly (its id is the tenant GUID).
	rootMGScopeTmpl = "/providers/Microsoft.Management/managementGroups/%s"

	// scopeQueryConcurrency bounds how many `az rest` processes run at once.
	scopeQueryConcurrency = 8
)

// subscriptionsResponse is the ARM subscriptions list, kept to the id.
type subscriptionsResponse struct {
	Value []struct {
		SubscriptionID string `json:"subscriptionId"`
	} `json:"value"`
}

// eligibleResponse mirrors the ARM PIM payload, keeping only the fields azsel
// shows. name is the instance GUID, unique per eligibility, used to deduplicate
// the same management-group eligibility seen through several subscriptions.
type eligibleResponse struct {
	Value []eligibleInstance `json:"value"`
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
				DisplayName string `json:"displayName"`
				Type        string `json:"type"`
			} `json:"principal"`
		} `json:"expandedProperties"`
	} `json:"properties"`
}

// ListEligible returns the eligible resource roles for the tenant whose az
// config lives in configDir. It enumerates the visible subscriptions, adds the
// tenant root management group (when tenantID is known) as a safety net, and
// queries eligibility at every scope concurrently, deduplicating by instance id.
//
// Known gaps: eligibilities scoped only to a resource group are not returned (a
// subscription query does not descend into its resource groups), nor are ones on
// a subscription the caller cannot otherwise see.
func ListEligible(configDir, tenantID string) ([]Eligible, error) {
	// The subscriptions call is also the auth gate: if the tenant is not logged
	// in, it fails here with az's own message.
	body, err := restGet(configDir, subscriptionsURL)
	if err != nil {
		return nil, err
	}
	var subs subscriptionsResponse
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
		scopes = append(scopes, fmt.Sprintf(rootMGScopeTmpl, tid))
	}
	if len(scopes) == 0 {
		return nil, nil
	}

	// Query scopes concurrently. A per-scope failure (a 403 on one subscription)
	// is recorded but not fatal: it must not hide the eligibilities from the
	// scopes that did answer. Only if every scope fails is the error surfaced.
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

			b, err := restGet(configDir, fmt.Sprintf(eligibleURLTmpl, scope))
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
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, nil
	}

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

	// Stable order — by scope type, then role, then scope — for deterministic
	// output and tests.
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
	return out, nil
}

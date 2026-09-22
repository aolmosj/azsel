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

// Seams onto the Azure CLI, package variables so tests can run without az.
var (
	restGet     = azure.RestGET
	tenantToken = azure.TenantToken
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

	token, err := tenantToken(configDir, tenantID)
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

	if !anyOK {
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

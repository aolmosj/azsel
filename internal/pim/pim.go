// Package pim reads a user's eligible Azure resource-role PIM assignments
// (Azure RBAC — Microsoft.Authorization) through the Azure CLI, so azsel can
// show, per tenant, the roles the user could activate without the portal.
package pim

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
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
}

// restGet is the seam onto the Azure CLI, a package variable so tests can feed
// canned JSON without az or a network (mirrors azure.run).
var restGet = azure.RestGET

// rootMGURL lists the current user's eligible resource-role instances across the
// whole tenant. The scope is the tenant root management group, whose id is the
// tenant GUID; asTarget() restricts the result to the caller, so it returns
// every eligibility the user has regardless of which subscriptions are visible —
// a subscription where you are only eligible (never activated) would not even
// appear in `az account list`, but it shows up here.
const rootMGURL = "https://management.azure.com/providers/Microsoft.Management/managementGroups/%s" +
	"/providers/Microsoft.Authorization/roleEligibilityScheduleInstances" +
	"?api-version=2020-10-01&$filter=asTarget()"

// restResponse mirrors the ARM payload, keeping only the fields azsel shows.
type restResponse struct {
	Value []struct {
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
			} `json:"expandedProperties"`
		} `json:"properties"`
	} `json:"value"`
}

// ListEligible returns the eligible resource roles for the tenant whose az
// config lives in configDir. tenantID names the root management group scope and
// is required: a tenant may be stored without one, and without it there is
// nothing to query, so that is reported before touching az.
func ListEligible(configDir, tenantID string) ([]Eligible, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("tenant has no tenant ID configured; cannot query PIM eligibility")
	}
	body, err := restGet(configDir, fmt.Sprintf(rootMGURL, tenantID))
	if err != nil {
		return nil, err
	}
	var resp restResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing PIM response: %w", err)
	}

	out := make([]Eligible, 0, len(resp.Value))
	for _, v := range resp.Value {
		p := v.Properties
		e := Eligible{
			RoleName:  p.ExpandedProperties.RoleDefinition.DisplayName,
			ScopeName: p.ExpandedProperties.Scope.DisplayName,
			ScopeType: p.ExpandedProperties.Scope.Type,
			ScopeID:   p.ExpandedProperties.Scope.ID,
			Status:    p.Status,
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
		return strings.Compare(a.ScopeName, b.ScopeName)
	})
	return out, nil
}

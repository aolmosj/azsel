package tui

import "github.com/aolmosj/azsel/internal/config"

type TenantItem struct {
	tenant config.Tenant
	active bool
}

func NewTenantItem(t config.Tenant, active bool) TenantItem {
	return TenantItem{tenant: t, active: active}
}

// marker is the two-column prefix flagging the tenant AZURE_CONFIG_DIR
// currently points at. Two columns either way, so names stay aligned.
func (t TenantItem) marker() string {
	if t.active {
		return activeStyle.Render("* ")
	}
	return "  "
}

// FilterValue is the only method list.Item requires. Title and Description
// belong to list.DefaultItem, which exists for DefaultDelegate; this list
// uses tenantDelegate, so implementing them only duplicated its rendering.
//
// Both name and ID are searchable: pasting a GUID should find its tenant.
func (t TenantItem) FilterValue() string {
	return t.tenant.Name + " " + t.tenant.TenantID
}

package tui

import "github.com/aolmosj/azsel/internal/config"

type TenantItem struct {
	tenant    config.Tenant
	active    bool
	isDefault bool
}

func NewTenantItem(t config.Tenant, active, isDefault bool) TenantItem {
	return TenantItem{tenant: t, active: active, isDefault: isDefault}
}

// marker is the two-column prefix flagging tenant state: column one is "*"
// for the active tenant, column two is "D" for the default. They are
// different things — active is where this shell points now, default is where
// new shells start — so a tenant can carry either, both, or neither. Always
// two columns wide so names stay aligned.
func (t TenantItem) marker() string {
	active := " "
	if t.active {
		active = activeStyle.Render("*")
	}
	def := " "
	if t.isDefault {
		def = defaultStyle.Render("D")
	}
	return active + def
}

// FilterValue is the only method list.Item requires. Title and Description
// belong to list.DefaultItem, which exists for DefaultDelegate; this list
// uses tenantDelegate, so implementing them only duplicated its rendering.
//
// Both name and ID are searchable: pasting a GUID should find its tenant.
func (t TenantItem) FilterValue() string {
	return t.tenant.Name + " " + t.tenant.TenantID
}

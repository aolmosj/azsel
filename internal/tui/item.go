package tui

import "github.com/aolmosj/azsel/internal/config"

type TenantItem struct {
	tenant config.Tenant
	active bool
}

func NewTenantItem(t config.Tenant, active bool) TenantItem {
	return TenantItem{tenant: t, active: active}
}

func (t TenantItem) Title() string {
	prefix := "  "
	if t.active {
		prefix = activeStyle.Render("* ")
	}
	return prefix + t.tenant.Name
}

func (t TenantItem) Description() string {
	return t.tenant.TenantID
}

func (t TenantItem) FilterValue() string {
	return t.tenant.Name + " " + t.tenant.TenantID
}

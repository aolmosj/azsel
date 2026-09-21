package tui

import (
	"github.com/aolmosj/azsel/internal/config"
	"github.com/aolmosj/azsel/internal/pim"
)

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

// pimItem adapts an eligible role to the bubbles list. Unlike TenantItem it
// implements list.DefaultItem (Title/Description) and rides the default
// delegate, which gives filter highlighting for free.
type pimItem struct{ e pim.Eligible }

func (p pimItem) Title() string {
	scope := p.e.ScopeName
	if scope == "" {
		scope = "(unknown scope)"
	}
	return p.e.RoleName + "  —  " + scope
}

func (p pimItem) Description() string {
	until := "permanent"
	if p.e.End != nil {
		until = p.e.End.Format("2006-01-02 15:04")
	}
	d := "(" + p.e.ScopeType + ") · until " + until
	if via := p.e.ViaGroup(); via != "" {
		d += " · via " + via
	}
	return d
}

// Role, scope, type and the granting group are all searchable.
func (p pimItem) FilterValue() string {
	return p.e.RoleName + " " + p.e.ScopeName + " " + p.e.ScopeType + " " + p.e.PrincipalName
}

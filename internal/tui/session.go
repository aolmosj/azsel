package tui

import (
	"github.com/aolmosj/azsel/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// sessionsLoadedMsg carries each tenant's login state (name -> usable).
type sessionsLoadedMsg struct{ valid map[string]bool }

// checkSessionsCmd probes every tenant's session off the UI loop and reports the
// result. Sequential is fine: it runs in a background goroutine while the list
// is already on screen, and the markers appear once it returns.
func checkSessionsCmd(check func(config.Tenant) bool, tenants []config.Tenant) tea.Cmd {
	return func() tea.Msg {
		valid := make(map[string]bool, len(tenants))
		for _, t := range tenants {
			valid[t.Name] = check(t)
		}
		return sessionsLoadedMsg{valid: valid}
	}
}

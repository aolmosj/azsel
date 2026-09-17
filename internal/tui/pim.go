package tui

import (
	"github.com/aolmosj/azsel/internal/config"
	"github.com/aolmosj/azsel/internal/pim"
	tea "github.com/charmbracelet/bubbletea"
)

// pimLoadedMsg carries the eligible roles once loaded; pimErrMsg carries a
// failure. They are the result side of loadPIMCmd.
type (
	pimLoadedMsg struct{ rows []pim.Eligible }
	pimErrMsg    struct{ err error }
)

// loadPIMCmd runs the (blocking) PIM lookup off the UI loop and reports the
// outcome as a message. It is the first real async command in the TUI beyond the
// delegate's selectTenantMsg: listing eligibility is a network round-trip, so it
// must not block Update the way setting a default does.
func loadPIMCmd(load func(config.Tenant) ([]pim.Eligible, error), t config.Tenant) tea.Cmd {
	return func() tea.Msg {
		rows, err := load(t)
		if err != nil {
			return pimErrMsg{err: err}
		}
		return pimLoadedMsg{rows: rows}
	}
}

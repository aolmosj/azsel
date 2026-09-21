package tui

import (
	"strings"

	"github.com/aolmosj/azsel/internal/config"
	"github.com/aolmosj/azsel/internal/pim"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// screen is the full-screen view the TUI is showing. It replaces a set of
// independent booleans, which could otherwise combine into impossible states
// (confirming and browsing PIM at once).
type screen int

const (
	screenList screen = iota
	screenConfirm
	screenPIM
)

type Model struct {
	list     list.Model
	selected *config.Tenant
	quitting bool

	// setDefault persists a tenant as the default and returns a note to show
	// (e.g. where an existing ~/.azure was backed up), empty if none. Injected
	// so the model stays free of config and clock: cmd/tui.go closes over
	// both. Nil disables the "d" key.
	setDefault func(name string) (note string, err error)

	// listPIM loads a tenant's eligible resource-role assignments. Injected the
	// same way as setDefault, keeping azure/pim/config out of the model. Nil
	// disables the "p" key.
	listPIM func(t config.Tenant) ([]pim.Eligible, error)

	// screen selects the view. confirmName carries the "d" prompt's subject;
	// setting a default rewrites ~/.azure, more consequential than anything else
	// the TUI does, so it asks first.
	screen      screen
	confirmName string
	status      string

	// PIM screen state, valid while screen == screenPIM. The eligible roles are
	// held in their own filterable list, so "/" narrows them like the tenant
	// list and long results scroll.
	pimTenant  string
	pimList    list.Model
	pimErr     string
	pimLoading bool
	spinner    spinner.Model

	width  int
	height int
}

func NewModel(tenants []config.Tenant, currentConfigDir, defaultName string, setDefault func(name string) (string, error), listPIM func(config.Tenant) ([]pim.Eligible, error)) Model {
	items := make([]list.Item, len(tenants))
	for i, t := range tenants {
		active := t.ConfigDir == currentConfigDir
		isDefault := defaultName != "" && strings.EqualFold(t.Name, defaultName)
		items[i] = NewTenantItem(t, active, isDefault)
	}

	delegate := newDelegate()
	l := list.New(items, delegate, 80, 20)
	l.Title = "Azure Tenants"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.Styles.Title = titleStyle
	l.Styles.StatusBar = statusStyle
	l.Styles.FilterPrompt = lipgloss.NewStyle().Foreground(azureBlue)
	l.Styles.FilterCursor = lipgloss.NewStyle().Foreground(azureBlue)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(azureBlue)

	// The PIM results get their own list so they filter and scroll. Its quit key
	// is repurposed as "back" (the program is not quit from here).
	pl := list.New(nil, list.NewDefaultDelegate(), 80, 20)
	pl.Title = "Eligible PIM roles"
	pl.SetShowStatusBar(false)
	pl.SetFilteringEnabled(true)
	pl.Styles.Title = titleStyle
	pl.Styles.FilterPrompt = lipgloss.NewStyle().Foreground(azureBlue)
	pl.Styles.FilterCursor = lipgloss.NewStyle().Foreground(azureBlue)
	pl.KeyMap.Quit = key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("esc", "back"))

	return Model{list: l, setDefault: setDefault, listPIM: listPIM, spinner: sp, pimList: pl}
}

// applyDefault marks name as the default across the list items, leaving one
// place that decides the marker (kept from #6).
func (m *Model) applyDefault(name string) {
	items := m.list.Items()
	for i, it := range items {
		ti, ok := it.(TenantItem)
		if !ok {
			continue
		}
		ti.isDefault = strings.EqualFold(ti.tenant.Name, name)
		items[i] = ti
	}
	m.list.SetItems(items)
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		h, v := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)
		m.pimList.SetSize(msg.Width-h, msg.Height-v)
		return m, nil

	case tea.KeyMsg:
		switch m.screen {
		case screenConfirm:
			// While confirming, keys answer the prompt and never reach the list.
			switch msg.String() {
			case "y", "Y":
				m.screen = screenList
				if note, err := m.setDefault(m.confirmName); err != nil {
					m.status = "Could not set default: " + err.Error()
				} else {
					m.applyDefault(m.confirmName)
					m.status = "Default is now " + m.confirmName + "."
					if note != "" {
						m.status += " " + note
					}
				}
			default:
				m.screen = screenList
				m.status = ""
			}
			return m, nil

		case screenPIM:
			// While loading or showing an error there is no list to drive, so
			// esc/q just returns. esc works mid-load; the result-message guards
			// drop a late arrival.
			if m.pimLoading || m.pimErr != "" {
				switch msg.String() {
				case "esc", "q":
					m.screen = screenList
					m.pimErr, m.pimLoading = "", false
				}
				return m, nil
			}
			// With the list shown, esc/q returns — but only when not filtering,
			// where esc cancels the filter and the keys are search text. Anything
			// else (/, arrows) drives the list.
			if m.pimList.FilterState() != list.Filtering {
				switch msg.String() {
				case "esc", "q":
					m.screen = screenList
					return m, nil
				}
			}
			var cmd tea.Cmd
			m.pimList, cmd = m.pimList.Update(msg)
			return m, cmd

		default: // screenList
			if m.list.FilterState() == list.Filtering {
				break
			}
			switch msg.String() {
			case "q", "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			case "d":
				// Only with a way to persist it, and only on a real item.
				if m.setDefault != nil {
					if item, ok := m.list.SelectedItem().(TenantItem); ok {
						m.screen = screenConfirm
						m.confirmName = item.tenant.Name
						m.status = ""
						return m, nil
					}
				}
			case "p":
				// Only with a loader, and only on a real item.
				if m.listPIM != nil {
					if item, ok := m.list.SelectedItem().(TenantItem); ok {
						m.screen = screenPIM
						m.pimTenant = item.tenant.Name
						m.pimErr = ""
						m.pimLoading = true
						m.pimList.SetItems(nil)
						m.pimList.Title = "Eligible PIM roles — " + item.tenant.Name
						return m, tea.Batch(m.spinner.Tick, loadPIMCmd(m.listPIM, item.tenant))
					}
				}
			}
		}

	case selectTenantMsg:
		t := msg.tenant.tenant
		m.selected = &t
		m.quitting = true
		return m, tea.Quit

	case pimLoadedMsg:
		// Ignore a result that arrives after the user left the PIM screen.
		if m.screen == screenPIM {
			m.pimLoading = false
			items := make([]list.Item, len(msg.rows))
			for i, r := range msg.rows {
				items[i] = pimItem{e: r}
			}
			m.pimList.SetItems(items)
		}
		return m, nil

	case pimErrMsg:
		if m.screen == screenPIM {
			m.pimLoading = false
			m.pimErr = msg.err.Error()
		}
		return m, nil

	case spinner.TickMsg:
		if m.pimLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	// The confirmation and the PIM screen each take over the screen as a
	// centered modal rather than trailing after the list and help bar.
	switch m.screen {
	case screenConfirm:
		return m.confirmView()
	case screenPIM:
		return m.pimView()
	}
	body := m.list.View()
	if m.status != "" {
		body += "\n\n" + statusMsgStyle.Render(m.status)
	}
	return appStyle.Render(body)
}

func (m Model) confirmView() string {
	box := confirmBoxStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
		confirmTitleStyle.Render("Set "+m.confirmName+" as the default?"),
		"",
		"This repoints ~/.azure to this tenant.",
		"",
		confirmKeysStyle.Render("y")+" set default     "+confirmKeysStyle.Render("N")+" cancel",
	))
	// Center in the content area (terminal minus the app margin). Before the
	// first WindowSizeMsg the size is unknown, so fall back to the bare box.
	fh, fv := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
	w, h := m.width-fh, m.height-fv
	if w <= 0 || h <= 0 {
		return appStyle.Render(box)
	}
	return appStyle.Render(lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box))
}

// pimView shows the eligible resource roles for the tenant chosen with "p".
// Loading, an error, and an empty result are centered boxes; the populated
// result is a filterable, scrolling list ("/" narrows it, esc goes back).
func (m Model) pimView() string {
	switch {
	case m.pimLoading:
		return m.pimBox(confirmTitleStyle.Render("Eligible PIM roles — "+m.pimTenant) +
			"\n\n" + m.spinner.View() + " Loading eligible roles…")
	case m.pimErr != "":
		return m.pimBox(statusMsgStyle.Render("Could not load PIM roles:") +
			"\n" + m.pimErr + "\n\n" + confirmKeysStyle.Render("esc") + " back")
	case len(m.pimList.Items()) == 0:
		return m.pimBox("No eligible resource-role assignments for " + m.pimTenant + "." +
			"\n\n" + confirmKeysStyle.Render("esc") + " back")
	}
	return appStyle.Render(m.pimList.View())
}

// pimBox centers a message the way confirmView does, for the PIM screen's
// non-list states.
func (m Model) pimBox(body string) string {
	box := confirmBoxStyle.Render(body)
	fh, fv := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
	w, h := m.width-fh, m.height-fv
	if w <= 0 || h <= 0 {
		return appStyle.Render(box)
	}
	return appStyle.Render(lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box))
}

func (m Model) Selected() *config.Tenant {
	return m.selected
}

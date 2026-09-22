package tui

import (
	"strings"

	"github.com/aolmosj/azsel/internal/config"
	"github.com/aolmosj/azsel/internal/pim"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
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
	screenExpired
)

// expiredAction is what the user was attempting when the "session expired"
// prompt appeared, so "proceed anyway" can resume it.
type expiredAction int

const (
	actActivate expiredAction = iota
	actPIM
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

	// checkSession reports whether a tenant's login is still usable. Set through
	// SetSessionCheck (rather than the constructor, to spare its many callers a
	// parameter); nil skips the on-open session check and its markers.
	checkSession func(t config.Tenant) bool

	// loginWanted records that the user asked to (re)login the selected tenant;
	// cmd/tui.go runs the interactive az login after the program exits.
	loginWanted *config.Tenant

	// expiredTenant is the tenant behind the "session expired" prompt, and
	// expiredAction what the user was doing (activate, view PIM) so it can be
	// resumed on "proceed anyway".
	expiredTenant config.Tenant
	expiredAction expiredAction

	// screen selects the view. confirmName carries the "d" prompt's subject;
	// setting a default rewrites ~/.azure, more consequential than anything else
	// the TUI does, so it asks first.
	screen      screen
	confirmName string
	status      string

	// PIM screen state, valid while screen == screenPIM. The eligible roles are
	// held in their own filterable list, so "/" narrows them like the tenant
	// list and long results scroll.
	pimTenant  config.Tenant
	pimList    list.Model
	pimErr     string
	pimLoading bool

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

	return Model{list: l, setDefault: setDefault, listPIM: listPIM, pimList: pl}
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

// SetSessionCheck installs the session probe and enables the on-open check.
func (m *Model) SetSessionCheck(f func(config.Tenant) bool) {
	m.checkSession = f
}

func (m Model) Init() tea.Cmd {
	if m.checkSession == nil {
		return nil
	}
	var tenants []config.Tenant
	for _, it := range m.list.Items() {
		if ti, ok := it.(TenantItem); ok {
			tenants = append(tenants, ti.tenant)
		}
	}
	return checkSessionsCmd(m.checkSession, tenants)
}

// applySessions stamps each item with its login state, leaving one place that
// decides the marker (as applyDefault does).
func (m *Model) applySessions(valid map[string]bool) {
	items := m.list.Items()
	for i, it := range items {
		ti, ok := it.(TenantItem)
		if !ok {
			continue
		}
		if v, known := valid[ti.tenant.Name]; known {
			if v {
				ti.session = sessionOK
			} else {
				ti.session = sessionExpired
			}
		}
		items[i] = ti
	}
	m.list.SetItems(items)
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

		case screenExpired:
			// The "session expired" prompt: log in, proceed anyway, or cancel.
			switch msg.String() {
			case "l":
				lt := m.expiredTenant
				m.loginWanted = &lt
				m.quitting = true
				return m, tea.Quit
			case "enter":
				t := m.expiredTenant
				if m.expiredAction == actPIM {
					m.screen = screenPIM
					m.pimTenant = t
					m.pimErr = ""
					m.pimLoading = true
					m.pimList.SetItems(nil)
					m.pimList.Title = "Eligible PIM roles — " + t.Name
					return m, loadPIMCmd(m.listPIM, t)
				}
				m.selected = &t
				m.quitting = true
				return m, tea.Quit
			default:
				m.screen = screenList
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
				case "l":
					// The error is most often an expired session for this tenant;
					// offer re-login right here (cmd/tui.go runs it after exit).
					if m.pimErr != "" {
						lt := m.pimTenant
						m.loginWanted = &lt
						m.quitting = true
						return m, tea.Quit
					}
				}
				return m, nil
			}
			// With the list shown, esc/q returns â but only when not filtering,
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
			case "enter":
				// On an expired session, warn and offer to log in before activating â the next
				// az command would otherwise just fail. Unknown/valid sessions fall through to
				// the delegate, which activates as usual.
				if item, ok := m.list.SelectedItem().(TenantItem); ok && item.session == sessionExpired {
					m.screen = screenExpired
					m.expiredTenant = item.tenant
					m.expiredAction = actActivate
					return m, nil
				}
			case "l":
				// Record the intent and quit; cmd/tui.go runs the interactive az login after
				// the program exits, as it does for activation.
				if item, ok := m.list.SelectedItem().(TenantItem); ok {
					lt := item.tenant
					m.loginWanted = &lt
					m.quitting = true
					return m, tea.Quit
				}
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
						if item.session == sessionExpired {
							m.screen = screenExpired
							m.expiredTenant = item.tenant
							m.expiredAction = actPIM
							return m, nil
						}
						m.screen = screenPIM
						m.pimTenant = item.tenant
						m.pimErr = ""
						m.pimLoading = true
						m.pimList.SetItems(nil)
						m.pimList.Title = "Eligible PIM roles â " + item.tenant.Name
						return m, loadPIMCmd(m.listPIM, item.tenant)
					}
				}
			}
		}

	case selectTenantMsg:
		t := msg.tenant.tenant
		m.selected = &t
		m.quitting = true
		return m, tea.Quit

	case sessionsLoadedMsg:
		m.applySessions(msg.valid)
		return m, nil

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
	case screenExpired:
		return m.expiredView()
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
		return m.pimBox(statusMsgStyle.Render("Loading eligible PIM roles for " + m.pimTenant.Name + "... (querying Azure, this can take a few seconds)"))
	case m.pimErr != "":
		return m.pimBox(statusMsgStyle.Render("Could not load PIM roles:") +
			"\n" + m.pimErr + "\n\n" + confirmKeysStyle.Render("l") + " log in     " + confirmKeysStyle.Render("esc") + " back")
	case len(m.pimList.Items()) == 0:
		return m.pimBox("No eligible resource-role assignments for " + m.pimTenant.Name + "." +
			"\n\n" + confirmKeysStyle.Render("esc") + " back")
	}
	return appStyle.Render(m.pimList.View())
}

// pimBox centers a message the way confirmView does, for the PIM screen's
// non-list states.
// expiredView is the prompt shown when activating a tenant whose login lapsed.
func (m Model) expiredView() string {
	return m.pimBox(confirmTitleStyle.Render(m.expiredTenant.Name+" â session expired") +
		"\n\nIts Azure login has expired; az commands will fail until you sign in again." +
		"\n\n" + confirmKeysStyle.Render("l") + " log in     " +
		confirmKeysStyle.Render("enter") + " proceed anyway     " +
		confirmKeysStyle.Render("esc") + " cancel")
}

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

// LoginWanted returns the tenant the user asked to (re)login with "l", or nil.
func (m Model) LoginWanted() *config.Tenant {
	return m.loginWanted
}

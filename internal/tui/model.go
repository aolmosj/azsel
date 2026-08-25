package tui

import (
	"strings"

	"github.com/aolmosj/azsel/internal/config"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

	// Confirmation state for the "d" key. Setting a default rewrites
	// ~/.azure, which is more consequential than anything else the TUI does,
	// so it asks first.
	confirming  bool
	confirmName string
	status      string

	width  int
	height int
}

func NewModel(tenants []config.Tenant, currentConfigDir, defaultName string, setDefault func(name string) (string, error)) Model {
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

	return Model{list: l, setDefault: setDefault}
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
		return m, nil

	case tea.KeyMsg:
		// While confirming, keys answer the prompt and never reach the list.
		if m.confirming {
			switch msg.String() {
			case "y", "Y":
				m.confirming = false
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
				m.confirming = false
				m.status = ""
			}
			return m, nil
		}

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
					m.confirming = true
					m.confirmName = item.tenant.Name
					m.status = ""
					return m, nil
				}
			}
		}

	case selectTenantMsg:
		t := msg.tenant.tenant
		m.selected = &t
		m.quitting = true
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	// The confirmation takes over the screen as a centered modal rather than
	// trailing after the list and help bar, where it was easy to miss.
	if m.confirming {
		return m.confirmView()
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

func (m Model) Selected() *config.Tenant {
	return m.selected
}

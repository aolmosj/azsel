package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

type selectTenantMsg struct {
	tenant TenantItem
}

func newDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()

	d.UpdateFunc = func(msg tea.Msg, m *list.Model) tea.Cmd {
		if msg, ok := msg.(tea.KeyMsg); ok {
			if msg.String() == "enter" {
				if item, ok := m.SelectedItem().(TenantItem); ok {
					return func() tea.Msg {
						return selectTenantMsg{tenant: item}
					}
				}
			}
		}
		return nil
	}

	d.ShortHelpFunc = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("enter", "activate"),
			),
			key.NewBinding(
				key.WithKeys("/"),
				key.WithHelp("/", "filter"),
			),
		}
	}

	return d
}

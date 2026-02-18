package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

type selectTenantMsg struct {
	tenant TenantItem
}

type tenantDelegate struct{}

func newDelegate() tenantDelegate {
	return tenantDelegate{}
}

func (d tenantDelegate) Height() int                             { return 2 }
func (d tenantDelegate) Spacing() int                            { return 1 }
func (d tenantDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
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

func (d tenantDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(TenantItem)
	if !ok {
		return
	}

	name := item.tenant.Name
	desc := item.tenant.TenantID
	marker := "  "
	if item.active {
		marker = activeStyle.Render("* ")
	}

	if index == m.Index() {
		fmt.Fprintf(w, "%s%s\n  %s",
			marker,
			selectedTitleStyle.Render(name),
			selectedDescStyle.Render(desc))
	} else {
		fmt.Fprintf(w, "%s%s\n  %s",
			marker,
			normalTitleStyle.Render(name),
			normalDescStyle.Render(desc))
	}
}

func (d tenantDelegate) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "activate")),
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	}
}

func (d tenantDelegate) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}

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

func (d tenantDelegate) Height() int  { return 2 }
func (d tenantDelegate) Spacing() int { return 1 }
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

	titleStyle, descStyle := normalTitleStyle, normalDescStyle
	if index == m.Index() {
		titleStyle, descStyle = selectedTitleStyle, selectedDescStyle
	}

	fmt.Fprintf(w, "%s%s\n  %s",
		item.marker(),
		titleStyle.Render(name),
		descStyle.Render(desc))
}

// ShortHelp declares only the keys the list does not know about.
//
// list.Model.ShortHelp splices the delegate's bindings between the cursor
// keys and its own KeyMap, which already contributes Filter and Quit. Listing
// those here too printed them twice: "… / filter • q quit • / filter • q
// quit • ? more".
//
// enter is the one binding that belongs to this delegate — tenantDelegate
// handles it in Update; the list's KeyMap has no idea it exists.
func (d tenantDelegate) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "activate")),
	}
}

func (d tenantDelegate) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}

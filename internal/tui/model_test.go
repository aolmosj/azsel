package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func tenants() []config.Tenant {
	return []config.Tenant{
		{Name: "acme", TenantID: "11111111-1111-1111-1111-111111111111", ConfigDir: "/home/u/.azsel/tenants/acme"},
		{Name: "globex", TenantID: "22222222-2222-2222-2222-222222222222", ConfigDir: "/home/u/.azsel/tenants/globex"},
	}
}

func keyMsg(s string) tea.KeyMsg {
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// send applies a sequence of messages and returns the resulting model along
// with the last command emitted.
func send(t *testing.T, m Model, msgs ...tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, msg := range msgs {
		next, c := m.Update(msg)
		got, ok := next.(Model)
		if !ok {
			t.Fatalf("Update returned %T, wanted tui.Model", next)
		}
		m, cmd = got, c
	}
	return m, cmd
}

// The active tenant is inferred by comparing ConfigDir with AZURE_CONFIG_DIR.
// It is stored nowhere, which lets each terminal have its own.
func TestNewModelMarksActiveTenant(t *testing.T) {
	ts := tenants()
	m := NewModel(ts, ts[1].ConfigDir, "", nil)

	items := m.list.Items()
	if len(items) != 2 {
		t.Fatalf("%d items, wanted 2", len(items))
	}
	if items[0].(TenantItem).active {
		t.Error("acme marked as active")
	}
	if !items[1].(TenantItem).active {
		t.Error("globex not marked as active")
	}
}

func TestNewModelNoActiveTenant(t *testing.T) {
	for _, dir := range []string{"", "/otro/sitio"} {
		m := NewModel(tenants(), dir, "", nil)
		for _, it := range m.list.Items() {
			if it.(TenantItem).active {
				t.Errorf("with AZURE_CONFIG_DIR=%q a tenant is marked active", dir)
			}
		}
	}
}

func TestSelectedIsNilUntilChosen(t *testing.T) {
	m := NewModel(tenants(), "", "", nil)
	if got := m.Selected(); got != nil {
		t.Errorf("Selected() = %+v before choosing, wanted nil", got)
	}
}

func TestSelectTenantMsgSetsSelection(t *testing.T) {
	ts := tenants()
	m := NewModel(ts, "", "", nil)
	m, _ = send(t, m, selectTenantMsg{tenant: NewTenantItem(ts[1], false, false)})

	got := m.Selected()
	if got == nil {
		t.Fatal("Selected() = nil after selecting")
	}
	if got.Name != "globex" {
		t.Errorf("Selected().Name = %q, wanted globex", got.Name)
	}
	// Exits the TUI and stops rendering.
	if v := m.View(); v != "" {
		t.Errorf("View() = %q after selecting, wanted empty", v)
	}
}

// Pressing enter on an item must emit selectTenantMsg. The delegate produces
// it, not the model.
func TestEnterEmitsSelectTenantMsg(t *testing.T) {
	m := NewModel(tenants(), "", "", nil)
	_, cmd := send(t, m, keyMsg("enter"))
	if cmd == nil {
		t.Fatal("enter emitted no command")
	}
	if _, ok := cmd().(selectTenantMsg); !ok {
		t.Fatalf("enter emitted %T, wanted selectTenantMsg", cmd())
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []tea.KeyMsg{keyMsg("q"), {Type: tea.KeyCtrlC}} {
		m := NewModel(tenants(), "", "", nil)
		m, _ = send(t, m, k)
		if v := m.View(); v != "" {
			t.Errorf("after %v, View() = %q, wanted empty", k, v)
		}
		if m.Selected() != nil {
			t.Errorf("after %v there is a selection, wanted nil", k)
		}
	}
}

// Protects Update's guard: while filtering, "q" is search text, not the quit
// command. Without it, searching for "quux" would close the application.
func TestQuitKeyIsTextWhileFiltering(t *testing.T) {
	m := NewModel(tenants(), "", "", nil)
	m, _ = send(t, m, keyMsg("/"))
	if m.list.FilterState() != list.Filtering {
		t.Fatalf("\"/\" did not enter filter mode: state = %v", m.list.FilterState())
	}

	m, _ = send(t, m, keyMsg("q"))
	if v := m.View(); v == "" {
		t.Error("\"q\" while filtering closed the TUI")
	}
	if m.Selected() != nil {
		t.Error("\"q\" while filtering left a selection")
	}
}

// The filter searches by name and by ID: pasting a GUID must find its tenant.
func TestFilterValueCoversNameAndID(t *testing.T) {
	item := NewTenantItem(tenants()[0], false, false)
	got := item.FilterValue()
	if !strings.Contains(got, "acme") {
		t.Errorf("FilterValue() = %q, wanted it to include the name", got)
	}
	if !strings.Contains(got, "11111111-1111-1111-1111-111111111111") {
		t.Errorf("FilterValue() = %q, wanted it to include the tenant ID", got)
	}
}

func TestWindowSizeMsgResizesList(t *testing.T) {
	m := NewModel(tenants(), "", "", nil)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if w := m.list.Width(); w >= 120 {
		t.Errorf("list width = %d, wanted the margin discounted from 120", w)
	}
	if h := m.list.Height(); h >= 40 {
		t.Errorf("list height = %d, wanted the margin discounted from 40", h)
	}
}

// The delegate renders two lines per tenant, name and ID, and marks the active one.
func TestDelegateRender(t *testing.T) {
	ts := tenants()
	m := NewModel(ts, ts[0].ConfigDir, "", nil)
	d := newDelegate()

	if got := d.Height(); got != 2 {
		t.Errorf("Height() = %d, wanted 2 (name + ID)", got)
	}

	var buf bytes.Buffer
	d.Render(&buf, m.list, 0, m.list.Items()[0])
	out := buf.String()
	if !strings.Contains(out, "acme") {
		t.Errorf("render = %q, wanted the name", out)
	}
	if !strings.Contains(out, ts[0].TenantID) {
		t.Errorf("render = %q, wanted the tenant ID", out)
	}
	if !strings.Contains(out, "*") {
		t.Errorf("render = %q, wanted the active tenant's \"*\" marker", out)
	}

	buf.Reset()
	d.Render(&buf, m.list, 1, m.list.Items()[1])
	if out := buf.String(); strings.Contains(out, "*") {
		t.Errorf("render of the inactive tenant = %q, wanted no marker", out)
	}
}

// The marker lives in a single place. It used to exist twice —in Title() and
// reimplemented in the delegate— and only the delegate's copy was used, so the
// two could diverge without anything noticing.
func TestMarker(t *testing.T) {
	ts := tenants()

	active := NewTenantItem(ts[0], true, false).marker()
	if !strings.Contains(active, "*") {
		t.Errorf("active marker = %q, wanted it to include \"*\"", active)
	}
	inactive := NewTenantItem(ts[0], false, false).marker()
	if strings.Contains(inactive, "*") {
		t.Errorf("inactive marker = %q, wanted no \"*\"", inactive)
	}
	// Both take two columns so that names stay aligned.
	if got := len(ansi.Strip(inactive)); got != 2 {
		t.Errorf("inactive marker width = %d, wanted 2", got)
	}
	if got := len(ansi.Strip(active)); got != 2 {
		t.Errorf("active marker width = %d, wanted 2", got)
	}
}

// Selected or not, the item shows the same data. What changes is the
// adornment: the selected style adds a left border, which is real content and
// not an escape code, so the strings can't be compared as-is.
func TestDelegateRenderShowsSameDataSelectedOrNot(t *testing.T) {
	ts := tenants()
	m := NewModel(ts, "", "", nil)
	d := newDelegate()

	var selected, normal bytes.Buffer
	d.Render(&selected, m.list, m.list.Index(), m.list.Items()[m.list.Index()])
	d.Render(&normal, m.list, m.list.Index()+1, m.list.Items()[m.list.Index()])

	for label, out := range map[string]string{
		"selected": ansi.Strip(selected.String()),
		"normal":   ansi.Strip(normal.String()),
	} {
		if !strings.Contains(out, ts[0].Name) {
			t.Errorf("render %s = %q, wanted the name", label, out)
		}
		if !strings.Contains(out, ts[0].TenantID) {
			t.Errorf("render %s = %q, wanted the tenant ID", label, out)
		}
		if lines := strings.Count(out, "\n") + 1; lines != 2 {
			t.Errorf("render %s has %d lines, wanted 2 (name + ID)", label, lines)
		}
	}
}

// helpKeys returns the keys visible in the help bar. Disabled bindings are in
// the slice but the help component does not render them.
func helpKeys(m Model) []string {
	var keys []string
	for _, b := range m.list.ShortHelp() {
		if b.Enabled() {
			keys = append(keys, b.Help().Key)
		}
	}
	return keys
}

// list.Model.ShortHelp interleaves the delegate's bindings between the cursor
// keys and its own KeyMap, which already provides Filter and Quit. Declaring
// them in the delegate too printed them twice.
func TestHelpHasNoDuplicateKeys(t *testing.T) {
	m := NewModel(tenants(), "", "", nil)

	seen := map[string]int{}
	for _, k := range helpKeys(m) {
		seen[k]++
	}
	for k, n := range seen {
		if n > 1 {
			t.Errorf("key %q appears %d times in the help: %v", k, n, helpKeys(m))
		}
	}
}

// The delegate should only add what the list doesn't know. enter it handles;
// "/" and "q" the list provides on its own.
func TestHelpContentsAreComplete(t *testing.T) {
	m := NewModel(tenants(), "", "", nil)
	keys := helpKeys(m)

	has := func(want string) bool {
		for _, k := range keys {
			if k == want {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"enter", "/", "q"} {
		if !has(want) {
			t.Errorf("missing %q in the help: %v", want, keys)
		}
	}

	// The delegate declares only the keys the list doesn't know: enter and d.
	// "/" and "q" are provided by the list, and must not be repeated (#21).
	declared := map[string]bool{}
	for _, b := range newDelegate().ShortHelp() {
		declared[b.Help().Key] = true
	}
	if !declared["enter"] || !declared["d"] {
		t.Errorf("the delegate must declare enter and d, has %v", declared)
	}
	if declared["/"] || declared["q"] {
		t.Errorf("the delegate must not declare / or q (the list provides them): %v", declared)
	}
}

// With the filter open the bar changes shape: the list omits the delegate's
// ShortHelp. There too there must be no repetitions.
func TestHelpHasNoDuplicateKeysWhileFiltering(t *testing.T) {
	m := NewModel(tenants(), "", "", nil)
	m, _ = send(t, m, keyMsg("/"))
	if m.list.FilterState() != list.Filtering {
		t.Fatalf("did not enter filter mode: %v", m.list.FilterState())
	}

	seen := map[string]int{}
	for _, k := range helpKeys(m) {
		seen[k]++
	}
	for k, n := range seen {
		if n > 1 {
			t.Errorf("while filtering, key %q appears %d times: %v", k, n, helpKeys(m))
		}
	}
}

// And the end-to-end check, over what the user sees.
func TestRenderedHelpHasNoRepeatedEntries(t *testing.T) {
	m := NewModel(tenants(), "", "", nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	view := ansi.Strip(updated.(Model).View())

	for _, entry := range []string{"/ filter", "q quit", "enter activate"} {
		if got := strings.Count(view, entry); got != 1 {
			t.Errorf("%q appears %d times in the view, wanted 1:\n%s", entry, got, view)
		}
	}
}

// The marker distinguishes the four combinations of active and default, and
// keeps two columns in all of them so that names stay aligned.
func TestMarkerDefaultAndActive(t *testing.T) {
	ts := tenants()
	cases := []struct {
		name            string
		active, isDef   bool
		wantStar, wantD bool
	}{
		{"none", false, false, false, false},
		{"only active", true, false, true, false},
		{"only default", false, true, false, true},
		{"both", true, true, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := ansi.Strip(NewTenantItem(ts[0], c.active, c.isDef).marker())
			if strings.Contains(m, "*") != c.wantStar {
				t.Errorf("marker %q: presence of \"*\" = %v, wanted %v", m, strings.Contains(m, "*"), c.wantStar)
			}
			if strings.Contains(m, "D") != c.wantD {
				t.Errorf("marker %q: presence of \"D\" = %v, wanted %v", m, strings.Contains(m, "D"), c.wantD)
			}
			if len(m) != 2 {
				t.Errorf("marker %q measures %d columns, wanted 2", m, len(m))
			}
		})
	}
}

// NewModel marks as default the tenant whose name matches, case-insensitively,
// and none if the name is empty.
func TestNewModelMarksDefault(t *testing.T) {
	ts := tenants() // acme, globex
	m := NewModel(ts, "", "GLOBEX", nil)
	items := m.list.Items()
	if items[0].(TenantItem).isDefault {
		t.Error("acme marked as default")
	}
	if !items[1].(TenantItem).isDefault {
		t.Error("globex not marked as default despite matching (case-insensitive)")
	}

	none := NewModel(ts, "", "", nil)
	for _, it := range none.list.Items() {
		if it.(TenantItem).isDefault {
			t.Error("a default is marked with an empty defaultName")
		}
	}
}

// Active and default are independent: the render shows them together when they
// coincide on the same tenant, and separately when not.
func TestDelegateRendersDefaultMarker(t *testing.T) {
	ts := tenants()
	// acme active, globex default.
	m := NewModel(ts, ts[0].ConfigDir, "globex", nil)
	d := newDelegate()

	var acme, globex bytes.Buffer
	d.Render(&acme, m.list, 0, m.list.Items()[0])
	d.Render(&globex, m.list, 1, m.list.Items()[1])

	if a := ansi.Strip(acme.String()); !strings.Contains(a, "*") || strings.Contains(a[:3], "D") {
		t.Errorf("acme should be active and not default: %q", a)
	}
	if g := ansi.Strip(globex.String()); !strings.Contains(g[:3], "D") {
		t.Errorf("globex should show the default marker: %q", g)
	}
}

// The "d" key asks for confirmation before setting the default, because it
// rewrites ~/.azure — the only TUI action with an effect on disk.
func TestSetDefaultKeyAsksConfirmation(t *testing.T) {
	ts := tenants()
	var called []string
	set := func(name string) (string, error) { called = append(called, name); return "", nil }
	m := NewModel(ts, "", "", set)

	// d on the first item enters confirmation, without setting yet.
	m, _ = send(t, m, keyMsg("d"))
	if !strings.Contains(m.View(), "as the default?") {
		t.Errorf("d did not show the confirmation:\n%s", ansi.Strip(m.View()))
	}
	if len(called) != 0 {
		t.Errorf("the default was set before confirming: %v", called)
	}

	// and confirm: it sets and the marker updates.
	m, _ = send(t, m, keyMsg("y"))
	if len(called) != 1 || called[0] != ts[0].Name {
		t.Fatalf("setDefault called with %v, wanted [%s]", called, ts[0].Name)
	}
	if !m.list.Items()[0].(TenantItem).isDefault {
		t.Error("the default marker did not update after confirming")
	}
}

func TestSetDefaultKeyCancelled(t *testing.T) {
	ts := tenants()
	var called []string
	set := func(name string) (string, error) { called = append(called, name); return "", nil }
	m := NewModel(ts, "", "", set)

	m, _ = send(t, m, keyMsg("d"))
	m, _ = send(t, m, keyMsg("n"))
	if len(called) != 0 {
		t.Errorf("n did not cancel: %v was set", called)
	}
	if strings.Contains(m.View(), "as the default?") {
		t.Error("still showing the confirmation after cancelling")
	}
}

// An error while setting is shown, not swallowed.
func TestSetDefaultKeyReportsError(t *testing.T) {
	ts := tenants()
	set := func(name string) (string, error) { return "", errTest }
	m := NewModel(ts, "", "", set)
	m, _ = send(t, m, keyMsg("d"))
	m, _ = send(t, m, keyMsg("y"))
	if !strings.Contains(m.View(), "Could not set default") {
		t.Errorf("the error was not shown:\n%s", ansi.Strip(m.View()))
	}
}

// Without a callback (setDefault nil), the d key does nothing.
func TestSetDefaultKeyNoopWithoutCallback(t *testing.T) {
	m := NewModel(tenants(), "", "", nil)
	m, _ = send(t, m, keyMsg("d"))
	if strings.Contains(m.View(), "as the default?") {
		t.Error("d entered confirmation without a callback")
	}
}

// While filtering, "d" is search text, not the command to set default — the
// same guard that protects "q" (#7).
func TestSetDefaultKeyIsTextWhileFiltering(t *testing.T) {
	ts := tenants()
	var called []string
	set := func(name string) (string, error) { called = append(called, name); return "", nil }
	m := NewModel(ts, "", "", set)

	m, _ = send(t, m, keyMsg("/"))
	if m.list.FilterState() != list.Filtering {
		t.Fatalf("did not enter filtering")
	}
	m, _ = send(t, m, keyMsg("d"))
	if strings.Contains(m.View(), "as the default?") {
		t.Error("d opened the confirmation while filtering")
	}
	if len(called) != 0 {
		t.Errorf("d set the default while filtering: %v", called)
	}
}

var errTest = &testError{}

type testError struct{}

func (*testError) Error() string { return "boom" }

// The confirmation is a modal that takes over the view, not a line appended
// after the list and help bar where it was easy to miss (#38). Guard: while
// confirming, other tenants from the list must not render.
func TestSetDefaultConfirmIsAModalNotAppended(t *testing.T) {
	ts := tenants() // acme (selected), globex
	m := NewModel(ts, "", "", func(string) (string, error) { return "", nil })
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 60, Height: 20})
	m, _ = send(t, m, keyMsg("d"))

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "as the default?") {
		t.Fatalf("confirmation prompt missing:\n%s", view)
	}
	// globex only appears in the list, never in the modal — if it shows, the
	// prompt is back to trailing after the list.
	if strings.Contains(view, ts[1].Name) {
		t.Errorf("the list still renders during confirmation (found %q):\n%s", ts[1].Name, view)
	}
}

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

// send aplica una secuencia de mensajes y devuelve el modelo resultante junto
// al último comando emitido.
func send(t *testing.T, m Model, msgs ...tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, msg := range msgs {
		next, c := m.Update(msg)
		got, ok := next.(Model)
		if !ok {
			t.Fatalf("Update devolvió %T, quería tui.Model", next)
		}
		m, cmd = got, c
	}
	return m, cmd
}

// El tenant activo se deduce comparando ConfigDir con AZURE_CONFIG_DIR. No se
// guarda en ningún sitio, lo que permite que cada terminal tenga el suyo.
func TestNewModelMarksActiveTenant(t *testing.T) {
	ts := tenants()
	m := NewModel(ts, ts[1].ConfigDir)

	items := m.list.Items()
	if len(items) != 2 {
		t.Fatalf("%d items, quería 2", len(items))
	}
	if items[0].(TenantItem).active {
		t.Error("acme marcado como activo")
	}
	if !items[1].(TenantItem).active {
		t.Error("globex no marcado como activo")
	}
}

func TestNewModelNoActiveTenant(t *testing.T) {
	for _, dir := range []string{"", "/otro/sitio"} {
		m := NewModel(tenants(), dir)
		for _, it := range m.list.Items() {
			if it.(TenantItem).active {
				t.Errorf("con AZURE_CONFIG_DIR=%q hay un tenant marcado activo", dir)
			}
		}
	}
}

func TestSelectedIsNilUntilChosen(t *testing.T) {
	m := NewModel(tenants(), "")
	if got := m.Selected(); got != nil {
		t.Errorf("Selected() = %+v antes de elegir, quería nil", got)
	}
}

func TestSelectTenantMsgSetsSelection(t *testing.T) {
	ts := tenants()
	m := NewModel(ts, "")
	m, _ = send(t, m, selectTenantMsg{tenant: NewTenantItem(ts[1], false)})

	got := m.Selected()
	if got == nil {
		t.Fatal("Selected() = nil tras seleccionar")
	}
	if got.Name != "globex" {
		t.Errorf("Selected().Name = %q, quería «globex»", got.Name)
	}
	// Sale de la TUI y deja de pintar.
	if v := m.View(); v != "" {
		t.Errorf("View() = %q tras seleccionar, quería vacío", v)
	}
}

// Pulsar enter sobre un item debe emitir selectTenantMsg. Es el delegate quien
// lo produce, no el modelo.
func TestEnterEmitsSelectTenantMsg(t *testing.T) {
	m := NewModel(tenants(), "")
	_, cmd := send(t, m, keyMsg("enter"))
	if cmd == nil {
		t.Fatal("enter no emitió ningún comando")
	}
	if _, ok := cmd().(selectTenantMsg); !ok {
		t.Fatalf("enter emitió %T, quería selectTenantMsg", cmd())
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []tea.KeyMsg{keyMsg("q"), {Type: tea.KeyCtrlC}} {
		m := NewModel(tenants(), "")
		m, _ = send(t, m, k)
		if v := m.View(); v != "" {
			t.Errorf("tras %v, View() = %q, quería vacío", k, v)
		}
		if m.Selected() != nil {
			t.Errorf("tras %v hay selección, quería nil", k)
		}
	}
}

// Protege la guarda de Update: mientras se filtra, «q» es texto de búsqueda,
// no la orden de salir. Sin ella, buscar «quux» cerraría la aplicación.
func TestQuitKeyIsTextWhileFiltering(t *testing.T) {
	m := NewModel(tenants(), "")
	m, _ = send(t, m, keyMsg("/"))
	if m.list.FilterState() != list.Filtering {
		t.Fatalf("«/» no entró en modo filtro: estado = %v", m.list.FilterState())
	}

	m, _ = send(t, m, keyMsg("q"))
	if v := m.View(); v == "" {
		t.Error("«q» durante el filtrado cerró la TUI")
	}
	if m.Selected() != nil {
		t.Error("«q» durante el filtrado dejó una selección")
	}
}

// El filtro busca por nombre y por ID: pegar un GUID debe encontrar su tenant.
func TestFilterValueCoversNameAndID(t *testing.T) {
	item := NewTenantItem(tenants()[0], false)
	got := item.FilterValue()
	if !strings.Contains(got, "acme") {
		t.Errorf("FilterValue() = %q, quería que incluyera el nombre", got)
	}
	if !strings.Contains(got, "11111111-1111-1111-1111-111111111111") {
		t.Errorf("FilterValue() = %q, quería que incluyera el tenant ID", got)
	}
}

func TestWindowSizeMsgResizesList(t *testing.T) {
	m := NewModel(tenants(), "")
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if w := m.list.Width(); w >= 120 {
		t.Errorf("ancho de la lista = %d, quería descontar el margen de 120", w)
	}
	if h := m.list.Height(); h >= 40 {
		t.Errorf("alto de la lista = %d, quería descontar el margen de 40", h)
	}
}

// El delegate pinta dos líneas por tenant, nombre e ID, y marca el activo.
func TestDelegateRender(t *testing.T) {
	ts := tenants()
	m := NewModel(ts, ts[0].ConfigDir)
	d := newDelegate()

	if got := d.Height(); got != 2 {
		t.Errorf("Height() = %d, quería 2 (nombre + ID)", got)
	}

	var buf bytes.Buffer
	d.Render(&buf, m.list, 0, m.list.Items()[0])
	out := buf.String()
	if !strings.Contains(out, "acme") {
		t.Errorf("render = %q, quería el nombre", out)
	}
	if !strings.Contains(out, ts[0].TenantID) {
		t.Errorf("render = %q, quería el tenant ID", out)
	}
	if !strings.Contains(out, "*") {
		t.Errorf("render = %q, quería el marcador «*» del tenant activo", out)
	}

	buf.Reset()
	d.Render(&buf, m.list, 1, m.list.Items()[1])
	if out := buf.String(); strings.Contains(out, "*") {
		t.Errorf("render del tenant inactivo = %q, no quería marcador", out)
	}
}

// El marcador vive en un solo sitio. Antes existía dos veces —en Title() y
// reimplementado en el delegate— y solo se usaba la copia del delegate, así
// que las dos podían divergir sin que nada lo notara.
func TestMarker(t *testing.T) {
	ts := tenants()

	active := NewTenantItem(ts[0], true).marker()
	if !strings.Contains(active, "*") {
		t.Errorf("marcador activo = %q, quería que incluyera «*»", active)
	}
	inactive := NewTenantItem(ts[0], false).marker()
	if strings.Contains(inactive, "*") {
		t.Errorf("marcador inactivo = %q, no quería «*»", inactive)
	}
	// Ambos ocupan dos columnas para que los nombres queden alineados.
	if got := len(ansi.Strip(inactive)); got != 2 {
		t.Errorf("ancho del marcador inactivo = %d, quería 2", got)
	}
	if got := len(ansi.Strip(active)); got != 2 {
		t.Errorf("ancho del marcador activo = %d, quería 2", got)
	}
}

// Seleccionado o no, el ítem muestra los mismos datos. Lo que cambia es el
// adorno: el estilo seleccionado añade un borde izquierdo, que es contenido
// real y no un código de escape, así que no se pueden comparar las cadenas
// tal cual.
func TestDelegateRenderShowsSameDataSelectedOrNot(t *testing.T) {
	ts := tenants()
	m := NewModel(ts, "")
	d := newDelegate()

	var selected, normal bytes.Buffer
	d.Render(&selected, m.list, m.list.Index(), m.list.Items()[m.list.Index()])
	d.Render(&normal, m.list, m.list.Index()+1, m.list.Items()[m.list.Index()])

	for label, out := range map[string]string{
		"seleccionado": ansi.Strip(selected.String()),
		"normal":       ansi.Strip(normal.String()),
	} {
		if !strings.Contains(out, ts[0].Name) {
			t.Errorf("render %s = %q, quería el nombre", label, out)
		}
		if !strings.Contains(out, ts[0].TenantID) {
			t.Errorf("render %s = %q, quería el tenant ID", label, out)
		}
		if lines := strings.Count(out, "\n") + 1; lines != 2 {
			t.Errorf("render %s tiene %d líneas, quería 2 (nombre + ID)", label, lines)
		}
	}
}

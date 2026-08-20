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
	m := NewModel(ts, ts[1].ConfigDir, "", nil)

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
		m := NewModel(tenants(), dir, "", nil)
		for _, it := range m.list.Items() {
			if it.(TenantItem).active {
				t.Errorf("con AZURE_CONFIG_DIR=%q hay un tenant marcado activo", dir)
			}
		}
	}
}

func TestSelectedIsNilUntilChosen(t *testing.T) {
	m := NewModel(tenants(), "", "", nil)
	if got := m.Selected(); got != nil {
		t.Errorf("Selected() = %+v antes de elegir, quería nil", got)
	}
}

func TestSelectTenantMsgSetsSelection(t *testing.T) {
	ts := tenants()
	m := NewModel(ts, "", "", nil)
	m, _ = send(t, m, selectTenantMsg{tenant: NewTenantItem(ts[1], false, false)})

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
	m := NewModel(tenants(), "", "", nil)
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
		m := NewModel(tenants(), "", "", nil)
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
	m := NewModel(tenants(), "", "", nil)
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
	item := NewTenantItem(tenants()[0], false, false)
	got := item.FilterValue()
	if !strings.Contains(got, "acme") {
		t.Errorf("FilterValue() = %q, quería que incluyera el nombre", got)
	}
	if !strings.Contains(got, "11111111-1111-1111-1111-111111111111") {
		t.Errorf("FilterValue() = %q, quería que incluyera el tenant ID", got)
	}
}

func TestWindowSizeMsgResizesList(t *testing.T) {
	m := NewModel(tenants(), "", "", nil)
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
	m := NewModel(ts, ts[0].ConfigDir, "", nil)
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

	active := NewTenantItem(ts[0], true, false).marker()
	if !strings.Contains(active, "*") {
		t.Errorf("marcador activo = %q, quería que incluyera «*»", active)
	}
	inactive := NewTenantItem(ts[0], false, false).marker()
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
	m := NewModel(ts, "", "", nil)
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

// helpKeys devuelve las teclas visibles en la barra de ayuda. Las bindings
// deshabilitadas están en el slice pero el componente de ayuda no las pinta.
func helpKeys(m Model) []string {
	var keys []string
	for _, b := range m.list.ShortHelp() {
		if b.Enabled() {
			keys = append(keys, b.Help().Key)
		}
	}
	return keys
}

// list.Model.ShortHelp intercala las bindings del delegate entre las teclas
// de cursor y su propio KeyMap, que ya aporta Filter y Quit. Declararlas
// también en el delegate las imprimía dos veces.
func TestHelpHasNoDuplicateKeys(t *testing.T) {
	m := NewModel(tenants(), "", "", nil)

	seen := map[string]int{}
	for _, k := range helpKeys(m) {
		seen[k]++
	}
	for k, n := range seen {
		if n > 1 {
			t.Errorf("la tecla %q aparece %d veces en la ayuda: %v", k, n, helpKeys(m))
		}
	}
}

// El delegate solo debe aportar lo que el list no sabe. enter lo maneja él;
// «/» y «q» las pone el list por su cuenta.
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
			t.Errorf("falta %q en la ayuda: %v", want, keys)
		}
	}

	// El delegate declara solo las teclas que el list no conoce: enter y d.
	// «/» y «q» las aporta el list, y no deben repetirse (#21).
	declared := map[string]bool{}
	for _, b := range newDelegate().ShortHelp() {
		declared[b.Help().Key] = true
	}
	if !declared["enter"] || !declared["d"] {
		t.Errorf("el delegate debe declarar enter y d, tiene %v", declared)
	}
	if declared["/"] || declared["q"] {
		t.Errorf("el delegate no debe declarar / ni q (los pone el list): %v", declared)
	}
}

// Con el filtro abierto la barra cambia de forma: el list omite el ShortHelp
// del delegate. Tampoco ahí debe haber repeticiones.
func TestHelpHasNoDuplicateKeysWhileFiltering(t *testing.T) {
	m := NewModel(tenants(), "", "", nil)
	m, _ = send(t, m, keyMsg("/"))
	if m.list.FilterState() != list.Filtering {
		t.Fatalf("no se entró en modo filtro: %v", m.list.FilterState())
	}

	seen := map[string]int{}
	for _, k := range helpKeys(m) {
		seen[k]++
	}
	for k, n := range seen {
		if n > 1 {
			t.Errorf("filtrando, la tecla %q aparece %d veces: %v", k, n, helpKeys(m))
		}
	}
}

// Y la comprobación de extremo a extremo, sobre lo que el usuario ve.
func TestRenderedHelpHasNoRepeatedEntries(t *testing.T) {
	m := NewModel(tenants(), "", "", nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	view := ansi.Strip(updated.(Model).View())

	for _, entry := range []string{"/ filter", "q quit", "enter activate"} {
		if got := strings.Count(view, entry); got != 1 {
			t.Errorf("%q aparece %d veces en la vista, quería 1:\n%s", entry, got, view)
		}
	}
}

// El marcador distingue las cuatro combinaciones de activo y default, y
// mantiene dos columnas en todas para que los nombres queden alineados.
func TestMarkerDefaultAndActive(t *testing.T) {
	ts := tenants()
	cases := []struct {
		name            string
		active, isDef   bool
		wantStar, wantD bool
	}{
		{"ninguno", false, false, false, false},
		{"solo activo", true, false, true, false},
		{"solo default", false, true, false, true},
		{"ambos", true, true, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := ansi.Strip(NewTenantItem(ts[0], c.active, c.isDef).marker())
			if strings.Contains(m, "*") != c.wantStar {
				t.Errorf("marcador %q: presencia de «*» = %v, quería %v", m, strings.Contains(m, "*"), c.wantStar)
			}
			if strings.Contains(m, "D") != c.wantD {
				t.Errorf("marcador %q: presencia de «D» = %v, quería %v", m, strings.Contains(m, "D"), c.wantD)
			}
			if len(m) != 2 {
				t.Errorf("marcador %q mide %d columnas, quería 2", m, len(m))
			}
		})
	}
}

// NewModel marca como default el tenant cuyo nombre coincide, sin distinguir
// mayúsculas, y ninguno si el nombre está vacío.
func TestNewModelMarksDefault(t *testing.T) {
	ts := tenants() // acme, globex
	m := NewModel(ts, "", "GLOBEX", nil)
	items := m.list.Items()
	if items[0].(TenantItem).isDefault {
		t.Error("acme marcado como default")
	}
	if !items[1].(TenantItem).isDefault {
		t.Error("globex no marcado como default pese a coincidir (case-insensitive)")
	}

	none := NewModel(ts, "", "", nil)
	for _, it := range none.list.Items() {
		if it.(TenantItem).isDefault {
			t.Error("hay un default marcado con defaultName vacío")
		}
	}
}

// Activo y default son independientes: el render los muestra a la vez cuando
// coinciden en el mismo tenant, y por separado cuando no.
func TestDelegateRendersDefaultMarker(t *testing.T) {
	ts := tenants()
	// acme activo, globex default.
	m := NewModel(ts, ts[0].ConfigDir, "globex", nil)
	d := newDelegate()

	var acme, globex bytes.Buffer
	d.Render(&acme, m.list, 0, m.list.Items()[0])
	d.Render(&globex, m.list, 1, m.list.Items()[1])

	if a := ansi.Strip(acme.String()); !strings.Contains(a, "*") || strings.Contains(a[:3], "D") {
		t.Errorf("acme debería ser activo y no default: %q", a)
	}
	if g := ansi.Strip(globex.String()); !strings.Contains(g[:3], "D") {
		t.Errorf("globex debería mostrar el marcador de default: %q", g)
	}
}

// La tecla "d" pide confirmación antes de fijar el default, porque reescribe
// ~/.azure — la única acción de la TUI con efecto en disco.
func TestSetDefaultKeyAsksConfirmation(t *testing.T) {
	ts := tenants()
	var called []string
	set := func(name string) (string, error) { called = append(called, name); return "", nil }
	m := NewModel(ts, "", "", set)

	// d sobre el primer item entra en confirmación, sin fijar aún.
	m, _ = send(t, m, keyMsg("d"))
	if !strings.Contains(m.View(), "as the default?") {
		t.Errorf("d no mostró la confirmación:\n%s", ansi.Strip(m.View()))
	}
	if len(called) != 0 {
		t.Errorf("se fijó el default antes de confirmar: %v", called)
	}

	// y confirma: se fija y el marcador se actualiza.
	m, _ = send(t, m, keyMsg("y"))
	if len(called) != 1 || called[0] != ts[0].Name {
		t.Fatalf("setDefault llamado con %v, quería [%s]", called, ts[0].Name)
	}
	if !m.list.Items()[0].(TenantItem).isDefault {
		t.Error("el marcador de default no se actualizó tras confirmar")
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
		t.Errorf("n no canceló: se fijó %v", called)
	}
	if strings.Contains(m.View(), "as the default?") {
		t.Error("sigue mostrando la confirmación tras cancelar")
	}
}

// Un error al fijar se muestra, no se traga.
func TestSetDefaultKeyReportsError(t *testing.T) {
	ts := tenants()
	set := func(name string) (string, error) { return "", errTest }
	m := NewModel(ts, "", "", set)
	m, _ = send(t, m, keyMsg("d"))
	m, _ = send(t, m, keyMsg("y"))
	if !strings.Contains(m.View(), "Could not set default") {
		t.Errorf("no se mostró el error:\n%s", ansi.Strip(m.View()))
	}
}

// Sin callback (setDefault nil), la tecla d no hace nada.
func TestSetDefaultKeyNoopWithoutCallback(t *testing.T) {
	m := NewModel(tenants(), "", "", nil)
	m, _ = send(t, m, keyMsg("d"))
	if strings.Contains(m.View(), "as the default?") {
		t.Error("d entró en confirmación sin callback")
	}
}

// Durante el filtrado, "d" es texto de búsqueda, no la orden de fijar default
// — la misma guarda que protege a "q" (#7).
func TestSetDefaultKeyIsTextWhileFiltering(t *testing.T) {
	ts := tenants()
	var called []string
	set := func(name string) (string, error) { called = append(called, name); return "", nil }
	m := NewModel(ts, "", "", set)

	m, _ = send(t, m, keyMsg("/"))
	if m.list.FilterState() != list.Filtering {
		t.Fatalf("no se entró en filtrado")
	}
	m, _ = send(t, m, keyMsg("d"))
	if strings.Contains(m.View(), "as the default?") {
		t.Error("d abrió la confirmación durante el filtrado")
	}
	if len(called) != 0 {
		t.Errorf("d fijó el default durante el filtrado: %v", called)
	}
}

var errTest = &testError{}

type testError struct{}

func (*testError) Error() string { return "boom" }

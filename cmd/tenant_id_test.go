package cmd

import (
	"strings"
	"testing"
)

func TestNormalizeTenantIDAccepts(t *testing.T) {
	cases := []struct {
		in   string
		want string
		why  string
	}{
		{"11111111-1111-1111-1111-111111111111", "11111111-1111-1111-1111-111111111111", "GUID en minúsculas"},
		{"AABBCCDD-1122-3344-5566-778899AABBCC", "aabbccdd-1122-3344-5566-778899aabbcc", "GUID en mayúsculas, normalizado"},
		{"  11111111-1111-1111-1111-111111111111  ", "11111111-1111-1111-1111-111111111111", "espacios alrededor"},
		{"11111111-1111-1111-1111-111111111111\n", "11111111-1111-1111-1111-111111111111", "salto de línea del prompt"},

		// az login --tenant también acepta un dominio verificado del tenant.
		{"contoso.onmicrosoft.com", "contoso.onmicrosoft.com", "dominio onmicrosoft"},
		{"CONTOSO.ONMICROSOFT.COM", "contoso.onmicrosoft.com", "dominio en mayúsculas"},
		{"contoso.com", "contoso.com", "dominio propio"},
		{"my-tenant.example.co.uk", "my-tenant.example.co.uk", "guiones y varios niveles"},
	}
	for _, c := range cases {
		got, err := normalizeTenantID(c.in)
		if err != nil {
			t.Errorf("normalizeTenantID(%q) = error %v, quería aceptarlo (%s)", c.in, err, c.why)
			continue
		}
		if got != c.want {
			t.Errorf("normalizeTenantID(%q) = %q, quería %q (%s)", c.in, got, c.want, c.why)
		}
	}
}

func TestNormalizeTenantIDRejects(t *testing.T) {
	cases := []struct {
		in  string
		why string
	}{
		{"", "vacío"},
		{"   ", "solo espacios"},
		{"contoso", "nombre suelto, sin punto ni forma de GUID"},
		{"11111111-1111-1111-1111-11111111111", "GUID con un dígito de menos"},
		{"11111111-1111-1111-1111-1111111111111", "GUID con un dígito de más"},
		{"11111111-1111-1111-111111111111111", "faltan guiones"},
		{"gggggggg-1111-1111-1111-111111111111", "caracteres fuera de hexadecimal"},
		{"11111111 1111 1111 1111 111111111111", "espacios en vez de guiones"},
		{"-contoso.com", "etiqueta que empieza por guión"},
		{"contoso-.com", "etiqueta que acaba en guión"},
		{"contoso..com", "etiqueta vacía"},
		{"contoso.com.", "punto final"},
		{".contoso.com", "punto inicial"},
		{"https://contoso.onmicrosoft.com", "una URL, no un dominio"},
		{"contoso onmicrosoft com", "espacios"},
	}
	for _, c := range cases {
		if got, err := normalizeTenantID(c.in); err == nil {
			t.Errorf("normalizeTenantID(%q) = %q, quería rechazarlo (%s)", c.in, got, c.why)
		}
	}
}

// El mensaje tiene que decir qué se esperaba, no solo que está mal: el usuario
// acaba de pegar algo y necesita saber qué formato quiere azsel.
func TestNormalizeTenantIDErrorNamesBothFormats(t *testing.T) {
	_, err := normalizeTenantID("no-soy-un-tenant")
	if err == nil {
		t.Fatal("se aceptó un tenant ID inválido")
	}
	got := err.Error()
	for _, want := range []string{"no-soy-un-tenant", "GUID", "domain"} {
		if !strings.Contains(got, want) {
			t.Errorf("error = %q, quería que mencionara %q", got, want)
		}
	}
}

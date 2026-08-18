package cmd

import "testing"

// nameRegex decide qué nombres de tenant se aceptan. El nombre acaba siendo
// un directorio bajo ~/.azsel/tenants/, así que la restricción no es
// cosmética: filtra separadores de ruta y espacios.
func TestNameRegex(t *testing.T) {
	cases := []struct {
		name  string
		valid bool
		why   string
	}{
		{"a", true, "un solo carácter"},
		{"acme", true, "minúsculas"},
		{"acme-corp", true, "guión interior"},
		{"client-1", true, "dígitos"},
		{"1", true, "solo dígito"},
		{"a-b-c-d", true, "varios guiones"},

		{"", false, "vacío"},
		{"-acme", false, "empieza por guión"},
		{"acme-", false, "acaba en guión"},
		{"-", false, "solo un guión"},
		{"ACME", false, "mayúsculas"},
		{"Acme", false, "mayúscula inicial"},
		{"acme_corp", false, "guión bajo"},
		{"acme corp", false, "espacio"},
		{"acme.corp", false, "punto"},
		{"acme/corp", false, "separador de ruta"},
		{"..", false, "recorrido de directorios"},
		{"acmé", false, "carácter acentuado"},
		{"acme\n", false, "salto de línea"},
	}

	for _, c := range cases {
		if got := nameRegex.MatchString(c.name); got != c.valid {
			t.Errorf("nameRegex(%q) = %v, quería %v (%s)", c.name, got, c.valid, c.why)
		}
	}
}

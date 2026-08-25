package cmd

import "testing"

// nameRegex decides which tenant names are accepted. The name ends up being
// a directory under ~/.azsel/tenants/, so the restriction is not cosmetic:
// it filters out path separators and spaces.
func TestNameRegex(t *testing.T) {
	cases := []struct {
		name  string
		valid bool
		why   string
	}{
		{"a", true, "a single character"},
		{"acme", true, "lowercase"},
		{"acme-corp", true, "interior hyphen"},
		{"client-1", true, "digits"},
		{"1", true, "digit only"},
		{"a-b-c-d", true, "several hyphens"},

		{"", false, "empty"},
		{"-acme", false, "starts with a hyphen"},
		{"acme-", false, "ends in a hyphen"},
		{"-", false, "just a hyphen"},
		{"ACME", false, "uppercase"},
		{"Acme", false, "leading uppercase"},
		{"acme_corp", false, "underscore"},
		{"acme corp", false, "space"},
		{"acme.corp", false, "dot"},
		{"acme/corp", false, "path separator"},
		{"..", false, "directory traversal"},
		{"acmé", false, "accented character"},
		{"acme\n", false, "newline"},
	}

	for _, c := range cases {
		if got := nameRegex.MatchString(c.name); got != c.valid {
			t.Errorf("nameRegex(%q) = %v, wanted %v (%s)", c.name, got, c.valid, c.why)
		}
	}
}

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
		{"11111111-1111-1111-1111-111111111111", "11111111-1111-1111-1111-111111111111", "lowercase GUID"},
		{"AABBCCDD-1122-3344-5566-778899AABBCC", "aabbccdd-1122-3344-5566-778899aabbcc", "uppercase GUID, normalized"},
		{"  11111111-1111-1111-1111-111111111111  ", "11111111-1111-1111-1111-111111111111", "surrounding spaces"},
		{"11111111-1111-1111-1111-111111111111\n", "11111111-1111-1111-1111-111111111111", "trailing newline from the prompt"},

		// az login --tenant also accepts a verified tenant domain.
		{"contoso.onmicrosoft.com", "contoso.onmicrosoft.com", "onmicrosoft domain"},
		{"CONTOSO.ONMICROSOFT.COM", "contoso.onmicrosoft.com", "uppercase domain"},
		{"contoso.com", "contoso.com", "custom domain"},
		{"my-tenant.example.co.uk", "my-tenant.example.co.uk", "hyphens and multiple levels"},
	}
	for _, c := range cases {
		got, err := normalizeTenantID(c.in)
		if err != nil {
			t.Errorf("normalizeTenantID(%q) = error %v, wanted it accepted (%s)", c.in, err, c.why)
			continue
		}
		if got != c.want {
			t.Errorf("normalizeTenantID(%q) = %q, wanted %q (%s)", c.in, got, c.want, c.why)
		}
	}
}

func TestNormalizeTenantIDRejects(t *testing.T) {
	cases := []struct {
		in  string
		why string
	}{
		{"", "empty"},
		{"   ", "only spaces"},
		{"contoso", "bare name, no dot and not GUID-shaped"},
		{"11111111-1111-1111-1111-11111111111", "GUID one digit short"},
		{"11111111-1111-1111-1111-1111111111111", "GUID one digit too many"},
		{"11111111-1111-1111-111111111111111", "missing hyphens"},
		{"gggggggg-1111-1111-1111-111111111111", "non-hexadecimal characters"},
		{"11111111 1111 1111 1111 111111111111", "spaces instead of hyphens"},
		{"-contoso.com", "label starting with a hyphen"},
		{"contoso-.com", "label ending in a hyphen"},
		{"contoso..com", "empty label"},
		{"contoso.com.", "trailing dot"},
		{".contoso.com", "leading dot"},
		{"https://contoso.onmicrosoft.com", "a URL, not a domain"},
		{"contoso onmicrosoft com", "spaces"},
	}
	for _, c := range cases {
		if got, err := normalizeTenantID(c.in); err == nil {
			t.Errorf("normalizeTenantID(%q) = %q, wanted it rejected (%s)", c.in, got, c.why)
		}
	}
}

// The message has to say what was expected, not just that it's wrong: the user
// has just pasted something and needs to know what format azsel wants.
func TestNormalizeTenantIDErrorNamesBothFormats(t *testing.T) {
	_, err := normalizeTenantID("not-a-tenant")
	if err == nil {
		t.Fatal("an invalid tenant ID was accepted")
	}
	got := err.Error()
	for _, want := range []string{"not-a-tenant", "GUID", "domain"} {
		if !strings.Contains(got, want) {
			t.Errorf("error = %q, wanted it to mention %q", got, want)
		}
	}
}

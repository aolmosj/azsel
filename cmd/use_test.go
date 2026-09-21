package cmd

import (
	"strings"
	"testing"
)

// use still switches, but notes when the tenant's session has expired — you are
// switching in order to use it.
func TestUseWarnsOnExpiredSession(t *testing.T) {
	listSandbox(t, "contoso")
	fakeAzureCLI(t, `case "$*" in
  *get-access-token*) exit 1 ;;
  *) echo '{"value":[]}' ;;
esac`)
	out := quiet(t)
	if err := run(t, newUseCmd(), "contoso"); err != nil {
		t.Fatalf("use: %v", err)
	}
	got := out()
	if !strings.Contains(got, "Switched") {
		t.Errorf("use should still switch: %q", got)
	}
	if !strings.Contains(got, "expired") || !strings.Contains(got, "azsel login") {
		t.Errorf("use should warn about the expired session: %q", got)
	}
}

func TestUseNoWarnWhenSessionValid(t *testing.T) {
	listSandbox(t, "contoso")
	fakeAzureCLI(t, `echo "2026-12-31T00:00:00"`) // get-access-token succeeds
	out := quiet(t)
	if err := run(t, newUseCmd(), "contoso"); err != nil {
		t.Fatalf("use: %v", err)
	}
	if strings.Contains(out(), "expired") {
		t.Errorf("use warned about a valid session: %q", out())
	}
}

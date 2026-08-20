package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

// The service-principal flag matrix, rejected before any prompt or disk work.
func TestValidateAddFlags(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		tenant  string
		sp      bool
		device  bool
		user    string
		cert    string
		pwStdin bool
		wantErr string
	}{
		{name: "interactive, no sp flags", wantErr: ""},
		{name: "sp flags without --service-principal", user: "app", wantErr: "require --service-principal"},
		{name: "cert without --service-principal", cert: "c.pem", wantErr: "require --service-principal"},
		{name: "sp ok with cert", args: []string{"acme"}, tenant: "t", sp: true, user: "app", cert: "c.pem", wantErr: ""},
		{name: "sp ok with password-stdin", args: []string{"acme"}, tenant: "t", sp: true, user: "app", pwStdin: true, wantErr: ""},
		{name: "sp with device-code", args: []string{"acme"}, tenant: "t", sp: true, user: "app", cert: "c.pem", device: true, wantErr: "cannot be combined with --device-code"},
		{name: "sp without name arg", tenant: "t", sp: true, user: "app", cert: "c.pem", wantErr: "pass the tenant name"},
		{name: "sp without --tenant", args: []string{"acme"}, sp: true, user: "app", cert: "c.pem", wantErr: "pass --tenant"},
		{name: "sp without username", args: []string{"acme"}, tenant: "t", sp: true, cert: "c.pem", wantErr: "requires --username"},
		{name: "sp with neither cert nor stdin", args: []string{"acme"}, tenant: "t", sp: true, user: "app", wantErr: "exactly one of --certificate or --password-stdin"},
		{name: "sp with both cert and stdin", args: []string{"acme"}, tenant: "t", sp: true, user: "app", cert: "c.pem", pwStdin: true, wantErr: "exactly one of --certificate or --password-stdin"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateAddFlags(c.args, c.tenant, c.sp, c.device, c.user, c.cert, c.pwStdin)
			if c.wantErr == "" {
				if err != nil {
					t.Errorf("wanted no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error = %v, wanted it to contain %q", err, c.wantErr)
			}
		})
	}
}

// End-to-end: a service-principal add runs az with the right flags. The fake
// az records its argv so we can assert the passthrough.
func TestAddServicePrincipalEndToEnd(t *testing.T) {
	home := addSandbox(t)
	argfile := filepath.Join(home, "az-args")
	// The fake az writes its own arguments (one per line) to a file.
	fakeAzureCLI(t, `printf '%s\n' "$@" > "`+argfile+`"; exit 0`)
	cert := filepath.Join(t.TempDir(), "cert.pem")
	if err := os.WriteFile(cert, []byte("PEM"), 0600); err != nil {
		t.Fatalf("prep: %v", err)
	}
	quiet(t)

	err := run(t, newAddCmd(),
		"acme", "--tenant", testTenantID, "--service-principal", "-u", "app-123", "--certificate", cert)
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	data, err := os.ReadFile(argfile)
	if err != nil {
		t.Fatalf("az was not invoked: %v", err)
	}
	got := string(data)
	for _, want := range []string{"login", "--service-principal", "--username", "app-123", "--certificate", cert, "--tenant", testTenantID} {
		if !strings.Contains(got, want) {
			t.Errorf("az args missing %q:\n%s", want, got)
		}
	}
	// It was saved.
	cfg, _ := config.Load()
	if cfg.FindTenant("acme") == nil {
		t.Error("tenant not saved after service-principal login")
	}
}

// The secret is read from stdin and handed to az; azsel never puts it in a flag.
func TestAddServicePrincipalSecretFromStdin(t *testing.T) {
	home := addSandbox(t)
	argfile := filepath.Join(home, "az-args")
	fakeAzureCLI(t, `printf '%s\n' "$@" > "`+argfile+`"; exit 0`)
	feedStdin(t, "the-secret\n")
	quiet(t)

	err := run(t, newAddCmd(),
		"acme", "--tenant", testTenantID, "--service-principal", "-u", "app-123", "--password-stdin")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	got, err := os.ReadFile(argfile)
	if err != nil {
		t.Fatalf("az was not invoked: %v", err)
	}
	if !strings.Contains(string(got), "--password\nthe-secret") {
		t.Errorf("secret not passed to az as --password:\n%s", got)
	}
}

// An empty stdin with --password-stdin is a clear error, not a login with an
// empty secret.
func TestAddServicePrincipalEmptyStdin(t *testing.T) {
	addSandbox(t)
	fakeAzureCLI(t, "exit 0")
	feedStdin(t, "")
	quiet(t)

	err := run(t, newAddCmd(),
		"acme", "--tenant", testTenantID, "--service-principal", "-u", "app", "--password-stdin")
	if err == nil || !strings.Contains(err.Error(), "stdin was empty") {
		t.Errorf("error = %v, wanted an empty-stdin error", err)
	}
}

// A missing certificate file fails fast, before az runs.
func TestAddServicePrincipalMissingCertificate(t *testing.T) {
	home := addSandbox(t)
	sentinel := filepath.Join(home, "az-ran")
	fakeAzureCLI(t, `: > "`+sentinel+`"; exit 0`)
	quiet(t)

	err := run(t, newAddCmd(),
		"acme", "--tenant", testTenantID, "--service-principal", "-u", "app", "--certificate", filepath.Join(home, "nope.pem"))
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Errorf("error = %v, wanted a certificate error", err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Error("az was invoked despite the missing certificate")
	}
}

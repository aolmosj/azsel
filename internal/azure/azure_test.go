package azure

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// capture holds the command handed to run, so a test can inspect what azsel
// built without anything being executed.
type capture struct{ cmd *exec.Cmd }

// stubRun replaces the executor for one test. fn may be nil to succeed.
func stubRun(t *testing.T, fn func(*exec.Cmd) error) *capture {
	t.Helper()
	c := &capture{}
	orig := run
	run = func(cmd *exec.Cmd) error {
		c.cmd = cmd
		if fn == nil {
			return nil
		}
		return fn(cmd)
	}
	t.Cleanup(func() { run = orig })
	return c
}

func stubLookPath(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	orig := lookPath
	lookPath = fn
	t.Cleanup(func() { lookPath = orig })
}

// envValue reports the value the subprocess will actually see. os/exec
// deduplicates Env keeping the *last* occurrence, so a key inherited from the
// parent and then overridden appears twice and the later one wins. Reading
// the first match instead is how this helper originally got it wrong: it
// passed on macOS, where nothing pre-sets these, and failed on a CI runner
// that ships with AZURE_EXTENSION_DIR already exported.
func envValue(cmd *exec.Cmd, key string) (string, bool) {
	value, found := "", false
	for _, kv := range cmd.Env {
		if name, v, ok := strings.Cut(kv, "="); ok && name == key {
			value, found = v, true
		}
	}
	return value, found
}

func TestLoginArguments(t *testing.T) {
	cases := []struct {
		name       string
		deviceCode bool
		want       []string
	}{
		{"browser", false, []string{"az", "login", "--tenant", "TID"}},
		{"device code", true, []string{"az", "login", "--tenant", "TID", "--use-device-code"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stubRun(t, nil)
			if err := Login("TID", "/cfg", c.deviceCode); err != nil {
				t.Fatalf("Login: %v", err)
			}
			if !slices.Equal(got.cmd.Args, c.want) {
				t.Errorf("args = %v, wanted %v", got.cmd.Args, c.want)
			}
		})
	}
}

// Isolation between tenants rests entirely on these two variables.
func TestLoginScopesTheEnvironment(t *testing.T) {
	// One snapshot of the environment, taken here, is the baseline for both
	// what command() inherits and what the test measures — reading
	// os.Environ() a second time to slice the result could drift (or panic)
	// if anything mutated the environment in between.
	base := os.Environ()
	got := stubRun(t, nil)
	if err := Login("TID", "/cfg/acme", false); err != nil {
		t.Fatalf("Login: %v", err)
	}

	if v, ok := envValue(got.cmd, "AZURE_CONFIG_DIR"); !ok || v != "/cfg/acme" {
		t.Errorf("AZURE_CONFIG_DIR = %q (present=%v), wanted /cfg/acme", v, ok)
	}

	// azsel must add exactly one variable to the inherited environment:
	// AZURE_CONFIG_DIR. Checking "AZURE_EXTENSION_DIR absent" would be false
	// on a runner that already exports it (Linux does) — what matters is that
	// azsel does not set it.
	if len(got.cmd.Env) < len(base) {
		t.Fatalf("command() shrank the environment: %d < %d", len(got.cmd.Env), len(base))
	}
	added := got.cmd.Env[len(base):]
	if len(added) != 1 || added[0] != "AZURE_CONFIG_DIR=/cfg/acme" {
		t.Errorf("azsel added %v, wanted exactly [AZURE_CONFIG_DIR=/cfg/acme]", added)
	}

	// The rest of the environment is inherited: az needs proxy, locale, HOME…
	if _, ok := envValue(got.cmd, "PATH"); !ok {
		t.Error("the process environment was not inherited")
	}
}

// Two deliberate behaviors, easy to break by accident: login is interactive,
// so stdin must reach az; and az's output goes to stderr because azsel keeps
// stdout clean for the shell.
func TestLoginWiresStreams(t *testing.T) {
	got := stubRun(t, nil)
	if err := Login("TID", "/cfg", false); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got.cmd.Stdin != os.Stdin {
		t.Error("stdin is not connected: interactive login would not work")
	}
	if got.cmd.Stdout != os.Stderr {
		t.Error("az's stdout does not go to stderr: it would pollute standard output")
	}
	if got.cmd.Stderr != os.Stderr {
		t.Error("az's stderr does not go to stderr")
	}
}

func TestLoginPropagatesFailure(t *testing.T) {
	want := errors.New("the user cancelled")
	stubRun(t, func(*exec.Cmd) error { return want })

	err := Login("TID", "/cfg", false)
	if !errors.Is(err, want) {
		t.Errorf("error = %v, wanted %v", err, want)
	}
}

func TestAvailable(t *testing.T) {
	t.Run("az present", func(t *testing.T) {
		stubLookPath(t, func(string) (string, error) { return "/usr/local/bin/az", nil })
		if err := Available(); err != nil {
			t.Errorf("Available: %v", err)
		}
	})

	t.Run("az absent", func(t *testing.T) {
		stubLookPath(t, func(name string) (string, error) {
			return "", fmt.Errorf("exec: %q: executable file not found in $PATH", name)
		})
		err := Available()
		if err == nil {
			t.Fatal("Available returned nil with az absent from PATH")
		}
		if !strings.Contains(err.Error(), "Azure CLI") {
			t.Errorf("error = %q, wanted it to name Azure CLI", err)
		}
	})

	t.Run("the correct binary is queried", func(t *testing.T) {
		var asked string
		stubLookPath(t, func(name string) (string, error) {
			asked = name
			return "/usr/local/bin/az", nil
		})
		if err := Available(); err != nil {
			t.Fatalf("Available: %v", err)
		}
		if asked != "az" {
			t.Errorf("looked up %q, wanted az", asked)
		}
	})
}

// azsel's whole isolation model rests on overriding these two variables, and
// they may well already be exported — by azsel itself in an active session,
// or by an Azure CLI install like the one on GitHub's Linux runners. Whatever
// azsel appends has to win.
func TestCommandEnvOverridesInheritedValues(t *testing.T) {
	t.Setenv("AZURE_CONFIG_DIR", "/heredado/config")

	got := stubRun(t, nil)
	if err := Login("TID", "/cfg/acme", false); err != nil {
		t.Fatalf("Login: %v", err)
	}

	if v, _ := envValue(got.cmd, "AZURE_CONFIG_DIR"); v != "/cfg/acme" {
		t.Errorf("effective AZURE_CONFIG_DIR = %q, wanted /cfg/acme", v)
	}
}

func TestLoginServicePrincipalWithCertificate(t *testing.T) {
	got := stubRun(t, nil)
	if err := LoginServicePrincipal("TID", "/cfg", "app-id", "/path/cert.pem", ""); err != nil {
		t.Fatalf("LoginServicePrincipal: %v", err)
	}
	want := []string{"az", "login", "--service-principal", "--tenant=TID", "--username=app-id", "--certificate=/path/cert.pem"}
	if !slices.Equal(got.cmd.Args, want) {
		t.Errorf("args = %v, wanted %v", got.cmd.Args, want)
	}
	// Non-interactive: stdin is not connected.
	if got.cmd.Stdin != nil {
		t.Error("stdin connected for a service-principal login; should be nil")
	}
}

func TestLoginServicePrincipalWithSecret(t *testing.T) {
	got := stubRun(t, nil)
	if err := LoginServicePrincipal("TID", "/cfg", "app-id", "", "s3cr3t"); err != nil {
		t.Fatalf("LoginServicePrincipal: %v", err)
	}
	want := []string{"az", "login", "--service-principal", "--tenant=TID", "--username=app-id", "--password=s3cr3t"}
	if !slices.Equal(got.cmd.Args, want) {
		t.Errorf("args = %v, wanted %v", got.cmd.Args, want)
	}
}

// The tenant's config directory is still scoped via AZURE_CONFIG_DIR.
func TestLoginServicePrincipalScopesConfigDir(t *testing.T) {
	got := stubRun(t, nil)
	if err := LoginServicePrincipal("TID", "/cfg/acme", "app", "", "x"); err != nil {
		t.Fatalf("LoginServicePrincipal: %v", err)
	}
	if v, ok := envValue(got.cmd, "AZURE_CONFIG_DIR"); !ok || v != "/cfg/acme" {
		t.Errorf("AZURE_CONFIG_DIR = %q (present=%v), wanted /cfg/acme", v, ok)
	}
}

// A secret starting with '-' must be one token joined with '=', or az's
// argparse would read it as a flag and the login would fail.
func TestLoginServicePrincipalSecretStartingWithDash(t *testing.T) {
	got := stubRun(t, nil)
	if err := LoginServicePrincipal("TID", "/cfg", "app", "", "-leadingdash"); err != nil {
		t.Fatalf("LoginServicePrincipal: %v", err)
	}
	for _, a := range got.cmd.Args {
		if a == "--password" {
			t.Fatal("secret passed as a separate token; a '-' secret would misparse")
		}
	}
	if !slices.Contains(got.cmd.Args, "--password=-leadingdash") {
		t.Errorf("args = %v, wanted a single --password=-leadingdash token", got.cmd.Args)
	}
}

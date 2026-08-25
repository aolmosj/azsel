package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
	"github.com/spf13/cobra"
)

// withoutAzureCLI points PATH at an empty directory, so exec.LookPath cannot
// find az. Real rather than stubbed: azure.Available's seam is unexported and
// this is what the user's machine actually looks like.
func withoutAzureCLI(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	t.Setenv("PATH", t.TempDir())
	return home
}

// quiet swallows the commands' direct writes to the process streams and
// returns a function reading back everything they wrote. The commands write
// straight to os.Stdout/os.Stderr rather than through cobra, so redirecting
// the process streams is the only way to see their output.
func quiet(t *testing.T) func() string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "output")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating the output file: %v", err)
	}
	outOrig, errOrig := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = f, f
	t.Cleanup(func() {
		os.Stdout, os.Stderr = outOrig, errOrig
		_ = f.Close()
	})
	return func() string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading the output: %v", err)
		}
		return string(data)
	}
}

func run(t *testing.T, c *cobra.Command, args ...string) error {
	t.Helper()
	c.SilenceErrors, c.SilenceUsage = true, true
	c.SetOut(io.Discard)
	c.SetErr(io.Discard)
	c.SetArgs(args)
	return c.Execute()
}

// The cost of az being missing should be the message and nothing more. It
// used to be that the user typed name and tenant ID, the tenant directory was
// created, and only then it blew up with Go's raw error.
func TestAddFailsBeforeAskingAnything(t *testing.T) {
	home := withoutAzureCLI(t)
	quiet(t)

	// What the user would have typed if they had been asked.
	f := feedStdin(t, "acme\n11111111-1111-1111-1111-111111111111\n")

	err := run(t, newAddCmd())
	if err == nil {
		t.Fatal("add returned nil without az installed")
	}
	if !strings.Contains(err.Error(), "Azure CLI") {
		t.Errorf("error = %q, wanted it to name Azure CLI", err)
	}
	if !strings.Contains(err.Error(), "install") {
		t.Errorf("error = %q, wanted it to say how to install it", err)
	}

	// Nothing read from stdin: it never got as far as asking.
	if pos, _ := f.Seek(0, io.SeekCurrent); pos != 0 {
		t.Errorf("stdin was consumed before checking az (offset %d)", pos)
	}
	// And nothing created on disk.
	if entries, err := os.ReadDir(filepath.Join(home, "tenants")); err == nil && len(entries) > 0 {
		t.Errorf("%d tenant directories were created despite the failure", len(entries))
	}
}

// Only add needs az. The rest must keep working on a machine without the
// Azure CLI — listing tenants or switching between them invokes nothing.
func TestCommandsThatDoNotNeedAzureCLI(t *testing.T) {
	home := withoutAzureCLI(t)
	quiet(t)

	tenantDir := filepath.Join(home, "tenants", "acme")
	if err := os.MkdirAll(tenantDir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cfg := &config.Config{Tenants: []config.Tenant{
		{Name: "acme", TenantID: "11111111-1111-1111-1111-111111111111", ConfigDir: tenantDir},
	}}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cases := []struct {
		name string
		cmd  *cobra.Command
		args []string
	}{
		{"list", newListCmd(), nil},
		{"use", newUseCmd(), []string{"acme"}},
		{"init --print", newInitCmd(), []string{"--print"}},
		{"remove -f", newRemoveCmd(), []string{"acme", "-f"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := run(t, c.cmd, c.args...); err != nil {
				t.Errorf("%s failed without az installed: %v", c.name, err)
			}
		})
	}
}

// feedStdin connects os.Stdin to a file with what the user would type, and
// returns the descriptor so we can check how much has been read.
func feedStdin(t *testing.T, content string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("setting up stdin: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening the fake stdin: %v", err)
	}
	orig := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = orig
		_ = f.Close()
	})
	return f
}

// fakeAzureCLI puts a fake az in front of the PATH with the given body. It
// lets us exercise the real path —Available finds it, Login runs it— without
// seams and without touching Azure.
//
// It's prepended rather than replacing the PATH: replacing it left the script
// without basic utilities, so that a body like `touch sentinel` failed
// silently with «command not found» and any assertion about the sentinel came
// out empty. Prepending is enough for the fake az to win.
func fakeAzureCLI(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "az"), []byte("#!/bin/sh\n"+body+"\n"), 0755); err != nil {
		t.Fatalf("writing the fake az: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectShellRC(t *testing.T) {
	cases := []struct {
		name       string
		shell      string
		makeBashrc bool
		wantRC     string // relative to home; "" = unsupported
		wantShell  string
	}{
		{"zsh", "/bin/zsh", false, ".zshrc", "zsh"},
		{"zsh at another path", "/opt/homebrew/bin/zsh", false, ".zshrc", "zsh"},
		{"bash with bashrc", "/bin/bash", true, ".bashrc", "bash"},
		{"bash without bashrc", "/bin/bash", false, ".bash_profile", "bash"},
		{"fish", "/opt/homebrew/bin/fish", false, "", "fish"},
		{"sh", "/bin/sh", false, "", "sh"},
		{"empty SHELL", "", false, "", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("SHELL", c.shell)
			if c.makeBashrc {
				if err := os.WriteFile(filepath.Join(home, ".bashrc"), nil, 0644); err != nil {
					t.Fatalf("setup: %v", err)
				}
			}

			rc, shell, err := detectShellRC()
			if err != nil {
				t.Fatalf("detectShellRC: %v", err)
			}
			want := ""
			if c.wantRC != "" {
				want = filepath.Join(home, c.wantRC)
			}
			if rc != want {
				t.Errorf("rcFile = %q, wanted %q", rc, want)
			}
			if shell != c.wantShell {
				t.Errorf("shellName = %q, wanted %q", shell, c.wantShell)
			}
		})
	}
}

// The previous message said "add this manually to your shell rc", which is
// false for fish: the function uses bash/zsh syntax that fish doesn't interpret.
func TestUnsupportedShellErrorIsHonest(t *testing.T) {
	got := unsupportedShellError("fish").Error()

	if !strings.Contains(got, "fish") {
		t.Errorf("the error does not name the detected shell:\n%s", got)
	}
	if !strings.Contains(got, "bash and zsh") {
		t.Errorf("the error does not say which shells are supported:\n%s", got)
	}
	// It should offer the way out that does work in any shell.
	if !strings.Contains(got, "azsel use") {
		t.Errorf("the error does not mention the script-based alternative:\n%s", got)
	}
}

func TestUnsupportedShellErrorWithoutShellEnv(t *testing.T) {
	got := unsupportedShellError("").Error()
	if !strings.Contains(got, "$SHELL") {
		t.Errorf("with no SHELL set, the error should mention it:\n%s", got)
	}
}

// A failure to resolve the home is its own problem, not an unsupported shell.
// It used to be reported as the latter, so that a zsh user was told that zsh
// is not supported.
func TestDetectShellRCSeparatesHomeFailure(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("HOME", "")

	rc, shell, err := detectShellRC()
	if err == nil {
		t.Fatal("detectShellRC returned nil with no HOME")
	}
	if !strings.Contains(err.Error(), "home directory") {
		t.Errorf("error = %q, wanted it to mention the home", err)
	}
	if rc != "" || shell != "" {
		t.Errorf("rcFile=%q shellName=%q, wanted both empty on an error", rc, shell)
	}
}

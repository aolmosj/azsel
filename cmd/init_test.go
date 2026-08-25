package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// shells returns the interpreters azsel installs its function into
// (detectShellRC covers zsh and bash), skipping the ones that aren't present.
// The function is installed into both, so it must work in both: #3 was born
// precisely from a zsh-only construct placed in a file that also ends up in
// .bashrc.
func shells(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	var found []string
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		if _, err := os.Stat(path); err != nil {
			return
		}
		seen[path] = true
		found = append(found, path)
	}

	for _, name := range []string{"bash", "zsh"} {
		if path, err := exec.LookPath(name); err == nil {
			add(path)
		}
	}
	// macOS ships bash 3.2 at /bin/bash. If PATH happens to resolve to a
	// newer one — Homebrew, or a future runner image — exercise both, so the
	// README's claim about 3.2 stays backed by something rather than by luck.
	add("/bin/bash")

	if len(found) == 0 {
		t.Skip("neither bash nor zsh available")
	}
	return found
}

// shellLabel names a subtest after the interpreter's full path: two entries
// can share a base name (/bin/bash and /opt/homebrew/bin/bash).
func shellLabel(path string) string {
	return strings.ReplaceAll(strings.TrimPrefix(path, "/"), "/", "_")
}

// runShellFunc evaluates the shell function installed by `azsel init --print`
// in a real interpreter, with a fake azsel on the PATH that writes whatever
// it's told to $AZSEL_SWITCH_FILE. Returns the script's output.
//
// It's the only test that exercises azsel's central mechanism end-to-end:
// the binary can't touch the parent shell's environment, so everything
// depends on this function sourcing correctly.
func runShellFunc(t *testing.T, shell, home, stubBody, script string, extraEnv ...string) string {
	t.Helper()

	binDir := t.TempDir()
	stub := filepath.Join(binDir, "azsel")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n"+stubBody), 0755); err != nil {
		t.Fatalf("writing the stub: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".azsel"), 0755); err != nil {
		t.Fatalf("setting up the home: %v", err)
	}

	cmd := exec.Command(shell, "-c", shellFunc+"\n"+script)
	cmd.Env = append([]string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"HOME=" + home,
	}, extraEnv...)
	// The exit status is not checked here: some tests exercise precisely
	// the propagation of non-zero codes.
	out, _ := cmd.CombinedOutput()
	return string(out)
}

// eachShell runs fn once per available interpreter, as a subtest.
func eachShell(t *testing.T, fn func(t *testing.T, shell string)) {
	t.Helper()
	for _, shell := range shells(t) {
		t.Run(shellLabel(shell), func(t *testing.T) { fn(t, shell) })
	}
}

// The base case: azsel writes the snippet, the function sources it, and the
// variable ends up set in the shell.
func TestShellFuncSourcesSwitchFile(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		stub := `printf 'export AZURE_CONFIG_DIR=%s\n' /fake/tenants/acme > "$AZSEL_SWITCH_FILE"` + "\n"
		out := runShellFunc(t, shell, home, stub, `azsel use acme; echo "RESULT=$AZURE_CONFIG_DIR"`)

		if !strings.Contains(out, "RESULT=/fake/tenants/acme") {
			t.Errorf("the variable did not reach the shell.\noutput:\n%s", out)
		}
	})
}

// After sourcing, the file should disappear: if it survived, the next
// invocation would apply it again.
func TestShellFuncRemovesSwitchFileAfterSourcing(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		stub := `printf 'export AZURE_CONFIG_DIR=%s\n' /fake/acme > "$AZSEL_SWITCH_FILE"` + "\n"
		out := runShellFunc(t, shell, home, stub,
			`azsel use acme; ls "$HOME/.azsel/" | grep -c switch || echo "NO_LEFTOVERS"`)

		if !strings.Contains(out, "NO_LEFTOVERS") {
			t.Errorf("switch files were left behind.\noutput:\n%s", out)
		}
	})
}

// The heart of #4: each shell uses its own path, derived from its PID.
// Without this, one terminal consumed and deleted another's pending switch.
func TestShellFuncUsesPerShellSwitchFile(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		// The stub gives away the path it was passed instead of writing anything.
		stub := `echo "PATH_USED=$AZSEL_SWITCH_FILE"` + "\n"

		first := runShellFunc(t, shell, home, stub, `azsel list`)
		second := runShellFunc(t, shell, home, stub, `azsel list`)

		extract := func(out string) string {
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, "PATH_USED=") {
					return strings.TrimPrefix(line, "PATH_USED=")
				}
			}
			t.Fatalf("the stub did not report a path.\noutput:\n%s", out)
			return ""
		}
		a, b := extract(first), extract(second)

		if a == b {
			t.Errorf("two shells used the same path (%s); they must differ by PID", a)
		}
		for _, got := range []string{a, b} {
			if !strings.HasPrefix(got, filepath.Join(home, ".azsel", ".switch.")) {
				t.Errorf("path = %q, wanted ~/.azsel/.switch.<pid>", got)
			}
		}
	})
}

// AZSEL_SWITCH_FILE is for the subprocess, not for the session: it must not
// be left dangling in the user's environment.
func TestShellFuncDoesNotLeakSwitchVar(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		out := runShellFunc(t, shell, home, "true\n",
			`azsel list; echo "LEAK=[${AZSEL_SWITCH_FILE:-}]"`)

		if !strings.Contains(out, "LEAK=[]") {
			t.Errorf("AZSEL_SWITCH_FILE was left in the environment.\noutput:\n%s", out)
		}
	})
}

// The function respects AZSEL_HOME, just like the binary.
func TestShellFuncHonoursAzselHome(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		alt := t.TempDir()
		stub := `echo "PATH_USED=$AZSEL_SWITCH_FILE"` + "\n"
		out := runShellFunc(t, shell, home, stub, `export AZSEL_HOME=`+alt+`; azsel list`)

		if !strings.Contains(out, "PATH_USED="+filepath.Join(alt, ".switch.")) {
			t.Errorf("AZSEL_HOME was not respected.\noutput:\n%s", out)
		}
	})
}

// Without a switch file, the function must not break or dirty the output.
func TestShellFuncWithoutSwitchFile(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		out := runShellFunc(t, shell, home, "true\n", `azsel list; echo "OK=$?"`)
		if !strings.Contains(out, "OK=0") {
			t.Errorf("the function failed without a switch file.\noutput:\n%s", out)
		}
	})
}

// #3: debug mode must work in both shells. The function used `whence -p`, a
// zsh-only builtin, and in bash it printed «whence: command not found» inside
// the debug trace itself.
func TestShellFuncDebugIsPortable(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		stub := `printf 'export AZURE_CONFIG_DIR=%s\n' /fake/acme > "$AZSEL_SWITCH_FILE"` + "\n"
		out := runShellFunc(t, shell, home, stub, `azsel use acme`, "AZSEL_DEBUG=1")

		for _, bad := range []string{"command not found", "not found", "whence"} {
			if strings.Contains(out, bad) {
				t.Errorf("the debug trace contains %q.\noutput:\n%s", bad, out)
			}
		}
		// And it must still be useful.
		for _, want := range []string{"args: use acme", "switch file:", "sourcing"} {
			if !strings.Contains(out, want) {
				t.Errorf("the trace does not mention %q.\noutput:\n%s", want, out)
			}
		}
	})
}

// The generated function must not contain zsh-only constructs: it's also
// installed in .bashrc.
func TestShellFuncHasNoZshOnlyBuiltins(t *testing.T) {
	for _, bad := range []string{"whence", "typeset", "setopt", "print -"} {
		if strings.Contains(shellFunc, bad) {
			t.Errorf("the function uses %q, which doesn't exist in bash", bad)
		}
	}
}

// The line init writes into the rc must invoke --print; without it, init
// modifies the rc file instead of printing the function (see #2).
func TestInitLineUsesPrint(t *testing.T) {
	if !strings.Contains(initLine, "--print") {
		t.Errorf("initLine = %q, wanted it to use --print", initLine)
	}
}

// The root command's help showed `eval "$(azsel init)"`, without --print.
// Without that flag, init doesn't print the function: it modifies the rc
// file. Anyone pasting that line into their .zshrc was left without
// integration.
//
// The help concatenates initLine instead of repeating the text, so this test
// watches that this join doesn't come undone.
func TestRootHelpUsesInitLine(t *testing.T) {
	long := rootCmd.Long
	if !strings.Contains(long, initLine) {
		t.Errorf("the root help does not contain %q:\n%s", initLine, long)
	}
	if strings.Contains(long, `eval "$(azsel init)"`) {
		t.Error("the root help still shows 'azsel init' without --print")
	}
}

// The wrapper sourced whatever it found at its path, even if the current
// invocation had written nothing. PIDs get reused, so an orphan left by a
// dead shell ended up being applied in a later shell that reused that PID —
// the same bug #4 wanted to avoid, by another route.
func TestShellFuncIgnoresOrphanFromReusedPID(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		// The stub writes nothing, as `azsel list` would.
		out := runShellFunc(t, shell, home, "true\n",
			`printf 'export AZURE_CONFIG_DIR=/tenant/STALE\n' > "$HOME/.azsel/.switch.$$"
azsel list
echo "RESULT=[${AZURE_CONFIG_DIR:-}]"`)

		if !strings.Contains(out, "RESULT=[]") {
			t.Errorf("an orphaned switch was applied.\noutput:\n%s", out)
		}
	})
}

// The wrapper always returned 0: the final `if` swallowed the binary's exit
// status. `azsel use nonexistent && deploy` ran deploy.
func TestShellFuncPropagatesExitStatus(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		out := runShellFunc(t, shell, home, "exit 7\n",
			`azsel use nonexistent; echo "STATUS=$?"`)

		if !strings.Contains(out, "STATUS=7") {
			t.Errorf("the exit code was not propagated.\noutput:\n%s", out)
		}
	})
}

// And the happy path must still return 0 even after having sourced.
func TestShellFuncReturnsZeroOnSuccess(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		home := t.TempDir()
		stub := `printf 'export AZURE_CONFIG_DIR=%s\n' /fake/acme > "$AZSEL_SWITCH_FILE"` + "\n"
		out := runShellFunc(t, shell, home, stub, `azsel use acme; echo "STATUS=$?"`)

		if !strings.Contains(out, "STATUS=0") {
			t.Errorf("a correct switch did not return 0.\noutput:\n%s", out)
		}
	})
}

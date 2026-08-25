package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

// The message suggested `eval $(azsel use NAME)`, which does nothing: use
// writes the snippet to a file and reports on stderr, so the command
// substitution captures the empty string and `eval ""` is a no-op.
func TestActivationHintNeverSuggestsEval(t *testing.T) {
	for _, active := range []bool{true, false} {
		got := activationHint("acme", active)
		if strings.Contains(got, "eval") {
			t.Errorf("integration=%v: the message still suggests eval:\n%s", active, got)
		}
		if !strings.Contains(got, "azsel use acme") {
			t.Errorf("integration=%v: the real command is missing:\n%s", active, got)
		}
	}
}

// With integration active the message should be a single instruction, without
// noise: the user already has everything set up.
func TestActivationHintIsTerseWhenIntegrationActive(t *testing.T) {
	got := activationHint("acme", true)
	if want := "To activate: azsel use acme\n"; got != want {
		t.Errorf("message = %q, wanted %q", got, want)
	}
}

// Without integration, `azsel use` can't reach the user's shell. It has to be
// said, not left for them to discover on their own.
func TestActivationHintWarnsWithoutIntegration(t *testing.T) {
	got := activationHint("acme", false)
	if !strings.Contains(got, "azsel init") {
		t.Errorf("the warning does not mention 'azsel init':\n%s", got)
	}
	if !strings.Contains(got, "AZURE_CONFIG_DIR") {
		t.Errorf("the warning does not explain the consequence:\n%s", got)
	}
}

// The name is interpolated, not lost.
func TestActivationHintUsesTenantName(t *testing.T) {
	if got := activationHint("globex-prod", true); !strings.Contains(got, "globex-prod") {
		t.Errorf("message = %q, wanted the tenant name", got)
	}
}

// `command azsel add` skips the wrapper on purpose, just like any script.
// Looking only at AZSEL_SWITCH_FILE told a correctly configured user to run
// `azsel init`, which would answer "already configured" and leave them
// without a way out.
func TestShellIntegrationInstalled(t *testing.T) {
	t.Run("wrapper variable present", func(t *testing.T) {
		t.Setenv(config.EnvSwitchFile, "/tmp/.switch.1")
		if !shellIntegrationInstalled() {
			t.Error("integration was not detected with the variable set")
		}
	})

	t.Run("wrapper skipped but rc configured", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("SHELL", "/bin/zsh")
		t.Setenv(config.EnvSwitchFile, "")
		if err := os.WriteFile(filepath.Join(home, ".zshrc"),
			[]byte("\n"+initLine+"\n"), 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if !shellIntegrationInstalled() {
			t.Error("integration was not detected by reading the rc")
		}
	})

	t.Run("without integration", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("SHELL", "/bin/zsh")
		t.Setenv(config.EnvSwitchFile, "")
		if shellIntegrationInstalled() {
			t.Error("integration was detected where there is none")
		}
	})

	t.Run("unsupported shell", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("SHELL", "/usr/bin/fish")
		t.Setenv(config.EnvSwitchFile, "")
		if shellIntegrationInstalled() {
			t.Error("integration was detected in an unsupported shell")
		}
	})
}

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
		wantRC     string // relativo al home; "" = no soportado
		wantShell  string
	}{
		{"zsh", "/bin/zsh", false, ".zshrc", "zsh"},
		{"zsh en otra ruta", "/opt/homebrew/bin/zsh", false, ".zshrc", "zsh"},
		{"bash con bashrc", "/bin/bash", true, ".bashrc", "bash"},
		{"bash sin bashrc", "/bin/bash", false, ".bash_profile", "bash"},
		{"fish", "/opt/homebrew/bin/fish", false, "", "fish"},
		{"sh", "/bin/sh", false, "", "sh"},
		{"SHELL vacío", "", false, "", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("SHELL", c.shell)
			if c.makeBashrc {
				if err := os.WriteFile(filepath.Join(home, ".bashrc"), nil, 0644); err != nil {
					t.Fatalf("preparando: %v", err)
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
				t.Errorf("rcFile = %q, quería %q", rc, want)
			}
			if shell != c.wantShell {
				t.Errorf("shellName = %q, quería %q", shell, c.wantShell)
			}
		})
	}
}

// El mensaje anterior decía "add this manually to your shell rc", lo cual es
// falso para fish: la función usa sintaxis bash/zsh que fish no interpreta.
func TestUnsupportedShellErrorIsHonest(t *testing.T) {
	got := unsupportedShellError("fish").Error()

	if !strings.Contains(got, "fish") {
		t.Errorf("el error no nombra el shell detectado:\n%s", got)
	}
	if !strings.Contains(got, "bash and zsh") {
		t.Errorf("el error no dice qué shells sí están soportados:\n%s", got)
	}
	// Debe ofrecer la salida que sí funciona en cualquier shell.
	if !strings.Contains(got, "azsel use") {
		t.Errorf("el error no menciona la alternativa vía script:\n%s", got)
	}
}

func TestUnsupportedShellErrorWithoutShellEnv(t *testing.T) {
	got := unsupportedShellError("").Error()
	if !strings.Contains(got, "$SHELL") {
		t.Errorf("sin SHELL definido, el error debería mencionarlo:\n%s", got)
	}
}

// Un fallo al resolver el home es su propio problema, no un shell no
// soportado. Antes se reportaba como lo segundo, de modo que a un usuario de
// zsh se le decía que zsh no está soportado.
func TestDetectShellRCSeparatesHomeFailure(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("HOME", "")

	rc, shell, err := detectShellRC()
	if err == nil {
		t.Fatal("detectShellRC devolvió nil sin HOME")
	}
	if !strings.Contains(err.Error(), "home directory") {
		t.Errorf("error = %q, quería que mencionara el home", err)
	}
	if rc != "" || shell != "" {
		t.Errorf("rcFile=%q shellName=%q, quería ambos vacíos ante un error", rc, shell)
	}
}

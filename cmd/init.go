package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const initLine = `eval "$(azsel init --print)"`

const shellFunc = `azsel() {
  local result
  result=$(command azsel "$@")
  if [[ $? -eq 0 && -n "$result" ]]; then
    eval "$result"
  fi
}`

func detectShellRC() string {
	shell := os.Getenv("SHELL")
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch filepath.Base(shell) {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "bash":
		// Prefer .bashrc, fall back to .bash_profile on macOS
		bashrc := filepath.Join(home, ".bashrc")
		if _, err := os.Stat(bashrc); err == nil {
			return bashrc
		}
		return filepath.Join(home, ".bash_profile")
	default:
		return ""
	}
}

func newInitCmd() *cobra.Command {
	var printOnly bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up shell integration",
		Long:  `Adds eval "$(azsel init --print)" to your shell profile. Use --print to output the shell function without modifying any files.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if printOnly {
				fmt.Println(shellFunc)
				return nil
			}

			rcFile := detectShellRC()
			if rcFile == "" {
				return fmt.Errorf("could not detect shell profile — add this manually to your shell rc:\n\n  %s", initLine)
			}

			data, err := os.ReadFile(rcFile)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("reading %s: %w", rcFile, err)
			}

			if strings.Contains(string(data), "azsel init") {
				fmt.Fprintf(os.Stderr, "Already configured in %s — no changes made.\n", rcFile)
				return nil
			}

			f, err := os.OpenFile(rcFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("opening %s: %w", rcFile, err)
			}
			defer f.Close()

			if _, err := fmt.Fprintf(f, "\n# azsel — Azure tenant selector\n%s\n", initLine); err != nil {
				return fmt.Errorf("writing to %s: %w", rcFile, err)
			}

			fmt.Fprintf(os.Stderr, "Added azsel init to %s\n", rcFile)
			fmt.Fprintf(os.Stderr, "Run 'source %s' or restart your shell to activate.\n", rcFile)
			return nil
		},
	}

	cmd.Flags().BoolVar(&printOnly, "print", false, "Print the shell function without modifying any files")
	return cmd
}

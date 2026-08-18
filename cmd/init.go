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
  # One switch file per shell, keyed by PID. A single shared file let one
  # terminal consume and delete another's pending switch.
  local _azsel_f="${AZSEL_HOME:-$HOME/.azsel}/.switch.$$"
  if [[ -n "$AZSEL_DEBUG" ]]; then
    echo "[azsel-debug] args: $*" >&2
    echo "[azsel-debug] switch file: $_azsel_f" >&2
  fi
  AZSEL_SWITCH_FILE="$_azsel_f" command azsel "$@"
  if [[ -f "$_azsel_f" ]]; then
    if [[ -n "$AZSEL_DEBUG" ]]; then
      echo "[azsel-debug] sourcing $_azsel_f" >&2
      command cat "$_azsel_f" >&2
    fi
    source "$_azsel_f"
    command rm -f "$_azsel_f"
    if [[ -n "$AZSEL_DEBUG" ]]; then
      echo "[azsel-debug] AZURE_CONFIG_DIR=$AZURE_CONFIG_DIR" >&2
    fi
  elif [[ -n "$AZSEL_DEBUG" ]]; then
    echo "[azsel-debug] no .switch file" >&2
  fi
}`

// detectShellRC reports the rc file to install the wrapper into, plus the
// shell it detected. An empty rcFile means azsel cannot integrate with that
// shell; shellName is still returned so the caller can say which one it was.
func detectShellRC() (rcFile, shellName string) {
	shell := os.Getenv("SHELL")
	if shell != "" {
		shellName = filepath.Base(shell)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", shellName
	}
	switch shellName {
	case "zsh":
		return filepath.Join(home, ".zshrc"), shellName
	case "bash":
		// Prefer .bashrc, fall back to .bash_profile on macOS
		bashrc := filepath.Join(home, ".bashrc")
		if _, err := os.Stat(bashrc); err == nil {
			return bashrc, shellName
		}
		return filepath.Join(home, ".bash_profile"), shellName
	default:
		return "", shellName
	}
}

// unsupportedShellError explains what azsel can and cannot do here.
//
// The previous message said "add this manually to your shell rc", which is
// wrong for fish: the wrapper is written in bash/zsh syntax that fish cannot
// parse, so following the advice fails. Suggest the line only where it has a
// chance of working, and point at the escape hatch that always does.
func unsupportedShellError(shellName string) error {
	which := "your shell could not be detected from $SHELL"
	if shellName != "" {
		which = fmt.Sprintf("%q is not one of them", shellName)
	}
	return fmt.Errorf(`azsel's shell integration is written for bash and zsh — %s.

The wrapper uses bash/zsh syntax, so adding it to a shell like fish will not
work. If your shell is bash-compatible you can add this line yourself:

  %s

Either way azsel still works: run 'azsel use <name>' and source the file it
writes. See "Use in scripts" in the README.`, which, initLine)
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

			rcFile, shellName := detectShellRC()
			if rcFile == "" {
				return unsupportedShellError(shellName)
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

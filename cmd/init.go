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
  # Clear the path before running: PIDs get reused, so a file orphaned by a
  # shell that died mid-switch would otherwise be sourced later by an
  # unrelated command in a shell that happens to reuse its PID.
  command rm -f "$_azsel_f"
  if [[ -n "$AZSEL_DEBUG" ]]; then
    echo "[azsel-debug] args: $*" >&2
    echo "[azsel-debug] switch file: $_azsel_f" >&2
  fi
  AZSEL_SWITCH_FILE="$_azsel_f" command azsel "$@"
  local _azsel_rc=$?
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
    echo "[azsel-debug] no switch file" >&2
  fi
  return $_azsel_rc
}`

// detectShellRC reports the rc file to install the wrapper into, plus the
// shell it detected. An empty rcFile means azsel cannot integrate with that
// shell; shellName is still returned so the caller can say which one it was.
func detectShellRC() (rcFile, shellName string, err error) {
	shell := os.Getenv("SHELL")
	if shell != "" {
		shellName = filepath.Base(shell)
	}
	// A failing home lookup is its own problem, not an unsupported shell.
	// Reporting it as the latter would tell a zsh user that zsh is not
	// supported, which the next few lines contradict.
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("getting home directory: %w", err)
	}
	switch shellName {
	case "zsh":
		return filepath.Join(home, ".zshrc"), shellName, nil
	case "bash":
		// Prefer .bashrc, fall back to .bash_profile on macOS
		bashrc := filepath.Join(home, ".bashrc")
		if _, err := os.Stat(bashrc); err == nil {
			return bashrc, shellName, nil
		}
		return filepath.Join(home, ".bash_profile"), shellName, nil
	default:
		return "", shellName, nil
	}
}

// initMarker is what identifies an already-installed integration line inside
// a shell rc file.
const initMarker = "azsel init"

// rcContainsInit reports whether the rc file already wires up azsel.
func rcContainsInit(rcFile string) bool {
	data, err := os.ReadFile(rcFile)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), initMarker)
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

			rcFile, shellName, err := detectShellRC()
			if err != nil {
				return err
			}
			if rcFile == "" {
				return unsupportedShellError(shellName)
			}

			data, err := os.ReadFile(rcFile)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("reading %s: %w", rcFile, err)
			}

			if strings.Contains(string(data), initMarker) {
				fmt.Fprintf(os.Stderr, "Already configured in %s — no changes made.\n", rcFile)
				return nil
			}

			f, err := os.OpenFile(rcFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("opening %s: %w", rcFile, err)
			}
			if _, err := fmt.Fprintf(f, "\n# azsel — Azure tenant selector\n%s\n", initLine); err != nil {
				_ = f.Close()
				return fmt.Errorf("writing to %s: %w", rcFile, err)
			}
			// Checked rather than deferred: a failing Close on a file just
			// written can mean the append never reached disk, and the user
			// would be told their shell was configured when it was not.
			if err := f.Close(); err != nil {
				return fmt.Errorf("closing %s: %w", rcFile, err)
			}

			fmt.Fprintf(os.Stderr, "Added azsel init to %s\n", rcFile)
			fmt.Fprintf(os.Stderr, "Run 'source %s' or restart your shell to activate.\n", rcFile)
			return nil
		},
	}

	cmd.Flags().BoolVar(&printOnly, "print", false, "Print the shell function without modifying any files")
	return cmd
}

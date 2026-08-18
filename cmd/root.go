package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "azsel",
	Short: "Azure tenant selector — manage multiple Azure CLI profiles",
	Long: `azsel manages multiple Azure tenant configurations by isolating
each tenant's az CLI config via AZURE_CONFIG_DIR.

Run without subcommands to launch the interactive TUI.

Shell integration — run 'azsel init' to set it up automatically, or add
this line to .bashrc / .zshrc yourself:

  ` + initLine,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runTUI,
}

func init() {
	rootCmd.AddCommand(newAddCmd())
	rootCmd.AddCommand(newListCmd())
	rootCmd.AddCommand(newUseCmd())
	rootCmd.AddCommand(newRemoveCmd())
	rootCmd.AddCommand(newInitCmd())
}

func Execute() error {
	rootCmd.Version = Version
	// The wrapper used to resolve this with `whence -p`, which only exists in
	// zsh, and the wrapper is installed into bash too. os.Executable is both
	// portable and more truthful: it reports the binary actually running, not
	// what PATH says should run.
	if os.Getenv("AZSEL_DEBUG") != "" {
		if exe, err := os.Executable(); err == nil {
			fmt.Fprintf(os.Stderr, "[azsel-debug-go] binary: %s\n", exe)
		}
	}
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}
	return nil
}

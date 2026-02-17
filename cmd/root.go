package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "azsel",
	Short: "Azure tenant selector — manage multiple Azure CLI profiles",
	Long: `azsel manages multiple Azure tenant configurations by isolating
each tenant's az CLI config via AZURE_CONFIG_DIR.

Run without subcommands to launch the interactive TUI.

Shell integration (add to .bashrc / .zshrc):

  azsel() {
    local result
    result=$(command azsel "$@")
    if [[ $? -eq 0 && -n "$result" ]]; then
      eval "$result"
    fi
  }`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runTUI,
}

func init() {
	rootCmd.AddCommand(newAddCmd())
	rootCmd.AddCommand(newListCmd())
	rootCmd.AddCommand(newUseCmd())
	rootCmd.AddCommand(newRemoveCmd())
}

func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}
	return nil
}

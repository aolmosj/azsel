package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/aolmosj/azsel/internal/config"
	"github.com/aolmosj/azsel/internal/tui"
	"github.com/spf13/cobra"
)

func runTUI(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if len(cfg.Tenants) == 0 {
		fmt.Fprintln(os.Stderr, "No tenants configured. Run 'azsel add' to add one.")
		return nil
	}

	currentDir := os.Getenv("AZURE_CONFIG_DIR")
	model := tui.NewModel(cfg.Tenants, currentDir)

	p := tea.NewProgram(model, tea.WithOutput(os.Stderr))
	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	if m, ok := finalModel.(tui.Model); ok {
		if selected := m.Selected(); selected != nil {
			extDir, err := config.ExtensionsDir()
			if err != nil {
				return err
			}
			exports := fmt.Sprintf("export AZURE_CONFIG_DIR=%s\nexport AZURE_EXTENSION_DIR=%s\n", selected.ConfigDir, extDir)
			if err := config.WriteEnv(exports); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Activated tenant %q\n", selected.Name)
		}
	}
	return nil
}

package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/aolmosj/azsel/internal/config"
	"github.com/aolmosj/azsel/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
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
	// The default is a filesystem fact (the ~/.azure symlink); resolving it
	// can fail on a genuinely broken filesystem, in which case the TUI simply
	// shows no default marker rather than refusing to open.
	defaultName := ""
	if def, err := config.ResolveDefault(cfg); err == nil && def.State == config.DefaultSet {
		defaultName = def.Tenant
	}
	setDefault := func(name string) error {
		stamp := time.Now().Format("20060102-150405")
		_, err := config.SetDefault(cfg, name, stamp)
		return err
	}
	model := tui.NewModel(cfg.Tenants, currentDir, defaultName, setDefault)

	p := tea.NewProgram(model, tea.WithOutput(os.Stderr))
	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	if m, ok := finalModel.(tui.Model); ok {
		if selected := m.Selected(); selected != nil {
			if err := config.EnsureSharedExtensionsLink(selected.ConfigDir); err != nil {
				return err
			}
			exports := fmt.Sprintf("export AZURE_CONFIG_DIR=%s\n", selected.ConfigDir)
			if err := config.WriteEnv(exports); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Activated tenant %q\n", selected.Name)
		}
	}
	return nil
}

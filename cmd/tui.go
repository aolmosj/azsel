package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/aolmosj/azsel/internal/azure"
	"github.com/aolmosj/azsel/internal/config"
	"github.com/aolmosj/azsel/internal/pim"
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
	setDefault := func(name string) (string, error) {
		stamp := time.Now().Format("20060102-150405")
		res, err := config.SetDefault(cfg, name, stamp)
		if err != nil {
			return "", err
		}
		if res.BackupPath != "" {
			return "Moved your existing ~/.azure to " + res.BackupPath, nil
		}
		return "", nil
	}
	// Loading eligible PIM roles needs the Azure CLI; checking inside the
	// closure means pressing "p" without az shows a friendly error in the PIM
	// view rather than a silent no-op, and keeps azure/pim out of the model.
	listPIM := func(t config.Tenant) ([]pim.Eligible, error) {
		if err := azure.Available(); err != nil {
			return nil, err
		}
		return pim.ListEligible(t.ConfigDir, t.TenantID)
	}
	model := tui.NewModel(cfg.Tenants, currentDir, defaultName, setDefault, listPIM)
	// The on-open session check needs the Azure CLI; without it the TUI simply
	// shows no login markers rather than failing to open.
	if azure.Available() == nil {
		model.SetSessionCheck(func(t config.Tenant) bool {
			valid, _ := azure.SessionState(t.ConfigDir)
			return valid
		})
	}

	p := tea.NewProgram(model, tea.WithOutput(os.Stderr))
	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	m, ok := finalModel.(tui.Model)
	if !ok {
		return nil
	}
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
	// The interactive login runs after the TUI closes, so az owns the terminal.
	if lw := m.LoginWanted(); lw != nil {
		if err := azure.Available(); err != nil {
			return err
		}
		if err := config.EnsureSharedExtensionsLink(lw.ConfigDir); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Logging in to tenant %q...\n", lw.Name)
		return azure.Login(lw.TenantID, lw.ConfigDir, false)
	}
	return nil
}

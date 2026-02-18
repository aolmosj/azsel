package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Azure blue palette
	azureBlue  = lipgloss.Color("#0078D4")
	azureLight = lipgloss.Color("#50A0E6")
	green      = lipgloss.Color("#04B575")
	subtle     = lipgloss.Color("#626262")
	white      = lipgloss.Color("#FAFAFA")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(white).
			Background(azureBlue).
			Padding(0, 1)

	activeStyle = lipgloss.NewStyle().
			Foreground(green).
			Bold(true)

	// Selected item in list
	selectedTitleStyle = lipgloss.NewStyle().
				Foreground(azureBlue).
				Bold(true).
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(azureBlue).
				Padding(0, 0, 0, 1)

	selectedDescStyle = lipgloss.NewStyle().
				Foreground(azureLight).
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(azureBlue).
				Padding(0, 0, 0, 1)

	// Normal item in list
	normalTitleStyle = lipgloss.NewStyle().
				Padding(0, 0, 0, 2)

	normalDescStyle = lipgloss.NewStyle().
			Foreground(subtle).
			Padding(0, 0, 0, 2)

	// Status bar
	statusStyle = lipgloss.NewStyle().
			Foreground(white).
			Background(azureBlue).
			Padding(0, 1)

	appStyle = lipgloss.NewStyle().Margin(1, 2)
)

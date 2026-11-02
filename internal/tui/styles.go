package tui

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vasyakrg/helm-tui/internal/health"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))

	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	normalStyle   = lipgloss.NewStyle()

	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("35"))
	failStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	checkingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))

	addedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("35"))
	removedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	modifiedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	contextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	hintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// statusMark renders the availability mark for a repo state.
func statusMark(state health.State) string {
	switch state {
	case health.Checking:
		return checkingStyle.Render("…")
	case health.OK:
		return okStyle.Render("●")
	default:
		return failStyle.Render("✕")
	}
}

// lipglossWidth returns the display width of s.
func lipglossWidth(s string) int { return lipgloss.Width(s) }

// truncate cuts s to at most width display cells, appending "…" when cut.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return ansi.Truncate(s, width, "…")
}

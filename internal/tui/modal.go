package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// modal is implemented by dialog screens (forms, confirmations). They are
// drawn as a centered box over the nearest non-modal screen below them.
type modal interface {
	modalTitle() string
}

const (
	dialogMaxWidth = 64
	dialogMinWidth = 30
	// dialogInputWidth is the width of text inputs inside dialogs.
	dialogInputWidth = 36
)

var (
	dialogStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Padding(1, 2)

	buttonStyle         = lipgloss.NewStyle().Padding(0, 2)
	buttonSelectedStyle = lipgloss.NewStyle().Padding(0, 2).Bold(true).
				Foreground(lipgloss.Color("0")).Background(lipgloss.Color("39"))
)

// dialogWidth returns the outer width of a dialog for a terminal width.
func dialogWidth(termWidth int) int {
	w := dialogMaxWidth
	if termWidth > 0 && termWidth-4 < w {
		w = termWidth - 4
	}
	if w < dialogMinWidth {
		w = dialogMinWidth
	}
	return w
}

// renderDialog draws the dialog box for m.
func renderDialog(m screen, termWidth int) string {
	title := m.(modal).modalTitle()
	body := titleStyle.Render(title) + "\n\n" + strings.TrimRight(m.View(), "\n")
	if h, ok := m.(hinter); ok {
		body += "\n\n" + hintStyle.Render(h.hints())
	}
	return dialogStyle.Width(dialogWidth(termWidth)).Render(body)
}

// overlayCenter draws box centered over base. base is padded to height rows
// so the box is centered in the whole window.
func overlayCenter(base, box string, width, height int) string {
	if height > 0 {
		if n := strings.Count(base, "\n") + 1; n < height {
			base += strings.Repeat("\n", height-n)
		}
	}
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	w, h := width, height
	if w <= 0 {
		w = lipgloss.Width(base)
	}
	if h <= 0 {
		h = lipgloss.Height(base)
	}
	x := maxInt((w-bw)/2, 0)
	y := maxInt((h-bh)/2, 0)
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(box).X(x).Y(y).Z(1),
	).Render()
}

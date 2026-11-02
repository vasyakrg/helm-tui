package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// diffScreen shows the diff between two chart versions.
// Full implementation lives in Task 13; this stub keeps the screen stack
// working.
type diffScreen struct {
	repo, chart, v1, v2 string
}

func newDiffScreen(repo, chart, v1, v2 string) *diffScreen {
	return &diffScreen{repo: repo, chart: chart, v1: v1, v2: v2}
}

func (s *diffScreen) Init(*app) tea.Cmd { return nil }

func (s *diffScreen) Update(msg tea.Msg, a *app) (screen, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "q" {
		return s, func() tea.Msg { return quitMsg{} }
	}
	return s, nil
}

func (s *diffScreen) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(s.chart+" "+s.v1+" → "+s.v2) + "\n")
	b.WriteString("loading diff…")
	return b.String()
}

const hintDiff = "j/k file  n/N hunk  esc back"

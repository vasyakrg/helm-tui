package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/index"
)

// versionsScreen lists versions of one chart; two can be marked for diff.
type versionsScreen struct {
	repo     string
	chart    index.Chart
	cursor   int
	marked   []int // indexes into chart.Versions, oldest selection first
	status   string
}

func newVersionsScreen(repo string, chart index.Chart) *versionsScreen {
	return &versionsScreen{repo: repo, chart: chart}
}

func (s *versionsScreen) Init(*app) tea.Cmd { return nil }

func (s *versionsScreen) Update(msg tea.Msg, a *app) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if s.cursor < len(s.chart.Versions)-1 {
				s.cursor++
			}
		case "k", "up":
			if s.cursor > 0 {
				s.cursor--
			}
		case " ":
			s.toggle(s.cursor)
		case "q":
			return s, func() tea.Msg { return quitMsg{} }
		}
	}
	return s, nil
}

// toggle marks/unmarks the version at i; at most two can be marked.
func (s *versionsScreen) toggle(i int) {
	for n, m := range s.marked {
		if m == i {
			s.marked = append(s.marked[:n], s.marked[n+1:]...)
			return
		}
	}
	if len(s.marked) >= 2 {
		s.status = "two versions already selected"
		return
	}
	s.marked = append(s.marked, i)
}

func (s *versionsScreen) View() string {
	var b strings.Builder
	for i, v := range s.chart.Versions {
		prefix := "  "
		st := normalStyle
		if i == s.cursor {
			prefix = "> "
			st = selectedStyle
		}
		mark := " "
		for _, m := range s.marked {
			if m == i {
				mark = "*"
			}
		}
		app := v.AppVersion
		b.WriteString(prefix + "[" + mark + "] " + st.Render(pad(v.Version, 16)) + "  " +
			dimStyle.Render(pad(app, 14)+"  "+v.Created.Format("2006-01-02")) + "\n")
	}
	if s.status != "" {
		b.WriteString(errStyle.Render(s.status) + "\n")
	}
	return b.String()
}

const hintVersions = "j/k move  space mark (max 2)  D diff  q quit"

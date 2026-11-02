package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/index"
)

// versionsScreen lists versions of one chart; two can be marked for diff.
type versionsScreen struct {
	repo   string
	chart  index.Chart
	cursor int
	marked []int // indexes into chart.Versions, oldest selection first
	status string
	top    int
	winSize
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
		case " ", "space":
			s.toggle(s.cursor)
		case "D":
			if len(s.marked) < 2 {
				s.status = "mark two versions with space first"
				return s, nil
			}
			v1 := s.chart.Versions[s.marked[0]].Version
			v2 := s.chart.Versions[s.marked[1]].Version
			repo, chart := s.repo, s.chart.Name
			return s, func() tea.Msg {
				return pushScreenMsg{Screen: newDiffScreen(repo, chart, v1, v2)}
			}
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

func (s *versionsScreen) hints() string { return hintVersions }

func (s *versionsScreen) View() string {
	var b strings.Builder
	reserved := 0
	if s.status != "" {
		reserved = 1
	}
	rows := s.listRows(reserved)
	s.top = windowTop(s.top, s.cursor, len(s.chart.Versions), rows)
	end := windowEnd(s.top, len(s.chart.Versions), rows)
	for i := s.top; i < end; i++ {
		v := s.chart.Versions[i]
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

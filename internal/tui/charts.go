package tui

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/index"
)

var errNoCacheDir = errors.New("helm cache dir unknown")

// chartsLoadedMsg delivers the chart list of a repository.
type chartsLoadedMsg struct {
	Repo   string
	Charts []index.Chart
	Err    error
}

// chartsScreen lists charts of one repository.
type chartsScreen struct {
	row    RepoRow
	charts []index.Chart // nil until loaded; empty slice = index exists but no charts
	filter string
	cursor int
	err    error
}

func newChartsScreen(row RepoRow) *chartsScreen {
	return &chartsScreen{row: row}
}

func (s *chartsScreen) Init(a *app) tea.Cmd {
	repo := s.row.Repo.Name
	return func() tea.Msg { return loadChartsCmd(repo)(a) }
}

func loadChartsCmd(repo string) func(*app) tea.Msg {
	return func(a *app) tea.Msg {
		if a.deps.Env.RepositoryCache == "" {
			return chartsLoadedMsg{Repo: repo, Err: errNoCacheDir}
		}
		idx, err := index.Load(a.deps.Env.RepositoryCache, repo)
		if err != nil {
			return chartsLoadedMsg{Repo: repo, Err: err}
		}
		return chartsLoadedMsg{Repo: repo, Charts: idx.Charts}
	}
}

func (s *chartsScreen) Update(msg tea.Msg, a *app) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case chartsLoadedMsg:
		if msg.Repo != s.row.Repo.Name {
			return s, nil
		}
		s.err = msg.Err
		s.charts = msg.Charts
		s.clampCursor()
		return s, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if s.cursor < len(s.visible())-1 {
				s.cursor++
			}
		case "k", "up":
			if s.cursor > 0 {
				s.cursor--
			}
		case "enter":
			visible := s.visible()
			if s.cursor < len(visible) {
				c := visible[s.cursor]
				return s, func() tea.Msg {
					return pushScreenMsg{Screen: newVersionsScreen(s.row.Repo.Name, c)}
				}
			}
		case "q":
			return s, func() tea.Msg { return quitMsg{} }
		}
	}
	return s, nil
}

func (s *chartsScreen) clampCursor() {
	if s.cursor >= len(s.visible()) {
		s.cursor = maxInt(len(s.visible())-1, 0)
	}
}

func (s *chartsScreen) visible() []index.Chart {
	if s.filter == "" {
		return s.charts
	}
	var out []index.Chart
	q := strings.ToLower(s.filter)
	for _, c := range s.charts {
		if strings.Contains(strings.ToLower(c.Name), q) {
			out = append(out, c)
		}
	}
	return out
}

func (s *chartsScreen) View() string {
	if s.err != nil {
		return errStyle.Render("index not found, press u to update the repository")
	}
	if s.charts == nil {
		return "loading charts…"
	}
	visible := s.visible()
	if len(visible) == 0 {
		return "no charts"
	}
	var b strings.Builder
	for i, c := range visible {
		prefix := "  "
		st := normalStyle
		if i == s.cursor {
			prefix = "> "
			st = selectedStyle
		}
		desc := ""
		if len(c.Versions) > 0 {
			desc = truncate(c.Versions[0].Description, 40)
		}
		b.WriteString(prefix + st.Render(pad(truncate(c.Name, 30), 30)) + "  " + dimStyle.Render(desc) + "\n")
	}
	return b.String()
}

const hintCharts = "/ filter  Enter versions  q quit"

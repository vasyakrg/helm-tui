package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/index"
	"github.com/vasyakrg/helm-tui/internal/proxy"
)

var errNoCacheDir = errors.New("helm cache dir unknown")

// chartsLoadedMsg delivers the chart list of a repository.
type chartsLoadedMsg struct {
	Repo   string
	Charts []index.Chart
	Err    error
}

// escConsumer lets a screen handle Esc itself (e.g. to leave filter mode
// before the app pops the screen).
type escConsumer interface {
	consumeEsc(*app) bool
}

// chartsUpdateFailedMsg reports a failed `helm repo update` for the repo.
type chartsUpdateFailedMsg struct {
	Repo string
	Err  error
}

// chartsScreen lists charts of one repository.
type chartsScreen struct {
	row       RepoRow
	charts    []index.Chart
	loaded    bool // the index was read (charts may legitimately be empty)
	filter    string
	filtering bool
	input     textinput.Model
	cursor    int
	err       error
	updating  bool
	updateErr string
}

func newChartsScreen(row RepoRow) *chartsScreen {
	ti := textinput.New()
	ti.Prompt = "filter: "
	return &chartsScreen{row: row, input: ti}
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
		s.loaded = msg.Err == nil
		s.updating = false
		s.updateErr = ""
		s.clampCursor()
		return s, nil

	case chartsUpdateFailedMsg:
		if msg.Repo != s.row.Repo.Name {
			return s, nil
		}
		s.updating = false
		s.updateErr = "update failed: " + proxy.MaskText(msg.Err.Error())
		return s, nil

	case tea.KeyPressMsg:
		if s.filtering {
			return s.updateFilter(msg)
		}
		switch msg.String() {
		case "j", "down":
			if s.cursor < len(s.visible())-1 {
				s.cursor++
			}
		case "k", "up":
			if s.cursor > 0 {
				s.cursor--
			}
		case "/":
			s.filtering = true
			s.input.SetValue(s.filter)
			s.input.CursorEnd()
			s.input.Focus()
		case "enter":
			visible := s.visible()
			if s.cursor < len(visible) {
				c := visible[s.cursor]
				return s, func() tea.Msg {
					return pushScreenMsg{Screen: newVersionsScreen(s.row.Repo.Name, c)}
				}
			}
		case "u":
			if !s.updating {
				return s, s.updateCmd(a)
			}
		case "q":
			return s, func() tea.Msg { return quitMsg{} }
		}
	}
	return s, nil
}

// updateFilter handles keys while the filter input has focus: everything
// except navigation and Enter goes to the text input.
func (s *chartsScreen) updateFilter(msg tea.KeyPressMsg) (screen, tea.Cmd) {
	switch msg.String() {
	case "enter":
		s.filtering = false
		s.input.Blur()
		return s, nil
	case "down":
		if s.cursor < len(s.visible())-1 {
			s.cursor++
		}
		return s, nil
	case "up":
		if s.cursor > 0 {
			s.cursor--
		}
		return s, nil
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	s.filter = s.input.Value()
	s.clampCursor()
	return s, cmd
}

// updateCmd runs `helm repo update <repo>` (with the repo proxy) and reloads
// the cached index.
func (s *chartsScreen) updateCmd(a *app) tea.Cmd {
	s.updating = true
	s.updateErr = ""
	repo := s.row.Repo.Name
	proxyURL := a.proxyFor(repo)
	return func() tea.Msg {
		if err := a.deps.Helm.Update(context.Background(), repo, proxyURL); err != nil {
			return chartsUpdateFailedMsg{Repo: repo, Err: err}
		}
		return loadChartsCmd(repo)(a)
	}
}

// consumeEsc exits filter mode first; otherwise lets the app pop the screen.
func (s *chartsScreen) consumeEsc(*app) bool {
	if s.filtering {
		s.filtering = false
		s.input.Blur()
		return true
	}
	return false
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
	var b strings.Builder
	if s.updating {
		b.WriteString(dimStyle.Render("updating repository…") + "\n")
	}
	if s.updateErr != "" {
		b.WriteString(errStyle.Render(s.updateErr) + "\n")
	}
	switch {
	case s.err != nil:
		b.WriteString(errStyle.Render(chartsErrText(s.err)))
		return b.String()
	case !s.loaded:
		b.WriteString("loading charts…")
		return b.String()
	case len(s.charts) == 0:
		b.WriteString("no charts in this repository index")
		return b.String()
	}
	switch {
	case s.filtering:
		b.WriteString(s.input.View() + "\n")
	case s.filter != "":
		b.WriteString(dimStyle.Render("filter: "+s.filter) + "\n")
	}
	visible := s.visible()
	if len(visible) == 0 {
		b.WriteString("no charts match the filter")
		return b.String()
	}
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

func chartsErrText(err error) string {
	if errors.Is(err, index.ErrNoIndex) {
		return "index not found, press u to update the repository"
	}
	return fmt.Sprintf("error: %s", proxy.MaskText(err.Error()))
}

const hintCharts = "/ filter  u update repo  Enter versions  q quit"

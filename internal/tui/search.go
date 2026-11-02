package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/index"
	"github.com/vasyakrg/helm-tui/internal/search"
)

// searchScreen is the global search: an input plus hits across all repos.
type searchScreen struct {
	input  textinput.Model
	idxs   []*index.Index
	hits   []search.Hit
	cursor int
	err    string
}

func newSearchScreen() *searchScreen {
	ti := textinput.New()
	ti.Placeholder = "search charts…"
	ti.Focus()
	return &searchScreen{input: ti}
}

func (s *searchScreen) Init(a *app) tea.Cmd {
	return s.loadIndexes(a)
}

// loadIndexes reads all cached indexes; repos without index are skipped.
func (s *searchScreen) loadIndexes(a *app) tea.Cmd {
	return func() tea.Msg {
		names := make([]string, 0)
		if rs, ok := a.top().(*reposScreen); ok {
			for _, r := range rs.rows {
				names = append(names, r.Repo.Name)
			}
		}
		var idxs []*index.Index
		for _, n := range names {
			if idx, err := index.Load(a.deps.Env.RepositoryCache, n); err == nil {
				idxs = append(idxs, idx)
			}
		}
		return indexesLoadedMsg{Idxs: idxs}
	}
}

// indexesLoadedMsg carries parsed indexes into the search screen.
type indexesLoadedMsg struct {
	Idxs []*index.Index
}

func (s *searchScreen) Update(msg tea.Msg, a *app) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case indexesLoadedMsg:
		s.idxs = msg.Idxs
		s.applyFilter()
		return s, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "down", "ctrl+n":
			if s.cursor < len(s.hits)-1 {
				s.cursor++
			}
			return s, nil
		case "up", "ctrl+p":
			if s.cursor > 0 {
				s.cursor--
			}
			return s, nil
		case "enter":
			if s.cursor < len(s.hits) {
				hit := s.hits[s.cursor]
				return s, func() tea.Msg {
					return pushScreenMsg{Screen: newVersionsScreen(hit.Repo, index.Chart{
						Name:     hit.Chart,
						Versions: hit.Versions,
					})}
				}
			}
			return s, nil
		}
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		s.applyFilter()
		return s, cmd
	}
	return s, nil
}

func (s *searchScreen) applyFilter() {
	s.hits = search.Search(s.idxs, s.input.Value())
	if s.cursor >= len(s.hits) {
		s.cursor = maxInt(len(s.hits)-1, 0)
	}
}

func (s *searchScreen) View() string {
	var b strings.Builder
	b.WriteString("search\n")
	b.WriteString(s.input.View() + "\n")
	if len(s.hits) == 0 {
		b.WriteString(dimStyle.Render("no matches"))
		return b.String()
	}
	for i, h := range s.hits {
		prefix := "  "
		st := normalStyle
		if i == s.cursor {
			prefix = "> "
			st = selectedStyle
		}
		b.WriteString(prefix + st.Render(pad(truncate(h.Repo+"/"+h.Chart, 40), 40)) + "  " +
			st.Render(pad(h.Latest, 12)) + "  " + dimStyle.Render(truncate(h.Description, 30)) + "\n")
	}
	return b.String()
}

const hintSearch = "type to filter  Enter open  esc back"

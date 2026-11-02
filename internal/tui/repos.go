package tui

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/health"
	"github.com/vasyakrg/helm-tui/internal/helm"
	"github.com/vasyakrg/helm-tui/internal/index"
)

// RepoRow is one row of the Repos screen.
type RepoRow struct {
	Repo     helm.Repo
	Charts   int // number of charts in the cached index; unused when HasIndex==false
	HasIndex bool
	State    health.State
	Latency  time.Duration
	Reason   string
}

// reposScreen lists repositories with availability status.
type reposScreen struct {
	rows    []RepoRow
	cursor  int
	loading bool
}

func newReposScreen() *reposScreen {
	return &reposScreen{loading: true}
}

func (s *reposScreen) Init(a *app) tea.Cmd {
	return loadReposCmd(a)
}

// loadReposCmd lists repos and reads cached indexes.
func loadReposCmd(a *app) tea.Cmd {
	return func() tea.Msg {
		repos, err := a.deps.Helm.List(context.Background())
		if err != nil {
			return opDoneMsg{Err: err}
		}
		rows := make([]RepoRow, 0, len(repos))
		for _, r := range repos {
			row := RepoRow{Repo: r, State: health.Checking}
			if idx, err := index.Load(a.deps.Env.RepositoryCache, r.Name); err == nil {
				row.HasIndex = true
				row.Charts = len(idx.Charts)
			}
			rows = append(rows, row)
		}
		return reposLoadedMsg{Rows: rows}
	}
}

// startHealthCmd launches the parallel availability check.
func startHealthCmd(a *app) tea.Cmd {
	if a.deps.Health == nil {
		return nil
	}
	repos := make([]helm.Repo, len(a.top().(*reposScreen).rows))
	for i, r := range a.top().(*reposScreen).rows {
		repos[i] = r.Repo
	}
	ch := a.deps.Health(context.Background(), repos, a.proxyFor)
	return waitForHealth(ch)
}

// waitForHealth returns a command yielding the next health result.
func waitForHealth(ch <-chan health.Result) tea.Cmd {
	return func() tea.Msg {
		res, ok := <-ch
		if !ok {
			return nil
		}
		return healthMsg{Result: res}
	}
}

func (s *reposScreen) Update(msg tea.Msg, a *app) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case reposLoadedMsg:
		s.rows = msg.Rows
		s.loading = false
		s.clampCursor()
		return s, startHealthCmd(a)

	case healthMsg:
		for i := range s.rows {
			if s.rows[i].Repo.Name == msg.Result.Repo {
				s.rows[i].State = msg.Result.State
				s.rows[i].Latency = msg.Result.Latency
				s.rows[i].Reason = msg.Result.Reason
			}
		}
		return s, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if s.cursor < len(s.rows)-1 {
				s.cursor++
			}
		case "k", "up":
			if s.cursor > 0 {
				s.cursor--
			}
		case "g", "home":
			s.cursor = 0
		case "G", "end":
			s.cursor = maxInt(len(s.rows)-1, 0)
		case "r":
			for i := range s.rows {
				s.rows[i].State = health.Checking
				s.rows[i].Reason = ""
			}
			return s, startHealthCmd(a)
		case "a":
			return s, func() tea.Msg { return pushScreenMsg{Screen: newAddForm()} }
		case "d":
			if len(s.rows) > 0 && s.cursor < len(s.rows) {
				row := s.rows[s.cursor]
				return s, func() tea.Msg { return pushScreenMsg{Screen: newDeleteDialog(row)} }
			}
		case "u":
			if len(s.rows) > 0 && s.cursor < len(s.rows) {
				name := s.rows[s.cursor].Repo.Name
				return s, updateOneCmd(a, name)
			}
		case "U":
			return s, updateAllCmd(a)
		case "p":
			if len(s.rows) > 0 && s.cursor < len(s.rows) {
				row := s.rows[s.cursor]
				return s, func() tea.Msg {
					return pushScreenMsg{Screen: newProxyForm(row.Repo.Name, a.proxyFor(row.Repo.Name))}
				}
			}
		case "/":
			return s, func() tea.Msg { return pushScreenMsg{Screen: newSearchScreen()} }
		case "enter":
			if len(s.rows) > 0 && s.cursor < len(s.rows) {
				row := s.rows[s.cursor]
				return s, func() tea.Msg {
					return pushScreenMsg{Screen: newChartsScreen(row)}
				}
			}
		case "q":
			return s, func() tea.Msg { return quitMsg{} }
		}
	}
	return s, nil
}

func (s *reposScreen) clampCursor() {
	if s.cursor >= len(s.rows) {
		s.cursor = maxInt(len(s.rows)-1, 0)
	}
}

func (s *reposScreen) View() string {
	return s.view(80)
}

func (s *reposScreen) view(width int) string {
	if s.loading {
		return "loading repositories…"
	}
	if len(s.rows) == 0 {
		return "no repositories configured.\nPress a to add one."
	}

	nameW := 10
	for _, r := range s.rows {
		if n := len(r.Repo.Name); n > nameW {
			nameW = n
		}
	}
	// mark + name + url + charts + latency/reason + margins
	avail := width - nameW - 24
	if avail < 8 {
		avail = 8
	}

	var b strings.Builder
	for i, r := range s.rows {
		prefix := "  "
		st := normalStyle
		if i == s.cursor {
			prefix = "> "
			st = selectedStyle
		}
		url := truncate(maskURLCredentials(r.Repo.URL), avail)

		charts := dimStyle.Render("—")
		if r.HasIndex {
			charts = strconv.Itoa(r.Charts)
		}
		mark := statusMark(r.State)
		status := ""
		switch r.State {
		case health.OK:
			status = okStyle.Render(strconv.Itoa(int(r.Latency.Milliseconds())) + "ms")
		case health.Fail:
			status = failStyle.Render(truncate(r.Reason, 14))
		}
		b.WriteString(prefix + st.Render(pad(truncate(r.Repo.Name, nameW), nameW)) + "  " +
			url + "  " + pad(charts, 6) + " " + mark + " " + status + "\n")
	}
	return b.String()
}

func pad(s string, w int) string {
	if lipglossWidth(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-lipglossWidth(s))
}

// maskURLCredentials hides user:password in URLs before display.
func maskURLCredentials(url string) string {
	i := strings.Index(url, "://")
	if i < 0 {
		return url
	}
	rest := url[i+3:]
	at := strings.Index(rest, "@")
	if at < 0 {
		return url
	}
	head := rest[:at]
	c := strings.IndexByte(head, ':')
	if c < 0 {
		return url
	}
	return url[:i+3] + head[:c] + ":***" + rest[at:]
}

// hintRepos is the bottom hint line of the Repos screen.
const hintRepos = "a add  d delete  u update  U update all  p proxy  r recheck  / search  Enter open  q quit"

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
	"github.com/vasyakrg/helm-tui/internal/proxy"
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
	width   int

	// availability check state: healthGen identifies the current run so
	// results of a superseded run are dropped and never extend a chain.
	healthGen    int
	healthCh     <-chan health.Result
	healthCtx    context.Context
	healthCancel context.CancelFunc
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
			return reposLoadFailedMsg{Err: err}
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

// startHealth (re)starts the parallel availability check. Any previous run
// is cancelled, so at most one result chain is alive at a time.
func (s *reposScreen) startHealth(a *app) tea.Cmd {
	if s.healthCancel != nil {
		s.healthCancel()
		s.healthCancel = nil
	}
	s.healthGen++
	if a.deps.Health == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.healthCtx, s.healthCancel = ctx, cancel
	repos := make([]helm.Repo, len(s.rows))
	proxies := make(map[string]string, len(s.rows))
	for i, r := range s.rows {
		repos[i] = r.Repo
		proxies[r.Repo.Name] = a.proxyFor(r.Repo.Name)
	}
	s.healthCh = a.deps.Health(ctx, repos, func(name string) string { return proxies[name] })
	return waitForHealth(ctx, s.healthCh, s.healthGen)
}

// waitForHealth returns a command yielding the next health result of run gen.
func waitForHealth(ctx context.Context, ch <-chan health.Result, gen int) tea.Cmd {
	return func() tea.Msg {
		select {
		case res, ok := <-ch:
			if !ok {
				return nil
			}
			return healthMsg{Gen: gen, Result: res}
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *reposScreen) onClose() {
	if s.healthCancel != nil {
		s.healthCancel()
		s.healthCancel = nil
	}
}

func (s *reposScreen) setSize(width, _ int) { s.width = width }

func (s *reposScreen) Update(msg tea.Msg, a *app) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case reposLoadedMsg:
		s.rows = msg.Rows
		s.loading = false
		s.clampCursor()
		return s, s.startHealth(a)

	case reposLoadFailedMsg:
		s.loading = false
		return s, nil

	case healthMsg:
		if msg.Gen != s.healthGen {
			return s, nil // superseded run
		}
		for i := range s.rows {
			if s.rows[i].Repo.Name == msg.Result.Repo {
				s.rows[i].State = msg.Result.State
				s.rows[i].Latency = msg.Result.Latency
				s.rows[i].Reason = msg.Result.Reason
			}
		}
		return s, waitForHealth(s.healthCtx, s.healthCh, s.healthGen)

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
			return s, s.startHealth(a)
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
	w := s.width
	if w <= 0 {
		w = 80
	}
	return s.view(w)
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
		url := truncate(proxy.MaskText(r.Repo.URL), avail)

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

// hintRepos is the bottom hint line of the Repos screen.
const hintRepos = "a add  d delete  u update  U update all  p proxy  r recheck  / search  Enter open  q quit"

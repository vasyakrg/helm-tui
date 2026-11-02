package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/diff"
	"github.com/vasyakrg/helm-tui/internal/proxy"
)

// DiffSession runs one chart-pair comparison and owns its temp workspace.
type DiffSession interface {
	Run(ctx context.Context, repo, chart, v1, v2, proxyURL string) ([]diff.FileDiff, error)
	Close() error
}

// DiffRunner creates sessions; production implementation over diff package.
type DiffRunner struct {
	Puller diff.Puller
}

func (r DiffRunner) NewSession() (DiffSession, error) {
	ws, err := diff.NewWorkspace("")
	if err != nil {
		return nil, err
	}
	return &diffSession{ws: ws, p: r.Puller}, nil
}

type diffSession struct {
	ws *diff.Workspace
	p  diff.Puller
}

func (s *diffSession) Run(ctx context.Context, repo, chart, v1, v2, proxyURL string) ([]diff.FileDiff, error) {
	pair, err := diff.PullPair(ctx, s.p, s.ws, repo, chart, v1, v2, proxyURL)
	if err != nil {
		return nil, err
	}
	return diff.Compare(pair.OldDir, pair.NewDir)
}

func (s *diffSession) Close() error { return s.ws.Close() }

// diffResultMsg delivers the compare result to the diff screen.
type diffResultMsg struct {
	Files []diff.FileDiff
	Err   error
}

// closer is notified when its screen is popped or the app quits.
type closer interface {
	onClose()
}

type diffScreen struct {
	repo, chart, v1, v2 string
	session             DiffSession
	cancel              context.CancelFunc

	files   []diff.FileDiff
	cursor  int
	hunk    int // selected hunk within the current file
	err     error
	loading bool
}

func newDiffScreen(repo, chart, v1, v2 string) *diffScreen {
	return &diffScreen{repo: repo, chart: chart, v1: v1, v2: v2, loading: true}
}

func (s *diffScreen) Init(a *app) tea.Cmd {
	if a.deps.Diff == nil {
		s.err = fmt.Errorf("diff is not available")
		s.loading = false
		return nil
	}
	session, err := a.deps.Diff()
	if err != nil {
		s.err = err
		s.loading = false
		return nil
	}
	s.session = session
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	repo, chart, v1, v2 := s.repo, s.chart, s.v1, s.v2
	proxyURL := a.proxyFor(repo)
	return func() tea.Msg {
		files, err := session.Run(ctx, repo, chart, v1, v2, proxyURL)
		return diffResultMsg{Files: files, Err: err}
	}
}

func (s *diffScreen) onClose() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.session != nil {
		_ = s.session.Close()
		s.session = nil
	}
}

func (s *diffScreen) Update(msg tea.Msg, a *app) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case diffResultMsg:
		s.loading = false
		s.err = msg.Err
		s.files = msg.Files
		return s, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			if s.cursor < len(s.files)-1 {
				s.cursor++
				s.hunk = 0
			}
		case "k", "up":
			if s.cursor > 0 {
				s.cursor--
				s.hunk = 0
			}
		case "n":
			s.jumpHunk(1)
		case "N":
			s.jumpHunk(-1)
		case "q":
			return s, func() tea.Msg { return quitMsg{} }
		}
	}
	return s, nil
}

func (s *diffScreen) jumpHunk(dir int) {
	if s.cursor >= len(s.files) {
		return
	}
	starts := s.files[s.cursor].HunkStarts
	if len(starts) == 0 {
		return
	}
	// current hunk index = largest start <= s.hunk, or before the first
	cur := -1
	for i, st := range starts {
		if st <= s.hunk {
			cur = i
		}
	}
	next := cur + dir
	if next < 0 {
		next = 0
	}
	if next >= len(starts) {
		next = len(starts) - 1
	}
	s.hunk = starts[next]
}

func (s *diffScreen) View() string {
	title := titleStyle.Render(s.chart + " " + s.v1 + " → " + s.v2)
	switch {
	case s.loading:
		return title + "\n" + "pulling chart versions…"
	case s.err != nil:
		return title + "\n" + errStyle.Render("error: "+proxy.MaskText(s.err.Error()))
	case len(s.files) == 0:
		return title + "\n" + "no differences"
	}

	var b strings.Builder
	b.WriteString(title + " · " + strconv.Itoa(len(s.files)) + " files changed\n")

	if s.cursor >= len(s.files) {
		s.cursor = len(s.files) - 1
	}
	sel := s.files[s.cursor]

	// file list
	b.WriteString("\n")
	for i, f := range s.files {
		prefix := "  "
		st := normalStyle
		if i == s.cursor {
			prefix = "> "
			st = selectedStyle
		}
		mark := modifiedStyle.Render("~")
		switch f.Status {
		case diff.Added:
			mark = addedStyle.Render("+")
		case diff.Removed:
			mark = removedStyle.Render("-")
		}
		note := ""
		if f.Binary {
			note = dimStyle.Render(" (binary)")
		}
		b.WriteString(prefix + mark + " " + st.Render(truncate(f.Path, 40)) + note + "\n")
	}

	// line diff of the selected file
	b.WriteString("\n")
	if sel.Binary {
		b.WriteString(dimStyle.Render("binary file, contents not shown") + "\n")
		return b.String()
	}
	shown := 0
	for i, l := range sel.Lines {
		style := contextStyle
		switch l.Kind {
		case '+':
			style = addedStyle
		case '-':
			style = removedStyle
		}
		b.WriteString(style.Render(string(l.Kind)+truncate(l.Text, 60)) + "\n")
		shown++
		if shown >= 40 {
			b.WriteString(dimStyle.Render("…") + "\n")
			break
		}
		_ = i
	}
	return b.String()
}

const hintDiff = "j/k file  n/N hunk  esc back"

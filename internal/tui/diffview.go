package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/diff"
	"github.com/vasyakrg/helm-tui/internal/index"
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
	fileTop int // first visible row of the file list
	hunk    int // index of the selected hunk in the current file, -1 = none
	scroll  int // first visible line of the diff pane
	err     error
	loading bool

	winSize
}

// newDiffScreen shows the comparison of two versions. The versions are
// ordered old → new (by semver), matching diff.PullPair, so that '+' lines
// belong to the version shown on the right of the arrow.
func newDiffScreen(repo, chart, v1, v2 string) *diffScreen {
	if index.Compare(v1, v2) > 0 {
		v1, v2 = v2, v1
	}
	return &diffScreen{repo: repo, chart: chart, v1: v1, v2: v2, loading: true, hunk: -1}
}

func (s *diffScreen) hints() string { return hintDiff }

// layout returns the number of rows for the file list and the diff pane.
// The height is the body height; two rows go to title and file header.
func (s *diffScreen) layout() (fileRows, diffRows int) {
	h := s.height
	if h <= 0 {
		h = 24
	}
	body := h - 2
	if body < 2 {
		body = 2
	}
	fileRows = body / 3
	if fileRows < 1 {
		fileRows = 1
	}
	if fileRows > len(s.files) {
		fileRows = len(s.files)
	}
	diffRows = body - fileRows
	if diffRows < 1 {
		diffRows = 1
	}
	return fileRows, diffRows
}

func (s *diffScreen) maxScroll() int {
	if s.cursor >= len(s.files) {
		return 0
	}
	_, diffRows := s.layout()
	return maxInt(len(s.files[s.cursor].Lines)-diffRows, 0)
}

func (s *diffScreen) setScroll(v int) {
	if v > s.maxScroll() {
		v = s.maxScroll()
	}
	if v < 0 {
		v = 0
	}
	s.scroll = v
}

func (s *diffScreen) selectFile(i int) {
	s.cursor = i
	s.hunk = -1
	s.scroll = 0
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
		_, diffRows := s.layout()
		switch msg.String() {
		case "j", "down":
			if s.cursor < len(s.files)-1 {
				s.selectFile(s.cursor + 1)
			}
		case "k", "up":
			if s.cursor > 0 {
				s.selectFile(s.cursor - 1)
			}
		case "n":
			s.jumpHunk(1)
		case "N":
			s.jumpHunk(-1)
		case "pgdown", "ctrl+d", "ctrl+f":
			s.setScroll(s.scroll + maxInt(diffRows/2, 1))
		case "pgup", "ctrl+u", "ctrl+b":
			s.setScroll(s.scroll - maxInt(diffRows/2, 1))
		case "q":
			return s, func() tea.Msg { return quitMsg{} }
		}
	}
	return s, nil
}

// jumpHunk selects the next (dir>0) or previous hunk of the current file and
// scrolls the diff pane so that it is visible.
func (s *diffScreen) jumpHunk(dir int) {
	if s.cursor >= len(s.files) {
		return
	}
	starts := s.files[s.cursor].HunkStarts
	if len(starts) == 0 {
		return
	}
	next := s.hunk + dir
	if next < 0 {
		next = 0
	}
	if next >= len(starts) {
		next = len(starts) - 1
	}
	s.hunk = next
	s.setScroll(starts[next] - hunkLeadContext)
}

// hunkLeadContext is the number of unchanged lines shown above a hunk after
// jumping to it.
const hunkLeadContext = 2

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
	width := s.widthOr(80)
	if s.cursor >= len(s.files) {
		s.cursor = len(s.files) - 1
	}
	fileRows, diffRows := s.layout()
	sel := s.files[s.cursor]

	var b strings.Builder
	b.WriteString(title + " · " + strconv.Itoa(len(s.files)) + " files changed\n")

	// file list, windowed around the cursor
	if s.cursor < s.fileTop {
		s.fileTop = s.cursor
	}
	if s.cursor >= s.fileTop+fileRows {
		s.fileTop = s.cursor - fileRows + 1
	}
	for i := s.fileTop; i < len(s.files) && i < s.fileTop+fileRows; i++ {
		f := s.files[i]
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
		b.WriteString(prefix + mark + " " + st.Render(truncate(f.Path, maxInt(width-14, 10))) + note + "\n")
	}

	// header of the diff pane
	pos := "file " + strconv.Itoa(s.cursor+1) + "/" + strconv.Itoa(len(s.files))
	if n := len(sel.HunkStarts); n > 0 {
		h := "-"
		if s.hunk >= 0 {
			h = strconv.Itoa(s.hunk + 1)
		}
		pos += "  hunk " + h + "/" + strconv.Itoa(n)
	}
	if sel.Binary {
		b.WriteString(dimStyle.Render(truncate(sel.Path+"  "+pos, width)) + "\n")
		b.WriteString(dimStyle.Render("binary file, contents not shown"))
		return b.String()
	}
	if total := len(sel.Lines); total > 0 {
		end := minInt(s.scroll+diffRows, total)
		pos += "  lines " + strconv.Itoa(s.scroll+1) + "-" + strconv.Itoa(end) + "/" + strconv.Itoa(total)
	}
	b.WriteString(dimStyle.Render(truncate(sel.Path+"  "+pos, width)))

	// visible window of the line diff
	s.setScroll(s.scroll)
	for i := s.scroll; i < len(sel.Lines) && i < s.scroll+diffRows; i++ {
		l := sel.Lines[i]
		style := contextStyle
		switch l.Kind {
		case '+':
			style = addedStyle
		case '-':
			style = removedStyle
		}
		text := strings.ReplaceAll(l.Text, "\t", "    ")
		b.WriteString("\n" + style.Render(string(l.Kind)+truncate(text, maxInt(width-1, 1))))
	}
	return b.String()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

const hintDiff = "j/k file  n/N hunk  pgup/pgdn scroll  esc back"

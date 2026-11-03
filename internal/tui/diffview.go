package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

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
	hscroll int // first visible column of the diff pane
	// focusDiff: the diff pane has the focus (j/k scroll it); otherwise the
	// file list does.
	focusDiff bool
	err       error
	loading   bool

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

func (s *diffScreen) hints() string {
	if s.focusDiff {
		return hintDiffPane
	}
	return hintDiff
}

// consumeEsc returns focus from the diff pane to the file list instead of
// leaving the screen.
func (s *diffScreen) consumeEsc(*app) bool {
	if s.focusDiff {
		s.focusDiff = false
		return true
	}
	return false
}

const (
	diffMinListW = 24
	diffMaxListW = 48
	diffSepW     = 3 // " │ "
	hscrollStep  = 8
)

// paneWidths splits the window into the file list (left) and the diff (right).
func (s *diffScreen) paneWidths() (left, right int) {
	w := s.widthOr(80)
	left = w * 35 / 100
	if left < diffMinListW {
		left = diffMinListW
	}
	if left > diffMaxListW {
		left = diffMaxListW
	}
	if left > w-diffSepW-10 { // keep some room for the diff on narrow windows
		left = maxInt(w-diffSepW-10, 8)
	}
	right = maxInt(w-left-diffSepW, 1)
	return left, right
}

// layout returns the number of rows of the file list and of the diff pane
// (both exclude the pane header). The body height holds the title row, the
// pane headers and the panes.
func (s *diffScreen) layout() (fileRows, diffRows int) {
	h := s.height
	if h <= 0 {
		h = 24
	}
	rows := maxInt(h-2, 1)
	return rows, rows
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

// maxHScroll is the largest useful horizontal offset of the current file.
func (s *diffScreen) maxHScroll() int {
	if s.cursor >= len(s.files) {
		return 0
	}
	_, rw := s.paneWidths()
	longest := 0
	for _, l := range s.files[s.cursor].Lines {
		longest = maxInt(longest, lipglossWidth(expandTabs(l.Text))+1)
	}
	return maxInt(longest-rw, 0)
}

func (s *diffScreen) setHScroll(v int) {
	if v > s.maxHScroll() {
		v = s.maxHScroll()
	}
	if v < 0 {
		v = 0
	}
	s.hscroll = v
}

func (s *diffScreen) selectFile(i int) {
	s.cursor = i
	s.hunk = -1
	s.scroll = 0
	s.hscroll = 0
}

func expandTabs(t string) string { return strings.ReplaceAll(t, "\t", "    ") }

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
		case "tab", "shift+tab":
			s.focusDiff = !s.focusDiff
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
		default:
			if s.focusDiff {
				s.diffPaneKey(msg.String())
			} else {
				s.listKey(msg.String())
			}
		}
	}
	return s, nil
}

// listKey handles keys while the file list has the focus.
func (s *diffScreen) listKey(k string) {
	switch k {
	case "j", "down":
		if s.cursor < len(s.files)-1 {
			s.selectFile(s.cursor + 1)
		}
	case "k", "up":
		if s.cursor > 0 {
			s.selectFile(s.cursor - 1)
		}
	case "g", "home":
		if len(s.files) > 0 {
			s.selectFile(0)
		}
	case "G", "end":
		if len(s.files) > 0 {
			s.selectFile(len(s.files) - 1)
		}
	case "enter", "right", "l":
		if len(s.files) > 0 {
			s.focusDiff = true
		}
	}
}

// diffPaneKey handles keys while the diff pane has the focus: line and column
// scrolling.
func (s *diffScreen) diffPaneKey(k string) {
	switch k {
	case "j", "down":
		s.setScroll(s.scroll + 1)
	case "k", "up":
		s.setScroll(s.scroll - 1)
	case "g", "home":
		s.setScroll(0)
	case "G", "end":
		s.setScroll(s.maxScroll())
	case "l", "right":
		s.setHScroll(s.hscroll + hscrollStep)
	case "h", "left":
		if s.hscroll == 0 {
			s.focusDiff = false // nothing to pan back: return to the files
			return
		}
		s.setHScroll(s.hscroll - hscrollStep)
	case "0":
		s.setHScroll(0)
	}
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

// truncateLeft keeps the tail of s (the file name) when it does not fit.
func truncateLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := lipglossWidth(s)
	if w <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return ansi.TruncateLeft(s, w-(width-1), "…")
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
	if s.cursor >= len(s.files) {
		s.cursor = len(s.files) - 1
	}
	lw, rw := s.paneWidths()
	rows, _ := s.layout()
	sel := s.files[s.cursor]
	s.setScroll(s.scroll)
	s.setHScroll(s.hscroll)

	// file list window around the cursor
	if s.cursor < s.fileTop {
		s.fileTop = s.cursor
	}
	if s.cursor >= s.fileTop+rows {
		s.fileTop = s.cursor - rows + 1
	}
	if s.fileTop > len(s.files)-rows {
		s.fileTop = maxInt(len(s.files)-rows, 0)
	}

	// pane headers
	listHdr := "FILES " + strconv.Itoa(s.cursor+1) + "/" + strconv.Itoa(len(s.files))
	pos := sel.Path
	if n := len(sel.HunkStarts); n > 0 {
		h := "-"
		if s.hunk >= 0 {
			h = strconv.Itoa(s.hunk + 1)
		}
		pos += "  hunk " + h + "/" + strconv.Itoa(n)
	}
	if total := len(sel.Lines); total > 0 && !sel.Binary {
		end := minInt(s.scroll+rows, total)
		pos += "  lines " + strconv.Itoa(s.scroll+1) + "-" + strconv.Itoa(end) + "/" + strconv.Itoa(total)
	}
	if s.hscroll > 0 {
		pos += "  col " + strconv.Itoa(s.hscroll+1)
	}
	hdrL, hdrR := dimStyle, dimStyle
	if s.focusDiff {
		hdrR = selectedStyle
	} else {
		hdrL = selectedStyle
	}

	var b strings.Builder
	b.WriteString(title + " · " + strconv.Itoa(len(s.files)) + " files changed\n")
	sep := dimStyle.Render(" │ ")
	b.WriteString(pad(hdrL.Render(truncate(listHdr, lw)), lw) + sep + hdrR.Render(truncateLeft(pos, rw)))
	for i := 0; i < rows; i++ {
		b.WriteString("\n" + pad(s.fileCell(s.fileTop+i, lw), lw) + sep + s.diffCell(sel, i, rw))
	}
	return b.String()
}

// fileCell renders one row of the file list ("" past the end).
func (s *diffScreen) fileCell(i, width int) string {
	if i >= len(s.files) {
		return ""
	}
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
	return prefix + mark + " " + st.Render(truncateLeft(f.Path, maxInt(width-4, 1)))
}

// diffCell renders row i of the visible window of the selected file's diff,
// panned horizontally by hscroll.
func (s *diffScreen) diffCell(sel diff.FileDiff, i, width int) string {
	if sel.Binary {
		if i == 0 {
			return dimStyle.Render("binary file, contents not shown")
		}
		return ""
	}
	idx := s.scroll + i
	if idx >= len(sel.Lines) {
		return ""
	}
	l := sel.Lines[idx]
	style := contextStyle
	switch l.Kind {
	case '+':
		style = addedStyle
	case '-':
		style = removedStyle
	}
	text := string(l.Kind) + expandTabs(l.Text)
	return style.Render(ansi.Cut(text, s.hscroll, s.hscroll+width))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

const (
	hintDiff     = "j/k file  tab/enter diff pane  n/N hunk  pgup/pgdn scroll  esc back"
	hintDiffPane = "j/k scroll  h/l pan  g/G top/end  n/N hunk  tab files  esc files"
)

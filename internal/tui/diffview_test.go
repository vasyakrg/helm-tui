package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/diff"
)

type fakeSession struct {
	dir      string
	runErr   error
	files    []diff.FileDiff
	blockCtx bool
	runCalls int
	closed   int
}

func (f *fakeSession) Run(ctx context.Context, _, _, _, _, _ string) ([]diff.FileDiff, error) {
	f.runCalls++
	if f.blockCtx {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.files, f.runErr
}

func (f *fakeSession) Close() error {
	f.closed++
	if f.dir != "" {
		os.RemoveAll(f.dir)
	}
	return nil
}

func diffDeps(s *fakeSession) Deps {
	deps, _ := fakeDeps(nil, nil)
	deps.Diff = func() (DiffSession, error) { return s, nil }
	return deps
}

func newDiffApp(t *testing.T, s *fakeSession) *app {
	deps := diffDeps(s)
	a := New(deps).(*app)
	return send(t, a, pushScreenMsg{Screen: newDiffScreen("repo", "chart", "1.0.0", "2.0.0")})
}

func TestDiff_ShowsSpinnerThenFiles(t *testing.T) {
	// blockCtx keeps Run pending so the spinner phase is observable
	s := &fakeSession{blockCtx: true, files: []diff.FileDiff{
		{Path: "Chart.yaml", Status: diff.Modified},
		{Path: "values.yaml", Status: diff.Modified},
	}}
	deps := diffDeps(s)
	a := New(deps).(*app)
	ds := newDiffScreen("repo", "chart", "1.0.0", "2.0.0")
	a = send(t, a, pushScreenMsg{Screen: ds})
	if !contains(ds.View(), "pulling") {
		t.Errorf("spinner missing:\n%s", ds.View())
	}
	a = send(t, a, diffResultMsg{Files: s.files})
	v := ds.View()
	if !contains(v, "Chart.yaml") || !contains(v, "values.yaml") || !contains(v, "2 files changed") {
		t.Errorf("files missing:\n%s", v)
	}
}

func TestDiff_PullErrorShownAndNoPartialDiff(t *testing.T) {
	s := &fakeSession{runErr: errors.New("pull failed")}
	a := newDiffApp(t, s)
	ds := a.top().(*diffScreen)
	a = send(t, a, diffResultMsg{Err: s.runErr})
	v := ds.View()
	if !contains(v, "error") || !contains(v, "pull failed") {
		t.Errorf("error not shown:\n%s", v)
	}
	if contains(v, "Chart.yaml") {
		t.Errorf("partial diff shown:\n%s", v)
	}
}

func TestDiff_EscCancelsPullAndClosesWorkspace(t *testing.T) {
	dir := t.TempDir()
	s := &fakeSession{dir: dir, blockCtx: true}
	a := newDiffApp(t, s)
	a = send(t, a, key("esc")) // pops and closes
	if s.closed == 0 {
		t.Error("session not closed on esc")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("workspace dir still exists: %v", err)
	}
}

func TestDiff_QuitClosesWorkspace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ws")
	s := &fakeSession{dir: dir}
	a := newDiffApp(t, s)
	a = send(t, a, key("q"))
	if s.closed == 0 {
		t.Error("session not closed on quit")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("workspace dir still exists: %v", err)
	}
}

func TestDiff_NKeyJumpsToNextHunk(t *testing.T) {
	s := &fakeSession{files: []diff.FileDiff{{
		Path:   "values.yaml",
		Status: diff.Modified,
		Lines: []diff.Line{
			{Kind: ' ', Text: "a"},
			{Kind: '-', Text: "b"},
			{Kind: '+', Text: "B"},
			{Kind: ' ', Text: "c"},
			{Kind: ' ', Text: "d"},
			{Kind: '-', Text: "e"},
			{Kind: '+', Text: "E"},
		},
		HunkStarts: []int{1, 5},
	}}}
	a := newDiffApp(t, s)
	ds := a.top().(*diffScreen)
	a = send(t, a, diffResultMsg{Files: s.files})
	a = send(t, a, key("n"))
	if ds.hunk != 0 {
		t.Errorf("hunk after n = %d, want 0", ds.hunk)
	}
	a = send(t, a, key("n"))
	if ds.hunk != 1 {
		t.Errorf("hunk after second n = %d, want 1", ds.hunk)
	}
	a = send(t, a, key("n"))
	if ds.hunk != 1 {
		t.Errorf("hunk must not run past the last one, got %d", ds.hunk)
	}
	a = send(t, a, key("N"))
	if ds.hunk != 0 {
		t.Errorf("hunk after N = %d, want 0", ds.hunk)
	}
}

func TestDiff_BinaryFileShowsPlaceholder(t *testing.T) {
	s := &fakeSession{files: []diff.FileDiff{
		{Path: "logo.png", Status: diff.Modified, Binary: true},
	}}
	a := newDiffApp(t, s)
	ds := a.top().(*diffScreen)
	a = send(t, a, diffResultMsg{Files: s.files})
	if !contains(ds.View(), "binary") {
		t.Errorf("binary placeholder missing:\n%s", ds.View())
	}
}

func TestDiff_NoDifferencesMessage(t *testing.T) {
	s := &fakeSession{files: nil}
	a := newDiffApp(t, s)
	ds := a.top().(*diffScreen)
	a = send(t, a, diffResultMsg{Files: nil})
	if !contains(ds.View(), "no differences") {
		t.Errorf("message missing:\n%s", ds.View())
	}
}

func bigDiffFile() diff.FileDiff {
	var lines []diff.Line
	var hunks []int
	for i := 0; i < 120; i++ {
		switch i {
		case 10, 55, 100:
			hunks = append(hunks, len(lines))
			lines = append(lines, diff.Line{Kind: '-', Text: fmt.Sprintf("old %d", i)}, diff.Line{Kind: '+', Text: fmt.Sprintf("new %d", i)})
		default:
			lines = append(lines, diff.Line{Kind: ' ', Text: fmt.Sprintf("ctx %d", i)})
		}
	}
	return diff.FileDiff{Path: "values.yaml", Status: diff.Modified, Lines: lines, HunkStarts: hunks}
}

func viewLines(a *app) int { return strings.Count(strings.TrimRight(a.View().Content, "\n"), "\n") + 1 }

func TestDiff_ViewFitsWindowAndHunkJumpScrolls(t *testing.T) {
	s := &fakeSession{files: []diff.FileDiff{bigDiffFile()}}
	for i := 0; i < 30; i++ {
		s.files = append(s.files, diff.FileDiff{Path: fmt.Sprintf("templates/f%02d.yaml", i), Status: diff.Modified})
	}
	a := newDiffApp(t, s)
	a = send(t, a, tea.WindowSizeMsg{Width: 80, Height: 14}, diffResultMsg{Files: s.files})
	if n := viewLines(a); n > 14 {
		t.Fatalf("view has %d lines, window is 14:\n%s", n, a.View().Content)
	}
	a = send(t, a, key("n"))
	v := a.View().Content
	if !contains(v, "new 10") || contains(v, "ctx 0\n") {
		t.Errorf("first hunk not scrolled into view:\n%s", v)
	}
	a = send(t, a, key("n"), key("n"))
	v = a.View().Content
	if !contains(v, "new 100") || contains(v, "new 10\n") {
		t.Errorf("third hunk not in view:\n%s", v)
	}
	if n := viewLines(a); n > 14 {
		t.Errorf("view has %d lines after scroll", n)
	}
	a = send(t, a, key("N"))
	if v = a.View().Content; !contains(v, "new 55") {
		t.Errorf("N did not go back to previous hunk:\n%s", v)
	}
}

func TestDiff_FileListIsWindowedAroundCursor(t *testing.T) {
	var files []diff.FileDiff
	for i := 0; i < 40; i++ {
		files = append(files, diff.FileDiff{Path: fmt.Sprintf("f%02d.yaml", i), Status: diff.Modified})
	}
	s := &fakeSession{files: files}
	a := newDiffApp(t, s)
	a = send(t, a, tea.WindowSizeMsg{Width: 80, Height: 14}, diffResultMsg{Files: files})
	for i := 0; i < 30; i++ {
		a = send(t, a, key("j"))
	}
	v := a.View().Content
	if !contains(v, "f30.yaml") {
		t.Errorf("selected file not visible:\n%s", v)
	}
	if n := viewLines(a); n > 14 {
		t.Errorf("view has %d lines", n)
	}
}

func TestDiff_TitleIsOldToNew(t *testing.T) {
	ds := newDiffScreen("r", "hub-manager", "1.0.1", "1.0.0")
	if v := ds.View(); !contains(v, "hub-manager 1.0.0 → 1.0.1") {
		t.Errorf("title:\n%s", v)
	}
}

func TestDiff_FilesLeftDiffRight(t *testing.T) {
	s := &fakeSession{files: []diff.FileDiff{bigDiffFile()}}
	a := newDiffApp(t, s)
	a = send(t, a, tea.WindowSizeMsg{Width: 100, Height: 20}, diffResultMsg{Files: s.files})
	var found bool
	for _, line := range strings.Split(plain(a.View()), "\n") {
		li, ri := strings.Index(line, "values.yaml"), strings.Index(line, "ctx 0")
		if li >= 0 && ri > li && strings.Contains(line, "│") {
			found = true
		}
	}
	if !found {
		t.Errorf("file list must be on the left and the diff on the right:\n%s", plain(a.View()))
	}
}

func TestDiff_FocusedPaneScrollsLineByLineAndEscReturns(t *testing.T) {
	s := &fakeSession{files: []diff.FileDiff{bigDiffFile(), {Path: "z.yaml", Status: diff.Modified}}}
	a := newDiffApp(t, s)
	a = send(t, a, tea.WindowSizeMsg{Width: 100, Height: 14}, diffResultMsg{Files: s.files})
	a = send(t, a, key("j")) // file list focus: selects next file
	if ds := a.top().(*diffScreen); ds.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", ds.cursor)
	}
	a = send(t, a, key("k"), key("tab"))
	ds := a.top().(*diffScreen)
	if !ds.focusDiff {
		t.Fatal("tab must focus the diff pane")
	}
	a = send(t, a, key("j"), key("j"), key("j"))
	if ds.cursor != 0 || ds.scroll != 3 {
		t.Fatalf("j in diff pane must scroll: cursor=%d scroll=%d", ds.cursor, ds.scroll)
	}
	a = send(t, a, key("G"))
	if ds.scroll != ds.maxScroll() || ds.scroll == 0 {
		t.Errorf("G must scroll to the end: %d/%d", ds.scroll, ds.maxScroll())
	}
	a = send(t, a, key("esc"))
	if _, ok := a.top().(*diffScreen); !ok || ds.focusDiff {
		t.Fatalf("esc must return focus to the file list, top=%T focus=%v", a.top(), ds.focusDiff)
	}
}

func TestDiff_LongLinesPanHorizontally(t *testing.T) {
	long := strings.Repeat("0123456789", 30) + "END"
	s := &fakeSession{files: []diff.FileDiff{{
		Path: "values.yaml", Status: diff.Modified,
		Lines: []diff.Line{{Kind: '+', Text: long}},
	}}}
	a := newDiffApp(t, s)
	a = send(t, a, tea.WindowSizeMsg{Width: 100, Height: 14}, diffResultMsg{Files: s.files})
	if contains(plain(a.View()), "END") {
		t.Fatal("long line must be cut at the pane edge")
	}
	a = send(t, a, key("tab"))
	for i := 0; i < 60 && !contains(plain(a.View()), "END"); i++ {
		a = send(t, a, key("l"))
	}
	if !contains(plain(a.View()), "END") {
		t.Errorf("panning right must reveal the end of the line:\n%s", plain(a.View()))
	}
	for i := 0; i < 60; i++ {
		a = send(t, a, key("h"))
	}
	ds := a.top().(*diffScreen)
	if ds.hscroll != 0 || ds.focusDiff {
		t.Errorf("panning back to column 0 then h returns focus: hscroll=%d focus=%v", ds.hscroll, ds.focusDiff)
	}
}

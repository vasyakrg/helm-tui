package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

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
	if ds.hunk != 1 {
		t.Errorf("hunk after n = %d, want 1", ds.hunk)
	}
	a = send(t, a, key("n"))
	if ds.hunk != 5 {
		t.Errorf("hunk after second n = %d, want 5", ds.hunk)
	}
	a = send(t, a, key("N"))
	if ds.hunk != 1 {
		t.Errorf("hunk after N = %d, want 1", ds.hunk)
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

package diff

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/vasyakrg/helm-tui/internal/helm"
)

func TestWorkspace_CloseRemovesDir(t *testing.T) {
	w, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.Dir); err != nil {
		t.Fatalf("workspace dir missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(w.Dir, "pid")); err != nil {
		t.Errorf("pid file missing: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.Dir); !os.IsNotExist(err) {
		t.Errorf("dir still exists after Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestCleanStale_RemovesDeadPidKeepsLive(t *testing.T) {
	root := t.TempDir()

	live := filepath.Join(root, "helm-tui-live")
	if err := os.MkdirAll(live, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(live, "pid"), strconv.Itoa(os.Getpid()))

	dead := filepath.Join(root, "helm-tui-dead")
	if err := os.MkdirAll(dead, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dead, "pid"), strconv.Itoa(99999999))

	other := filepath.Join(root, "unrelated")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}

	n, err := CleanStale(root)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("removed %d, want 1", n)
	}
	if _, err := os.Stat(live); err != nil {
		t.Errorf("live workspace removed: %v", err)
	}
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Errorf("dead workspace kept: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("unrelated dir removed: %v", err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type fakePuller struct {
	calls []string
	errOn string
}

func (f *fakePuller) Pull(_ context.Context, repo, chart, version, destDir, _ string) error {
	f.calls = append(f.calls, repo+"/"+chart+"@"+version)
	if f.errOn == version {
		return errors.New("pull failed")
	}
	if err := os.MkdirAll(filepath.Join(destDir, chart), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(destDir, chart, "Chart.yaml"), []byte("name: "+chart), 0o644)
}

func TestPullPair_OrdersBySemver(t *testing.T) {
	w, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	p := &fakePuller{}
	pair, err := PullPair(context.Background(), p, w, "repo", "chart", "2.0.0", "1.0.0", "")
	if err != nil {
		t.Fatal(err)
	}
	if pair.OldVersion != "1.0.0" || pair.NewVersion != "2.0.0" {
		t.Errorf("pair = %+v", pair)
	}
	if _, err := os.Stat(pair.OldDir); err != nil {
		t.Errorf("OldDir missing: %v", err)
	}
	if _, err := os.Stat(pair.NewDir); err != nil {
		t.Errorf("NewDir missing: %v", err)
	}
	if len(p.calls) != 2 {
		t.Errorf("pull calls = %v", p.calls)
	}
}

func TestPullPair_CancelledContext_ReturnsErrorAndWorkspaceClosable(t *testing.T) {
	w, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &fakePuller{}
	if _, err := PullPair(ctx, p, w, "r", "c", "1.0.0", "2.0.0", ""); err == nil {
		t.Error("want error for cancelled context")
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close after failed pull: %v", err)
	}
}

func TestPullPair_FailureOfOneVersionReturnsError(t *testing.T) {
	w, _ := NewWorkspace(t.TempDir())
	defer w.Close()
	p := &fakePuller{errOn: "2.0.0"}
	pair, err := PullPair(context.Background(), p, w, "r", "c", "1.0.0", "2.0.0", "")
	if err == nil {
		t.Error("want error")
	}
	if pair.OldDir != "" || pair.NewDir != "" {
		t.Errorf("got partial result: %+v", pair)
	}
	// old version pulled fine, new version failed: no third call
	if len(p.calls) != 2 {
		t.Errorf("calls = %v", p.calls)
	}
}

// *helm.Client must satisfy Puller
var _ Puller = (*helm.Client)(nil)

func TestPullPair_RejectsPathTraversalInVersionOrChart(t *testing.T) {
	cases := []struct{ chart, v1, v2 string }{
		{"c", "../../x", "1.0.0"},
		{"c", "1.0.0", "a/b"},
		{"c", "..", "1.0.0"},
		{"c", "", "1.0.0"},
		{"../evil", "1.0.0", "2.0.0"},
		{"a/b", "1.0.0", "2.0.0"},
		{"..", "1.0.0", "2.0.0"},
		{"c", `a\b`, "1.0.0"},
	}
	for _, c := range cases {
		root := t.TempDir()
		w, err := NewWorkspace(filepath.Join(root, "ws"))
		if err != nil {
			t.Fatal(err)
		}
		p := &fakePuller{}
		if _, err := PullPair(context.Background(), p, w, "r", c.chart, c.v1, c.v2, ""); err == nil {
			t.Errorf("chart=%q v1=%q v2=%q: want error", c.chart, c.v1, c.v2)
		}
		if len(p.calls) != 0 {
			t.Errorf("chart=%q v1=%q v2=%q: pull was called: %v", c.chart, c.v1, c.v2, p.calls)
		}
		// nothing may be created outside the workspace dir
		entries, _ := os.ReadDir(filepath.Join(root, "ws"))
		if len(entries) != 1 { // only helm-tui-<id>
			t.Errorf("unexpected entries beside workspace: %v", entries)
		}
		w.Close()
	}
}

func TestPullPair_UsesFixedSubdirs(t *testing.T) {
	w, _ := NewWorkspace(t.TempDir())
	defer w.Close()
	pair, err := PullPair(context.Background(), &fakePuller{}, w, "r", "c", "1.0.0+build", "2.0.0", "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(pair.OldDir) != filepath.Join(w.Dir, "old") || filepath.Dir(pair.NewDir) != filepath.Join(w.Dir, "new") {
		t.Errorf("dirs = %q %q", pair.OldDir, pair.NewDir)
	}
}

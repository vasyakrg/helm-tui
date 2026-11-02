package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vasyakrg/helm-tui/internal/health"
	"github.com/vasyakrg/helm-tui/internal/helm"
	"github.com/vasyakrg/helm-tui/internal/index"
)

func manyRepos(n int) []string {
	var names []string
	for i := 0; i < n; i++ {
		names = append(names, fmt.Sprintf("repo%02d", i))
	}
	return names
}

func sized(t *testing.T, a *app, w, h int) *app {
	return send(t, a, tea.WindowSizeMsg{Width: w, Height: h})
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

func TestChrome_HintLineIsLastAndTruncated(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	a := sized(t, New(deps).(*app), 40, 20)
	a = loadRepos(t, a, "r1")
	a = send(t, a, statusMsg{Text: "hello status"})
	v := plain(a.View())
	lines := strings.Split(strings.TrimRight(v, "\n"), "\n")
	if !contains(lines[0], "hello status") {
		t.Errorf("status must be the first line:\n%s", v)
	}
	last := lines[len(lines)-1]
	if !contains(last, "a add") {
		t.Errorf("hint line missing:\n%s", v)
	}
	if lipglossWidth(last) > 40 {
		t.Errorf("hint not truncated to width: %d", lipglossWidth(last))
	}
	if len(lines) != 20 {
		t.Errorf("hint must sit on the bottom row, got %d lines", len(lines))
	}
}

func TestChrome_EveryScreenHasHint(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	screens := map[string]screen{
		"/ filter": newChartsScreen(RepoRow{Repo: helm.Repo{Name: "r"}}),
		"mark":     newVersionsScreen("r", mkChart("c", "1.0.0")),
		"filter":   newSearchScreen(),
		"file":     newDiffScreen("r", "c", "1.0.0", "2.0.0"),
		"submit":   newAddForm(),
		"y":        newDeleteDialog(RepoRow{Repo: helm.Repo{Name: "r"}}),
		"ctrl+d":   newProxyForm("r", ""),
	}
	for want, sc := range screens {
		a := send(t, New(deps), pushScreenMsg{Screen: sc})
		a = sized(t, a, 100, 20)
		if !contains(lastLine(plain(a.View())), want) {
			t.Errorf("%T: hint %q missing in last line %q", sc, want, lastLine(plain(a.View())))
		}
	}
}

func plain(v tea.View) string { return ansi.Strip(v.Content) }

func cursorVisible(v, marker string) bool { return strings.Contains(v, "> "+marker) }

func TestRepos_WindowKeepsCursorVisible(t *testing.T) {
	for _, h := range []int{6, 12, 30, 60} {
		deps, _ := fakeDeps(nil, nil)
		a := sized(t, New(deps).(*app), 112, h)
		a = loadRepos(t, a, manyRepos(42)...)
		for i := 0; i < 41; i++ {
			a = send(t, a, key("j"))
			v := plain(a.View())
			if n := strings.Count(strings.TrimRight(v, "\n"), "\n") + 1; n > h {
				t.Fatalf("h=%d: view has %d lines", h, n)
			}
			if !cursorVisible(v, fmt.Sprintf("repo%02d", i+1)) {
				t.Fatalf("h=%d: cursor repo%02d not visible:\n%s", h, i+1, v)
			}
		}
		a = send(t, a, key("g"))
		if !cursorVisible(plain(a.View()), "repo00") {
			t.Errorf("h=%d: cursor not visible after g", h)
		}
	}
}

func TestRepos_HeaderAndAlignedColumns(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	a := sized(t, New(deps).(*app), 112, 30)
	a = send(t, a, reposLoadedMsg{Rows: []RepoRow{
		{Repo: helm.Repo{Name: "a", URL: "https://short"}, HasIndex: true, Charts: 5, State: health.OK},
		{Repo: helm.Repo{Name: "longer-name", URL: "https://a-much-longer-url.example.com/charts"}, HasIndex: true, Charts: 1234, State: health.OK},
	}})
	v := plain(a.View())
	for _, h := range []string{"NAME", "URL", "CHARTS", "STATUS"} {
		if !contains(v, h) {
			t.Errorf("header %s missing:\n%s", h, v)
		}
	}
	var marks []int
	for _, l := range strings.Split(v, "\n") {
		if i := strings.Index(l, "●"); i >= 0 {
			marks = append(marks, len([]rune(l[:i])))
		}
	}
	if len(marks) != 2 || marks[0] != marks[1] {
		t.Errorf("status column not aligned: %v\n%s", marks, v)
	}
}

func TestCharts_WindowKeepsCursorVisible(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	a := sized(t, New(deps).(*app), 100, 10)
	s := newChartsScreen(RepoRow{Repo: helm.Repo{Name: "testrepo"}})
	a = send(t, a, pushScreenMsg{Screen: s})
	var charts []index.Chart
	for i := 0; i < 50; i++ {
		charts = append(charts, mkChart(fmt.Sprintf("chart%02d", i), "1.0.0"))
	}
	a = send(t, a, chartsLoadedMsg{Repo: "testrepo", Charts: charts})
	for i := 0; i < 49; i++ {
		a = send(t, a, key("j"))
		v := plain(a.View())
		if n := strings.Count(strings.TrimRight(v, "\n"), "\n") + 1; n > 10 {
			t.Fatalf("view has %d lines", n)
		}
		if !cursorVisible(v, fmt.Sprintf("chart%02d", i+1)) {
			t.Fatalf("cursor chart%02d not visible:\n%s", i+1, v)
		}
	}
}

func TestVersions_WindowKeepsCursorVisible(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	a := sized(t, New(deps).(*app), 100, 10)
	var vs []string
	for i := 0; i < 60; i++ {
		vs = append(vs, fmt.Sprintf("1.0.%d", i))
	}
	a = send(t, a, pushScreenMsg{Screen: newVersionsScreen("r", mkChart("c", vs...))})
	for i := 0; i < 59; i++ {
		a = send(t, a, key("j"))
		v := plain(a.View())
		if n := strings.Count(strings.TrimRight(v, "\n"), "\n") + 1; n > 10 {
			t.Fatalf("view has %d lines", n)
		}
		if !strings.Contains(v, fmt.Sprintf("> [ ] 1.0.%d ", i+1)) {
			t.Fatalf("cursor 1.0.%d not visible:\n%s", i+1, v)
		}
	}
}

func TestSearch_WindowKeepsCursorVisible(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	a := sized(t, New(deps).(*app), 100, 10)
	ss := newSearchScreen()
	a = send(t, a, pushScreenMsg{Screen: ss})
	var charts []index.Chart
	for i := 0; i < 50; i++ {
		charts = append(charts, mkChart(fmt.Sprintf("chart%02d", i), "1.0.0"))
	}
	a = send(t, a, indexesLoadedMsg{Idxs: []*index.Index{{Repo: "r", Charts: charts}}})
	for i := 0; i < 49; i++ {
		a = send(t, a, key("down"))
		v := plain(a.View())
		if n := strings.Count(strings.TrimRight(v, "\n"), "\n") + 1; n > 10 {
			t.Fatalf("view has %d lines", n)
		}
		if !cursorVisible(v, fmt.Sprintf("r/chart%02d", i+1)) {
			t.Fatalf("cursor chart%02d not visible:\n%s", i+1, v)
		}
	}
}

func TestRepos_UpdateRefreshesChartCountsWithoutRestartingHealth(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	deps, _ := fakeDeps([]helm.Repo{{Name: "r1", URL: "http://r1"}}, nil)
	deps.Env.RepositoryCache = dir
	inner := deps.Health
	deps.Health = func(ctx context.Context, repos []helm.Repo, f func(string) string) <-chan health.Result {
		calls++
		return inner(ctx, repos, f)
	}
	a := loadRepos(t, New(deps).(*app), "r1")
	a.stack[0].(*reposScreen).rows[0].State = health.OK
	a.stack[0].(*reposScreen).rows[0].HasIndex = false
	before := calls
	if err := os.WriteFile(filepath.Join(dir, "r1-index.yaml"), []byte("entries:\n  a:\n    - version: 1.0.0\n  b:\n    - version: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a = send(t, a, key("u"))
	row := a.stack[0].(*reposScreen).rows[0]
	if !row.HasIndex || row.Charts != 2 {
		t.Errorf("counts not refreshed: %+v", row)
	}
	if row.State != health.OK || calls != before {
		t.Errorf("health restarted: state=%v calls=%d->%d", row.State, before, calls)
	}
	// U (update all) too
	a.stack[0].(*reposScreen).rows[0].Charts = 0
	a = send(t, a, key("U"))
	if a.stack[0].(*reposScreen).rows[0].Charts != 2 {
		t.Errorf("U did not refresh counts")
	}
}

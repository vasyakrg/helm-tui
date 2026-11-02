package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/health"
	"github.com/vasyakrg/helm-tui/internal/helm"
	"github.com/vasyakrg/helm-tui/internal/index"
)

func writeIndex(t *testing.T, dir, repo string, charts ...string) {
	t.Helper()
	body := "apiVersion: v1\nentries:\n"
	for _, c := range charts {
		body += "  " + c + ":\n    - version: 1.0.0\n      description: " + c + " chart\n"
	}
	if err := os.WriteFile(filepath.Join(dir, repo+"-index.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func loadRepos(t *testing.T, a *app, names ...string) *app {
	var rows []RepoRow
	for _, n := range names {
		rows = append(rows, RepoRow{Repo: helm.Repo{Name: n, URL: "http://" + n}, HasIndex: true})
	}
	return send(t, a, reposLoadedMsg{Rows: rows})
}

func rootRepos(a *app) *reposScreen { return a.stack[0].(*reposScreen) }

// --- item 1: forms close on success, stay on error, list reloads ---

func typeAddForm(t *testing.T, a *app, name, url string) *app {
	return send(t, a, name, key("\t"), url, key("enter"))
}

func TestAddForm_SuccessClosesFormAndReloadsRepos(t *testing.T) {
	deps, fh := fakeDeps([]helm.Repo{{Name: "old", URL: "http://old"}}, nil)
	a := loadRepos(t, New(deps).(*app), "old")
	a = send(t, a, key("a"))
	if _, ok := a.top().(*addForm); !ok {
		t.Fatalf("top = %T", a.top())
	}
	a = typeAddForm(t, a, "fresh", "https://fresh.example.com")
	if len(fh.added) != 1 {
		t.Fatalf("Add calls = %d", len(fh.added))
	}
	if _, ok := a.top().(*reposScreen); !ok {
		t.Fatalf("form not closed, top = %T", a.top())
	}
	if rs := rootRepos(a); len(rs.rows) != 2 || rs.rows[1].Repo.Name != "fresh" {
		t.Errorf("repo list not reloaded: %+v", rs.rows)
	}
}

func TestAddForm_ErrorKeepsFormWithMaskedMessage(t *testing.T) {
	deps, fh := fakeDeps(nil, nil)
	fh.addErr = errors.New("Get \"HTTPS://u:p@ss1@h/index.yaml\": denied")
	a := loadRepos(t, New(deps).(*app))
	a = send(t, a, key("a"))
	f := a.top().(*addForm)
	a = typeAddForm(t, a, "fresh", "https://fresh.example.com")
	if a.top() != screen(f) {
		t.Fatalf("form closed on error, top = %T", a.top())
	}
	v := f.View()
	if !contains(v, "denied") || contains(v, "p@ss1") || !contains(v, "***") {
		t.Errorf("error not shown masked in form:\n%s", v)
	}
	// form is usable again after the error
	a = send(t, a, key("enter"))
	if len(fh.added) != 2 {
		t.Errorf("retry after error did not call Add: %d", len(fh.added))
	}
}

func TestAddForm_DoubleEnterSubmitsOnce(t *testing.T) {
	deps, fh := fakeDeps(nil, nil)
	a := loadRepos(t, New(deps).(*app))
	a = send(t, a, key("a"))
	f := a.top().(*addForm)
	a = send(t, a, "r", key("\t"), "http://r")
	_, cmd1 := a.Update(key("enter"))
	_, cmd2 := a.Update(key("enter"))
	if cmd1 == nil || cmd2 != nil {
		t.Fatalf("second Enter while busy must be ignored (cmd1=%v cmd2=%v)", cmd1 != nil, cmd2 != nil)
	}
	_ = f
	_ = fh
}

func TestDeleteDialog_SuccessClosesAndReloads(t *testing.T) {
	deps, fh := fakeDeps([]helm.Repo{{Name: "r1", URL: "http://r1"}, {Name: "r2", URL: "http://r2"}}, nil)
	a := loadRepos(t, New(deps).(*app), "r1", "r2")
	a = send(t, a, key("d"), key("y"))
	if len(fh.removed) != 1 {
		t.Fatalf("removed = %v", fh.removed)
	}
	if _, ok := a.top().(*reposScreen); !ok {
		t.Fatalf("dialog not closed, top = %T", a.top())
	}
	if rs := rootRepos(a); len(rs.rows) != 1 || rs.rows[0].Repo.Name != "r2" {
		t.Errorf("rows = %+v", rs.rows)
	}
}

func TestDeleteDialog_ErrorStaysWithMessage(t *testing.T) {
	deps, fh := fakeDeps([]helm.Repo{{Name: "r1", URL: "http://r1"}}, nil)
	a := loadRepos(t, New(deps).(*app), "r1")
	fh.err = errors.New("boom http://u:s3cret@h")
	a = send(t, a, key("d"), key("y"))
	if _, ok := a.top().(*confirmDialog); !ok {
		t.Fatalf("dialog closed on error, top = %T", a.top())
	}
	v := a.top().View()
	if !contains(v, "boom") || contains(v, "s3cret") {
		t.Errorf("dialog view:\n%s", v)
	}
}

// --- item 10: proxy dialog never shows the password ---

func TestProxyForm_ShowsMaskedCurrentAndStartsEmpty(t *testing.T) {
	deps, _ := fakeDeps([]helm.Repo{{Name: "r1", URL: "http://r1"}}, map[string]string{"r1": "http://u:hunter2@p:3128"})
	a := loadRepos(t, New(deps).(*app), "r1")
	a = send(t, a, key("p"))
	f, ok := a.top().(*proxyForm)
	if !ok {
		t.Fatalf("top = %T", a.top())
	}
	if f.fields[0].input.Value() != "" {
		t.Errorf("input must start empty, got %q", f.fields[0].input.Value())
	}
	v := f.View()
	if contains(v, "hunter2") {
		t.Errorf("password visible:\n%s", v)
	}
	if !contains(v, "http://u:***@p:3128") || !contains(v, "ctrl+d") {
		t.Errorf("masked current / ctrl+d hint missing:\n%s", v)
	}
}

func TestProxyForm_EmptyEnterKeepsProxyAndCloses(t *testing.T) {
	deps, _ := fakeDeps([]helm.Repo{{Name: "r1", URL: "http://r1"}}, map[string]string{"r1": "http://p:1"})
	fc := deps.Config.(*fakeConfig)
	a := loadRepos(t, New(deps).(*app), "r1")
	a = send(t, a, key("p"), key("enter"))
	if len(fc.set) != 0 {
		t.Errorf("config changed on empty Enter: %+v", fc.set)
	}
	if _, ok := a.top().(*reposScreen); !ok {
		t.Errorf("form not closed, top = %T", a.top())
	}
}

func TestProxyForm_CtrlDClears_EnterSets(t *testing.T) {
	deps, _ := fakeDeps([]helm.Repo{{Name: "r1", URL: "http://r1"}}, map[string]string{"r1": "http://p:1"})
	fc := deps.Config.(*fakeConfig)
	a := loadRepos(t, New(deps).(*app), "r1")
	a = send(t, a, key("p"), key("ctrl+d"))
	if v, ok := fc.set["r1"]; !ok || v != "" {
		t.Errorf("proxy not cleared: %+v", fc.set)
	}
	if _, ok := a.top().(*reposScreen); !ok {
		t.Errorf("form not closed, top = %T", a.top())
	}
	a = send(t, a, key("p"), "socks5://h:1080", key("enter"))
	if fc.set["r1"] != "socks5://h:1080" {
		t.Errorf("proxy not set: %+v", fc.set)
	}
}

// --- item 2: global search loads indexes in the real order of events ---

func TestSearch_LoadsIndexesFromReposWhenOpenedFromRepos(t *testing.T) {
	dir := t.TempDir()
	writeIndex(t, dir, "bitnami", "nginx")
	writeIndex(t, dir, "grafana", "loki")
	deps, _ := fakeDeps(nil, nil)
	deps.Env.RepositoryCache = dir
	a := loadRepos(t, New(deps).(*app), "bitnami", "grafana", "authentik")
	a = send(t, a, key("/"))
	ss, ok := a.top().(*searchScreen)
	if !ok {
		t.Fatalf("top = %T", a.top())
	}
	if len(ss.idxs) != 2 {
		t.Fatalf("indexes loaded = %d, want 2 (repo without index skipped)", len(ss.idxs))
	}
	a = send(t, a, "ng")
	if len(ss.hits) != 1 || ss.hits[0].Chart != "nginx" {
		t.Errorf("hits = %+v", ss.hits)
	}
}

// --- item 4: filter input ---

func TestCharts_FilterAcceptsReservedKeysAndUnicode(t *testing.T) {
	withT(t, func(a *app, s *chartsScreen) {
		a = send(t, a, chartsLoadedMsg{Repo: "testrepo", Charts: []index.Chart{mkChart("jkq", "1.0.0"), mkChart("жук", "1.0.0")}})
		a = send(t, a, key("/"), "jkq/")
		if s.filter != "jkq/" || !s.filtering {
			t.Fatalf("filter = %q filtering=%v", s.filter, s.filtering)
		}
		if _, ok := a.top().(*chartsScreen); !ok {
			t.Fatalf("q must not quit while filtering")
		}
		a = send(t, a, key("backspace"), key("backspace"), key("backspace"), key("backspace"))
		a = send(t, a, "жук")
		if s.filter != "жук" || len(s.visible()) != 1 {
			t.Errorf("filter = %q visible=%d", s.filter, len(s.visible()))
		}
		a = send(t, a, key("backspace"))
		if s.filter != "жу" {
			t.Errorf("backspace corrupted utf-8: %q", s.filter)
		}
	})
}

// --- item 7: empty index / update ---

func TestCharts_EmptyIndexShowsNoCharts(t *testing.T) {
	withT(t, func(a *app, s *chartsScreen) {
		a = send(t, a, chartsLoadedMsg{Repo: "testrepo", Charts: nil})
		if v := s.View(); contains(v, "loading") || !contains(v, "no charts") {
			t.Errorf("view:\n%s", v)
		}
	})
}

func TestCharts_UKeyUpdatesRepoWithItsProxyAndReloadsIndex(t *testing.T) {
	dir := t.TempDir()
	deps, fh := fakeDeps(nil, map[string]string{"testrepo": "http://p:1"})
	deps.Env.RepositoryCache = dir
	a := New(deps).(*app)
	s := newChartsScreen(RepoRow{Repo: helm.Repo{Name: "testrepo"}})
	a = send(t, a, pushScreenMsg{Screen: s})
	if !contains(s.View(), "press u") {
		t.Fatalf("view:\n%s", s.View())
	}
	writeIndex(t, dir, "testrepo", "nginx") // appears "after update"
	a = send(t, a, key("u"))
	if len(fh.updated) != 1 || fh.updated[0] != "testrepo" {
		t.Fatalf("updated = %v", fh.updated)
	}
	if len(s.charts) != 1 || !contains(s.View(), "nginx") {
		t.Errorf("index not reloaded:\n%s", s.View())
	}
}

func TestCharts_UpdateErrorIsShownMasked(t *testing.T) {
	deps, fh := fakeDeps(nil, nil)
	fh.updateErrs = map[string]error{"testrepo": errors.New("fail http://u:s3cret@h")}
	a := New(deps).(*app)
	s := newChartsScreen(RepoRow{Repo: helm.Repo{Name: "testrepo"}})
	a = send(t, a, pushScreenMsg{Screen: s})
	a = send(t, a, key("u"))
	v := s.View()
	if !contains(v, "fail") || contains(v, "s3cret") {
		t.Errorf("view:\n%s", v)
	}
}

// --- item 8: async messages reach the Repos screen from any screen ---

func healthCmdOf(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case m := <-done:
		return m
	case <-time.After(time.Second):
		t.Fatal("cmd blocked")
		return nil
	}
}

func TestHealth_ChainSurvivesNavigationToAnotherScreen(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	deps.Health = func(ctx context.Context, repos []helm.Repo, _ func(string) string) <-chan health.Result {
		ch := make(chan health.Result, len(repos))
		for _, r := range repos {
			ch <- health.Result{Repo: r.Name, State: health.OK, Latency: time.Millisecond}
		}
		close(ch)
		return ch
	}
	a := New(deps).(*app)
	m, cmd := a.Update(reposLoadedMsg{Rows: []RepoRow{
		{Repo: helm.Repo{Name: "r1", URL: "http://r1"}, State: health.Checking},
		{Repo: helm.Repo{Name: "r2", URL: "http://r2"}, State: health.Checking},
	}})
	a = m.(*app)
	// user leaves Repos before any result arrives
	m, _ = a.Update(pushScreenMsg{Screen: newChartsScreen(RepoRow{Repo: helm.Repo{Name: "r1"}})})
	a = m.(*app)
	for i := 0; cmd != nil && i < 10; i++ {
		msg := healthCmdOf(t, cmd)
		if msg == nil {
			break
		}
		m, cmd = a.Update(msg)
		a = m.(*app)
	}
	for _, r := range rootRepos(a).rows {
		if r.State != health.OK {
			t.Errorf("%s stuck in state %v", r.Repo.Name, r.State)
		}
	}
}

func TestReposLoaded_ReachesReposScreenFromOtherScreen(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	a := New(deps).(*app)
	m, _ := a.Update(pushScreenMsg{Screen: newChartsScreen(RepoRow{Repo: helm.Repo{Name: "r1"}})})
	a = m.(*app)
	m, _ = a.Update(reposLoadedMsg{Rows: []RepoRow{{Repo: helm.Repo{Name: "r1", URL: "http://r1"}}}})
	a = m.(*app)
	if rs := rootRepos(a); rs.loading || len(rs.rows) != 1 {
		t.Errorf("reposLoadedMsg lost: loading=%v rows=%d", rs.loading, len(rs.rows))
	}
}

func TestHealth_RecheckCancelsPreviousAndDoesNotDuplicateChain(t *testing.T) {
	var ctxs []context.Context
	deps, _ := fakeDeps(nil, nil)
	deps.Health = func(ctx context.Context, repos []helm.Repo, _ func(string) string) <-chan health.Result {
		ctxs = append(ctxs, ctx)
		ch := make(chan health.Result, len(repos))
		for _, r := range repos {
			ch <- health.Result{Repo: r.Name, State: health.OK}
		}
		close(ch)
		return ch
	}
	a := New(deps).(*app)
	m, cmd1 := a.Update(reposLoadedMsg{Rows: []RepoRow{{Repo: helm.Repo{Name: "r1", URL: "http://r1"}}}})
	a = m.(*app)
	m, cmd2 := a.Update(key("r"))
	a = m.(*app)
	if len(ctxs) != 2 {
		t.Fatalf("health started %d times", len(ctxs))
	}
	if ctxs[0].Err() == nil {
		t.Error("previous health check was not cancelled")
	}
	// stale chain: its result is dropped and does not continue
	if msg := healthCmdOf(t, cmd1); msg != nil {
		_, next := a.Update(msg)
		if next != nil {
			t.Error("stale health chain continued")
		}
	}
	// live chain still works
	msg := healthCmdOf(t, cmd2)
	m, _ = a.Update(msg)
	a = m.(*app)
	if rootRepos(a).rows[0].State != health.OK {
		t.Errorf("live chain result lost")
	}
}

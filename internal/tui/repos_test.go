package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/health"
	"github.com/vasyakrg/helm-tui/internal/helm"
)

// fakeDeps builds Deps with an in-memory helm/config backend.
func fakeDeps(repos []helm.Repo, proxies map[string]string) (Deps, *fakeHelm) {
	fh := &fakeHelm{repos: repos}
	fd := &fakeConfig{proxies: proxies}
	return Deps{
		Helm:   fh,
		Config: fd,
		Env:    helm.Env{RepositoryCache: "testdata-cache", RepositoryConfig: "/x"},
		Health: func(ctx context.Context, repos []helm.Repo, proxyFor func(string) string) <-chan health.Result {
			ch := make(chan health.Result, len(repos))
			close(ch)
			return ch
		},
	}, fh
}

type fakeHelm struct {
	repos   []helm.Repo
	added   []helm.AddArgs
	removed []string
	updated []string
	err     error
}

func (f *fakeHelm) List(context.Context) ([]helm.Repo, error) { return f.repos, f.err }
func (f *fakeHelm) Add(_ context.Context, a helm.AddArgs) error {
	f.added = append(f.added, a)
	return f.err
}
func (f *fakeHelm) Remove(_ context.Context, name string) error {
	f.removed = append(f.removed, name)
	return f.err
}
func (f *fakeHelm) Update(_ context.Context, name, _ string) error {
	f.updated = append(f.updated, name)
	return f.err
}
func (f *fakeHelm) Pull(context.Context, string, string, string, string, string) error {
	return f.err
}

type fakeConfig struct {
	proxies map[string]string
	set     map[string]string
}

func (f *fakeConfig) Load() (ConfigView, error) {
	return ConfigView{Proxies: f.proxies}, nil
}
func (f *fakeConfig) SetProxy(repo, proxyURL string) error {
	if f.set == nil {
		f.set = map[string]string{}
	}
	f.set[repo] = proxyURL
	return nil
}

func send(t *testing.T, m tea.Model, msgs ...tea.Msg) *app {
	t.Helper()
	a := m.(*app)
	for _, msg := range msgs {
		for {
			var cmd tea.Cmd
			m, cmd = a.Update(msg)
			a = m.(*app)
			if cmd == nil {
				break
			}
			next := cmd()
			if next == nil {
				break
			}
			msg = next
		}
	}
	return a
}

func key(s string) tea.Msg { return tea.KeyPressMsg{Code: rune(s[0]), Text: s} }

func TestRepos_ShowsRowsAfterLoad(t *testing.T) {
	deps, _ := fakeDeps([]helm.Repo{{Name: "bitnami", URL: "https://charts.bitnami.com"}}, nil)
	a := send(t, New(deps), key("x")) // Init not run; feed load result directly
	a = send(t, a, reposLoadedMsg{Rows: []RepoRow{
		{Repo: helm.Repo{Name: "bitnami", URL: "https://charts.bitnami.com"}, HasIndex: true, Charts: 42},
	}})
	view := a.top().View()
	if !contains(view, "bitnami") || !contains(view, "https://charts.bitnami.com") || !contains(view, "42") {
		t.Errorf("view missing row data:\n%s", view)
	}
}

func TestRepos_MissingIndexShowsDash(t *testing.T) {
	deps, _ := fakeDeps([]helm.Repo{{Name: "authentik", URL: "https://x"}}, nil)
	a := New(deps).(*app)
	a = send(t, a, reposLoadedMsg{Rows: []RepoRow{
		{Repo: helm.Repo{Name: "authentik", URL: "https://x"}, HasIndex: false},
	}})
	if !contains(a.top().View(), "—") {
		t.Errorf("dash missing for repo without index:\n%s", a.top().View())
	}
}

func TestRepos_EmptyListShowsHint(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	a := send(t, New(deps), reposLoadedMsg{Rows: nil})
	if !contains(a.top().View(), "a") {
		t.Errorf("hint about 'a' missing:\n%s", a.top().View())
	}
}

func TestRepos_StatusUpdatesFromHealthMsg(t *testing.T) {
	deps, _ := fakeDeps([]helm.Repo{{Name: "r1", URL: "http://r1"}}, nil)
	a := send(t, New(deps), reposLoadedMsg{Rows: []RepoRow{
		{Repo: helm.Repo{Name: "r1", URL: "http://r1"}, HasIndex: true, Charts: 1},
	}})
	if !contains(a.top().View(), "…") {
		t.Error("checking mark missing")
	}
	a = send(t, a, healthMsg{Result: health.Result{Repo: "r1", State: health.OK, Latency: 123 * time.Millisecond}})
	v := a.top().View()
	if !contains(v, "●") || !contains(v, "123ms") {
		t.Errorf("ok status missing:\n%s", v)
	}
	a = send(t, a, healthMsg{Result: health.Result{Repo: "r1", State: health.Fail, Reason: "timeout"}})
	if !contains(a.top().View(), "✕") || !contains(a.top().View(), "timeout") {
		t.Errorf("fail status missing:\n%s", a.top().View())
	}
}

func TestRepos_NavigationJK_And_EnterPushesCharts(t *testing.T) {
	rows := []RepoRow{
		{Repo: helm.Repo{Name: "a", URL: "http://a"}, HasIndex: true, Charts: 1},
		{Repo: helm.Repo{Name: "b", URL: "http://b"}, HasIndex: true, Charts: 2},
	}
	deps, _ := fakeDeps(nil, nil)
	a := send(t, New(deps), reposLoadedMsg{Rows: rows})
	rs := a.top().(*reposScreen)
	if rs.cursor != 0 {
		t.Fatalf("cursor starts at %d", rs.cursor)
	}
	a = send(t, a, key("j"))
	if rs.cursor != 1 {
		t.Errorf("cursor after j = %d", rs.cursor)
	}
	a = send(t, a, key("k"))
	if rs.cursor != 0 {
		t.Errorf("cursor after k = %d", rs.cursor)
	}
	a = send(t, a, key("enter"))
	if _, ok := a.top().(*chartsScreen); !ok {
		t.Errorf("enter did not push charts screen, top is %T", a.top())
	}
}

func TestRepos_RKeyRestartsHealthCheck(t *testing.T) {
	var calls int
	deps, _ := fakeDeps([]helm.Repo{{Name: "r", URL: "http://r"}}, nil)
	deps.Health = func(ctx context.Context, repos []helm.Repo, proxyFor func(string) string) <-chan health.Result {
		calls++
		ch := make(chan health.Result, len(repos))
		close(ch)
		return ch
	}
	a := send(t, New(deps), reposLoadedMsg{Rows: []RepoRow{
		{Repo: helm.Repo{Name: "r", URL: "http://r"}, HasIndex: true, Charts: 1, State: health.OK},
	}})
	before := calls
	a = send(t, a, key("r"))
	rs := a.top().(*reposScreen)
	if rs.rows[0].State != health.Checking {
		t.Errorf("state after r = %v, want Checking", rs.rows[0].State)
	}
	if calls != before+1 {
		t.Errorf("health calls = %d, want %d", calls, before+1)
	}
}

func TestRepos_URLCredentialsMasked(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	a := send(t, New(deps), reposLoadedMsg{Rows: []RepoRow{
		{Repo: helm.Repo{Name: "secure", URL: "https://user:p@ssw0rd@charts.example.com"}, HasIndex: true},
	}})
	v := a.top().View()
	if contains(v, "p@ssw0rd") {
		t.Errorf("password leaked in view:\n%s", v)
	}
}

func TestRepos_TinyWindowDoesNotPanic(t *testing.T) {
	deps, _ := fakeDeps([]helm.Repo{{Name: "r", URL: "http://r"}}, nil)
	a := New(deps).(*app)
	a = send(t, a, reposLoadedMsg{Rows: []RepoRow{
		{Repo: helm.Repo{Name: "r-with-a-very-long-name-here", URL: "https://very-long-url.example.com/path/to/charts"}, HasIndex: true, Charts: 100},
	}})
	a.Update(tea.WindowSizeMsg{Width: 10, Height: 3})
	_ = a.View() // must not panic
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

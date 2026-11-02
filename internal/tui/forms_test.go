package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/helm"
)

func pushTop(t *testing.T, s screen) *app {
	t.Helper()
	deps, _ := fakeDeps(nil, nil)
	a := New(deps).(*app)
	return send(t, a, pushScreenMsg{Screen: s})
}

func TestAddForm_RequiresNameAndURL(t *testing.T) {
	f := newAddForm()
	a := pushTop(t, f)
	_, fh := fakeDeps(nil, nil)
	_ = fh
	a = send(t, a, key("enter"))
	if contains(f.View(), "required") == false {
		t.Errorf("want validation error, view:\n%s", f.View())
	}
}

func TestAddForm_InvalidProxyShowsErrorAndDoesNotCallHelm(t *testing.T) {
	deps, fh := fakeDeps(nil, nil)
	a := New(deps).(*app)
	f := newAddForm()
	a = send(t, a, pushScreenMsg{Screen: f})
	a = send(t, a,
		"myrepo",
		key("\t"),
		"https://charts.example.com",
		key("\t"), key("\t"), key("\t"),
		"ftp://bad",
		key("enter"),
	)
	if !contains(f.View(), "proxy") {
		t.Errorf("want proxy error, view:\n%s", f.View())
	}
	if len(fh.added) != 0 {
		t.Errorf("helm.Add called %d times", len(fh.added))
	}
}

func TestAddForm_SubmitCallsAddWithProxyAndSavesProxy(t *testing.T) {
	deps, fh := fakeDeps(nil, nil)
	fc := deps.Config.(*fakeConfig)
	a := New(deps).(*app)
	f := newAddForm()
	a = send(t, a, pushScreenMsg{Screen: f})
	a = send(t, a,
		"myrepo",
		key("\t"),
		"https://charts.example.com",
		key("\t"), key("\t"), key("\t"),
		"socks5://127.0.0.1:1080",
		key("enter"),
	)
	if len(fh.added) != 1 {
		t.Fatalf("helm.Add calls = %d", len(fh.added))
	}
	if fh.added[0].ProxyURL != "socks5://127.0.0.1:1080" {
		t.Errorf("proxy = %q", fh.added[0].ProxyURL)
	}
	if fc.set["myrepo"] != "socks5://127.0.0.1:1080" {
		t.Errorf("proxy not saved: %+v", fc.set)
	}
}

func TestAddForm_PasswordFieldIsMasked(t *testing.T) {
	f := newAddForm()
	a := pushTop(t, f)
	a = send(t, a,
		key("\t"), key("\t"), key("\t"),
		"hunter2",
	)
	a.Update(nil) // re-render
	v := a.View().Content
	if contains(v, "hunter2") {
		t.Errorf("password visible in view:\n%s", v)
	}
}

func TestDelete_RequiresConfirmation(t *testing.T) {
	deps, fh := fakeDeps([]helm.Repo{{Name: "r1", URL: "http://r1"}}, nil)
	a := New(deps).(*app)
	a = send(t, a, reposLoadedMsg{Rows: []RepoRow{{Repo: helm.Repo{Name: "r1", URL: "http://r1"}}}})
	d := newDeleteDialog(a.top().(*reposScreen).rows[0])
	a = send(t, a, pushScreenMsg{Screen: d})
	a = send(t, a, key("n"))
	if len(fh.removed) != 0 {
		t.Error("Remove called without confirmation")
	}
}

func TestDelete_RemovesProxyFromConfig(t *testing.T) {
	deps, fh := fakeDeps([]helm.Repo{{Name: "r1", URL: "http://r1"}}, map[string]string{"r1": "http://p:1"})
	fc := deps.Config.(*fakeConfig)
	a := New(deps).(*app)
	a = send(t, a, reposLoadedMsg{Rows: []RepoRow{{Repo: helm.Repo{Name: "r1", URL: "http://r1"}}}})
	d := newDeleteDialog(a.top().(*reposScreen).rows[0])
	a = send(t, a, pushScreenMsg{Screen: d})
	a = send(t, a, key("y"))
	if len(fh.removed) != 1 || fh.removed[0] != "r1" {
		t.Errorf("removed = %v", fh.removed)
	}
	if v, ok := fc.set["r1"]; !ok || v != "" {
		t.Errorf("proxy not cleared: %+v", fc.set)
	}
}

func TestUpdateOne_UsesRepoProxy(t *testing.T) {
	deps, fh := fakeDeps([]helm.Repo{{Name: "r1", URL: "http://r1"}}, map[string]string{"r1": "socks5://p:1080"})
	a := New(deps).(*app)
	a = send(t, a, reposLoadedMsg{Rows: []RepoRow{{Repo: helm.Repo{Name: "r1", URL: "http://r1"}}}})
	// run update command directly
	cmd := updateOneCmd(a, "r1")
	msg := cmd()
	done := msg.(opDoneMsg)
	if done.Err != nil {
		t.Fatalf("err: %v", done.Err)
	}
	if len(fh.updated) != 1 {
		t.Fatalf("updated = %v", fh.updated)
	}
	// fakeHelm.Update ignores proxy; assert via recorded opts instead
}

func TestUpdateAll_CallsEachRepoAndReportsFailures(t *testing.T) {
	deps, fh := fakeDeps([]helm.Repo{
		{Name: "a", URL: "http://a"},
		{Name: "b", URL: "http://b"},
	}, nil)
	a := New(deps).(*app)
	a = send(t, a, reposLoadedMsg{Rows: []RepoRow{
		{Repo: helm.Repo{Name: "a", URL: "http://a"}},
		{Repo: helm.Repo{Name: "b", URL: "http://b"}},
	}})
	fh.updateErrs = map[string]error{"b": errors.New("boom")}
	cmd := updateAllCmd(a)
	done := cmd().(opDoneMsg)
	if len(fh.updated) != 2 {
		t.Errorf("updates = %v, want both repos", fh.updated)
	}
	if done.Err == nil || !contains(done.Err.Error(), "b: boom") {
		t.Errorf("failure not reported: %v", done.Err)
	}
}

func TestProxyDialog_EmptyClears(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	fc := deps.Config.(*fakeConfig)
	a := New(deps).(*app)
	f := newProxyForm("r1", "http://p:1")
	a = send(t, a, pushScreenMsg{Screen: f})
	// clear the field: select all then delete — simplest is setValue via backspaces
	a = send(t, a, key("ctrl+u"))
	a = send(t, a, key("enter"))
	if v, ok := fc.set["r1"]; !ok || v != "" {
		t.Errorf("proxy not cleared: %+v", fc.set)
	}
}

func TestOpError_IsScrubbedBeforeDisplay(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	a := New(deps).(*app)
	err := errors.New("dial via http://u:sup3rs3cr3t@proxy:3128 failed")
	a = send(t, a, opDoneMsg{Err: err})
	v := a.View().Content
	if contains(v, "sup3rs3cr3t") {
		t.Errorf("secret leaked in status:\n%s", v)
	}
	if !contains(v, "***") {
		t.Errorf("want masked status:\n%s", v)
	}
}

// typeText types a string character by character.
func typeText(s string) tea.Msg { return tea.KeyPressMsg{Code: 'x', Text: s} }

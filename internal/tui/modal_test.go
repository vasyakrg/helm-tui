package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/vasyakrg/helm-tui/internal/health"
	"github.com/vasyakrg/helm-tui/internal/helm"
)

func TestDialog_DrawnOverRepoList(t *testing.T) {
	deps, _ := fakeDeps([]helm.Repo{{Name: "r1", URL: "http://r1"}, {Name: "r2", URL: "http://r2"}}, nil)
	a := loadRepos(t, New(deps).(*app), "r1", "r2")
	a = sized(t, a, 100, 24)
	a = send(t, a, key("d"))
	v := plain(a.View())
	if !contains(v, "Delete repository r1?") || !contains(v, "╭") {
		t.Fatalf("dialog box missing:\n%s", v)
	}
	if !contains(v, "r2") || !contains(v, "NAME") {
		t.Errorf("repo list must stay visible behind the dialog:\n%s", v)
	}
	if n := strings.Count(v, "\n") + 1; n != 24 {
		t.Errorf("view must keep window height 24, got %d", n)
	}
}

func TestConfirmDialog_DefaultIsNo(t *testing.T) {
	deps, fh := fakeDeps([]helm.Repo{{Name: "r1", URL: "http://r1"}}, nil)
	a := loadRepos(t, New(deps).(*app), "r1")
	a = send(t, a, key("d"), key("enter"))
	if len(fh.removed) != 0 {
		t.Fatal("Enter on the default button must not delete")
	}
	if _, ok := a.top().(*reposScreen); !ok {
		t.Fatalf("dialog must close on No, top = %T", a.top())
	}
}

func TestConfirmDialog_ChooseYesWithArrowsAndEnter(t *testing.T) {
	deps, fh := fakeDeps([]helm.Repo{{Name: "r1", URL: "http://r1"}}, nil)
	a := loadRepos(t, New(deps).(*app), "r1")
	a = send(t, a, key("d"), key("left"), key("enter"))
	if len(fh.removed) != 1 {
		t.Fatalf("removed = %v", fh.removed)
	}
}

func TestRepos_ReloadDoesNotRestartHealthAndKeepsStates(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	runs := 0
	deps.Health = func(ctx context.Context, repos []helm.Repo, _ func(string) string) <-chan health.Result {
		runs++
		ch := make(chan health.Result)
		return ch
	}
	a := New(deps).(*app)
	rows := func(names ...string) reposLoadedMsg {
		var m reposLoadedMsg
		for _, n := range names {
			m.Rows = append(m.Rows, RepoRow{Repo: helm.Repo{Name: n, URL: "http://" + n}, State: health.Checking})
		}
		return m
	}
	a = send(t, a, rows("r1"))
	rs := rootRepos(a)
	rs.rows[0].State, rs.rows[0].Reason = health.Fail, "timeout"
	a = send(t, a, rows("r1", "r2")) // e.g. after adding r2
	if runs != 1 {
		t.Fatalf("health check runs = %d, want 1 (entry only)", runs)
	}
	rs = rootRepos(a)
	if rs.rows[0].State != health.Fail || rs.rows[0].Reason != "timeout" {
		t.Errorf("known state lost: %+v", rs.rows[0])
	}
	if rs.rows[1].State != health.Unchecked {
		t.Errorf("new repo must be unchecked, got %v", rs.rows[1].State)
	}
	a = send(t, a, key("r"))
	if runs != 2 {
		t.Errorf("r must run a check, runs = %d", runs)
	}
}

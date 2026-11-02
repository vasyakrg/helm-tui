package tui

import (
	"testing"
	"time"

	"github.com/vasyakrg/helm-tui/internal/helm"
	"github.com/vasyakrg/helm-tui/internal/index"
)

func chartScreen(t *testing.T) (*app, *chartsScreen) {
	deps, _ := fakeDeps(nil, nil)
	a := New(deps).(*app)
	row := RepoRow{Repo: helm.Repo{Name: "testrepo"}, HasIndex: true, Charts: 2}
	s := newChartsScreen(row)
	a = send(t, a, pushScreenMsg{Screen: s})
	return a, s
}

func withT(t *testing.T, fn func(a *app, s *chartsScreen)) {
	a, s := chartScreen(t)
	fn(a, s)
}

func mkChart(name string, versions ...string) index.Chart {
	c := index.Chart{Name: name}
	for _, v := range versions {
		c.Versions = append(c.Versions, index.Version{Version: v, Created: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)})
	}
	return c
}

func TestCharts_FilterBySlash(t *testing.T) {
	withT(t, func(a *app, s *chartsScreen) {
		a = send(t, a, chartsLoadedMsg{Repo: "testrepo", Charts: []index.Chart{
			mkChart("nginx", "1.0.0"),
			mkChart("redis", "2.0.0"),
		}})
		a = send(t, a, key("/"), "ng")
		if s.filter != "ng" {
			t.Fatalf("filter = %q", s.filter)
		}
		if len(s.visible()) != 1 || s.visible()[0].Name != "nginx" {
			t.Errorf("visible = %+v", s.visible())
		}
		// esc exits filter mode, second esc pops screen
		a = send(t, a, key("esc"))
		if s.filtering {
			t.Error("still filtering after esc")
		}
		if _, ok := a.top().(*chartsScreen); !ok {
			t.Error("first esc popped the screen")
		}
		a = send(t, a, key("esc"))
		if _, ok := a.top().(*chartsScreen); ok {
			t.Error("second esc did not pop the screen")
		}
	})
}

func TestCharts_MissingIndexShowsMessage(t *testing.T) {
	withT(t, func(a *app, s *chartsScreen) {
		a = send(t, a, chartsLoadedMsg{Repo: "testrepo", Err: index.ErrNoIndex})
		if !contains(s.View(), "index not found") {
			t.Errorf("view:\n%s", s.View())
		}
	})
}

func TestVersions_SpaceSelectsMaxTwo(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	a := New(deps).(*app)
	vs := newVersionsScreen("repo", mkChart("c", "1.0.0", "2.0.0", "3.0.0"))
	a = send(t, a, pushScreenMsg{Screen: vs})
	a = send(t, a, key(" "), key("j"), key(" "))
	if len(vs.marked) != 2 {
		t.Fatalf("marked = %v", vs.marked)
	}
	a = send(t, a, key("j"), key(" "))
	if len(vs.marked) != 2 {
		t.Errorf("third mark accepted: %v", vs.marked)
	}
	if !contains(vs.View(), "two versions already selected") {
		t.Errorf("hint missing:\n%s", vs.View())
	}
}

func TestVersions_DWithOneSelectedShowsHint(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	a := New(deps).(*app)
	vs := newVersionsScreen("repo", mkChart("c", "1.0.0", "2.0.0"))
	a = send(t, a, pushScreenMsg{Screen: vs})
	a = send(t, a, key(" "), key("D"))
	if !contains(vs.View(), "mark two versions") {
		t.Errorf("hint missing:\n%s", vs.View())
	}
}

func TestVersions_DWithTwoPushesDiff(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	a := New(deps).(*app)
	vs := newVersionsScreen("repo", mkChart("c", "1.0.0", "2.0.0"))
	a = send(t, a, pushScreenMsg{Screen: vs})
	a = send(t, a, key(" "), key("j"), key(" "), key("D"))
	if _, ok := a.top().(*diffScreen); !ok {
		t.Errorf("D did not push diff screen, top = %T", a.top())
	}
}

func TestSearch_TypingFiltersAcrossRepos(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	a := New(deps).(*app)
	ss := newSearchScreen()
	a = send(t, a, pushScreenMsg{Screen: ss})
	a = send(t, a, indexesLoadedMsg{Idxs: searchTestIndexes()})
	a = send(t, a, "ng")
	if len(ss.hits) != 1 || ss.hits[0].Chart != "nginx" {
		t.Errorf("hits = %+v", ss.hits)
	}
}

func TestSearch_EnterOpensVersionsOfHit(t *testing.T) {
	deps, _ := fakeDeps(nil, nil)
	a := New(deps).(*app)
	ss := newSearchScreen()
	a = send(t, a, pushScreenMsg{Screen: ss})
	a = send(t, a, indexesLoadedMsg{Idxs: searchTestIndexes()})
	a = send(t, a, "ngin", key("enter"))
	if _, ok := a.top().(*versionsScreen); !ok {
		t.Errorf("enter did not open versions, top = %T", a.top())
	}
}

func TestSearch_WorksForUnavailableRepoFromCache(t *testing.T) {
	// search reads from cached indexes only; health state is irrelevant
	deps, _ := fakeDeps(nil, nil)
	a := New(deps).(*app)
	ss := newSearchScreen()
	a = send(t, a, pushScreenMsg{Screen: ss})
	a = send(t, a, indexesLoadedMsg{Idxs: searchTestIndexes()})
	a = send(t, a, "")
	if len(ss.hits) != 2 {
		t.Errorf("hits = %d, want 2", len(ss.hits))
	}
}

func TestEsc_ReturnsToPreviousScreen(t *testing.T) {
	withT(t, func(a *app, s *chartsScreen) {
		a = send(t, a, chartsLoadedMsg{Repo: "testrepo", Charts: []index.Chart{mkChart("nginx", "1.0.0")}})
		a = send(t, a, key("enter"))
		if _, ok := a.top().(*versionsScreen); !ok {
			t.Fatalf("top = %T", a.top())
		}
		a = send(t, a, key("esc"))
		if _, ok := a.top().(*chartsScreen); !ok {
			t.Errorf("esc did not return to charts, top = %T", a.top())
		}
	})
}

func searchTestIndexes() []*index.Index {
	return []*index.Index{
		{Repo: "bitnami", Charts: []index.Chart{mkChart("nginx", "1.0.0")}},
		{Repo: "grafana", Charts: []index.Chart{mkChart("loki", "5.0.0")}},
	}
}

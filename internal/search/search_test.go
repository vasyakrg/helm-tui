package search

import (
	"testing"
	"time"

	"github.com/vasyakrg/helm-tui/internal/index"
)

func mkIdx(repo string, charts map[string][]index.Version) *index.Index {
	idx := &index.Index{Repo: repo}
	for name, vs := range charts {
		idx.Charts = append(idx.Charts, index.Chart{Name: name, Versions: vs})
	}
	return idx
}

func v(ver, desc string) index.Version {
	return index.Version{Version: ver, Description: desc, Created: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)}
}

var sample = []*index.Index{
	mkIdx("bitnami", map[string][]index.Version{
		"nginx":  {v("1.2.0", "NGINX web server"), v("1.1.0", "NGINX web server")},
		"common": {v("2.0.0", "Common library chart")},
	}),
	mkIdx("grafana", map[string][]index.Version{
		"loki": {v("5.0.0", "Log aggregation system")},
	}),
}

func TestSearch_MatchesNameCaseInsensitive(t *testing.T) {
	hits := Search(sample, "NGINX")
	if len(hits) != 1 || hits[0].Chart != "nginx" {
		t.Errorf("hits = %+v", hits)
	}
}

func TestSearch_MatchesDescription(t *testing.T) {
	hits := Search(sample, "log aggregation")
	if len(hits) != 1 || hits[0].Chart != "loki" {
		t.Errorf("hits = %+v", hits)
	}
}

func TestSearch_OneHitPerChartWithLatest(t *testing.T) {
	hits := Search(sample, "nginx")
	if hits[0].Latest != "1.2.0" {
		t.Errorf("latest = %q", hits[0].Latest)
	}
	if len(hits[0].Versions) != 2 {
		t.Errorf("versions = %d, want 2", len(hits[0].Versions))
	}
}

func TestSearch_EmptyQueryReturnsAll(t *testing.T) {
	hits := Search(sample, "")
	if len(hits) != 3 {
		t.Errorf("hits = %d, want 3", len(hits))
	}
	// sorted by Repo then Chart
	if hits[0].Repo != "bitnami" || hits[0].Chart != "common" ||
		hits[1].Chart != "nginx" || hits[2].Repo != "grafana" {
		t.Errorf("order wrong: %+v", hits)
	}
}

func TestSearch_SkipsNilIndexes(t *testing.T) {
	hits := Search([]*index.Index{sample[0], nil, sample[1]}, "")
	if len(hits) != 3 {
		t.Errorf("hits = %d, want 3", len(hits))
	}
}

func TestSearch_SameChartNameInTwoReposGivesTwoHits(t *testing.T) {
	dup := mkIdx("other", map[string][]index.Version{
		"nginx": {v("9.9.9", "different nginx")},
	})
	hits := Search([]*index.Index{sample[0], dup}, "nginx")
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].Repo == hits[1].Repo {
		t.Errorf("both hits from %s", hits[0].Repo)
	}
}

func TestSearch_SubstringInsideName(t *testing.T) {
	hits := Search(sample, "gin") // substring of "nginx"
	if len(hits) != 1 {
		t.Errorf("hits = %d, want 1", len(hits))
	}
}

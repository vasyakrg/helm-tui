// Package search performs case-insensitive substring search across repository indexes.
package search

import (
	"sort"
	"strings"

	"github.com/vasyakrg/helm-tui/internal/index"
)

// Hit is one matching chart with its latest version and full version list.
type Hit struct {
	Repo        string
	Chart       string
	Latest      string
	Description string
	Versions    []index.Version
}

// Search returns one hit per chart whose name or description contains query
// (case-insensitive). A nil index is skipped; an empty query matches all.
// Hits are sorted by Repo, then Chart.
func Search(idxs []*index.Index, query string) []Hit {
	q := strings.ToLower(query)
	var hits []Hit
	for _, idx := range idxs {
		if idx == nil {
			continue
		}
		for _, c := range idx.Charts {
			var latest index.Version
			if len(c.Versions) > 0 {
				latest = c.Versions[0]
			}
			desc := latest.Description
			if q != "" &&
				!strings.Contains(strings.ToLower(c.Name), q) &&
				!strings.Contains(strings.ToLower(desc), q) {
				continue
			}
			hits = append(hits, Hit{
				Repo:        idx.Repo,
				Chart:       c.Name,
				Latest:      latest.Version,
				Description: desc,
				Versions:    c.Versions,
			})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Repo != hits[j].Repo {
			return hits[i].Repo < hits[j].Repo
		}
		return hits[i].Chart < hits[j].Chart
	})
	return hits
}

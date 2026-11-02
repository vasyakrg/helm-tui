// Package index reads helm repository indexes from the local helm cache.
package index

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

// ErrNoIndex is returned when the repository has no cached index file.
var ErrNoIndex = errors.New("index not found")

// Version is one entry of a chart in the index.
type Version struct {
	Version     string
	AppVersion  string
	Description string
	Created     time.Time
	Deprecated  bool
}

// Chart is a chart with its versions sorted from newest to oldest.
type Chart struct {
	Name     string
	Versions []Version
}

// Index is a parsed repository index.
type Index struct {
	Repo   string
	Charts []Chart
}

type indexFile struct {
	Entries map[string][]entry `yaml:"entries"`
}

type entry struct {
	Version     string    `yaml:"version"`
	AppVersion  string    `yaml:"appVersion"`
	Description string    `yaml:"description"`
	Created     time.Time `yaml:"created"`
	Deprecated  bool      `yaml:"deprecated"`
}

// Load parses <cacheDir>/<repo>-index.yaml. A missing file yields ErrNoIndex.
func Load(cacheDir, repo string) (*Index, error) {
	path := filepath.Join(cacheDir, repo+"-index.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNoIndex, path)
		}
		return nil, err
	}
	var f indexFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	idx := &Index{Repo: repo}
	for name, entries := range f.Entries {
		chart := Chart{Name: name}
		seen := map[string]bool{}
		for _, e := range entries {
			if seen[e.Version] {
				continue
			}
			seen[e.Version] = true
			chart.Versions = append(chart.Versions, Version(e))
		}
		sort.SliceStable(chart.Versions, func(i, j int) bool {
			return Compare(chart.Versions[i].Version, chart.Versions[j].Version) > 0
		})
		idx.Charts = append(idx.Charts, chart)
	}
	sort.Slice(idx.Charts, func(i, j int) bool { return idx.Charts[i].Name < idx.Charts[j].Name })
	return idx, nil
}

// Compare orders version strings: semver first (descending when used as a
// sort comparison), non-semver strings compare equal to each other and are
// less than any semver.
func Compare(a, b string) int {
	aOK := checkSemver(a)
	bOK := checkSemver(b)
	switch {
	case aOK && bOK:
		av, _ := semver.NewVersion(strings.TrimSpace(a))
		bv, _ := semver.NewVersion(strings.TrimSpace(b))
		return av.Compare(bv)
	case aOK:
		return 1
	case bOK:
		return -1
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func checkSemver(s string) bool {
	_, err := semver.NewVersion(strings.TrimSpace(s))
	return err == nil
}

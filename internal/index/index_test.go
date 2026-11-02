package index

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func loadSample(t *testing.T) *Index {
	t.Helper()
	idx, err := Load("testdata", "sample")
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

func findChart(t *testing.T, idx *Index, name string) Chart {
	t.Helper()
	for _, c := range idx.Charts {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("chart %q not found", name)
	return Chart{}
}

func TestLoad_ParsesChartsAndDates(t *testing.T) {
	idx := loadSample(t)
	if idx.Repo != "sample" {
		t.Errorf("Repo = %q", idx.Repo)
	}
	if len(idx.Charts) != 2 {
		t.Fatalf("charts = %d, want 2", len(idx.Charts))
	}
	nginx := findChart(t, idx, "nginx")
	created := nginx.Versions[0].Created
	if created.IsZero() || created.Year() != 2023 {
		t.Errorf("created not parsed: %v", created)
	}
	if nginx.Versions[0].AppVersion != "1.25.3" {
		t.Errorf("appVersion = %q", nginx.Versions[0].AppVersion)
	}
	if nginx.Versions[0].Description == "" {
		t.Error("description not parsed")
	}
}

func TestLoad_SortsSemverDescending(t *testing.T) {
	idx := loadSample(t)
	nginx := findChart(t, idx, "nginx")
	want := []string{"1.10.0", "1.9.1", "1.2.0", "latest"}
	if len(nginx.Versions) != len(want) {
		t.Fatalf("versions = %d, want %d", len(nginx.Versions), len(want))
	}
	for i, v := range want {
		if nginx.Versions[i].Version != v {
			t.Errorf("versions[%d] = %q, want %q", i, nginx.Versions[i].Version, v)
		}
	}
}

func TestLoad_NonSemverVersionsDoNotPanicAndSortLast(t *testing.T) {
	idx := loadSample(t)
	nginx := findChart(t, idx, "nginx")
	last := nginx.Versions[len(nginx.Versions)-1]
	if last.Version != "latest" {
		t.Errorf("last version = %q, want non-semver last", last.Version)
	}
}

func TestLoad_DuplicateVersionsKeptOnce(t *testing.T) {
	idx := loadSample(t)
	nginx := findChart(t, idx, "nginx")
	n := 0
	for _, v := range nginx.Versions {
		if v.Version == "1.9.1" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("1.9.1 appears %d times, want 1", n)
	}
}

func TestLoad_MissingIndexReturnsErrNoIndex(t *testing.T) {
	_, err := Load(t.TempDir(), "nonexistent")
	if !errors.Is(err, ErrNoIndex) {
		t.Errorf("err = %v, want ErrNoIndex", err)
	}
}

func TestLoad_CorruptYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad-index.yaml"), []byte("{not: valid: yaml: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, "bad"); err == nil || errors.Is(err, ErrNoIndex) {
		t.Errorf("err = %v, want parse error", err)
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.0", "1.10.0", -1},
		{"1.10.0", "1.9.1", 1},
		{"1.0.0", "1.0.0", 0},
		{"latest", "1.0.0", -1},
		{"1.0.0", "latest", 1},
		{"latest", "latest", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

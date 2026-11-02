package diff

import (
	"os"
	"path/filepath"
	"testing"
)

func mkTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func statuses(diffs []FileDiff) map[string]Status {
	m := map[string]Status{}
	for _, d := range diffs {
		m[d.Path] = d.Status
	}
	return m
}

func TestCompare_ModifiedAddedRemoved(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	mkTree(t, oldDir, map[string]string{
		"Chart.yaml":         "a",
		"values.yaml":        "x: 1",
		"templates/old.yaml": "gone",
	})
	mkTree(t, newDir, map[string]string{
		"Chart.yaml":         "a",
		"values.yaml":        "x: 2",
		"templates/new.yaml": "added",
	})
	diffs, err := Compare(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	st := statuses(diffs)
	if st["values.yaml"] != Modified {
		t.Errorf("values.yaml = %v, want Modified", st["values.yaml"])
	}
	if st["templates/new.yaml"] != Added {
		t.Errorf("new.yaml = %v, want Added", st["templates/new.yaml"])
	}
	if st["templates/old.yaml"] != Removed {
		t.Errorf("old.yaml = %v, want Removed", st["templates/old.yaml"])
	}
	if _, ok := st["Chart.yaml"]; ok {
		t.Error("unchanged Chart.yaml included")
	}
	// sorted by path
	if diffs[0].Path > diffs[len(diffs)-1].Path {
		t.Errorf("not sorted: %v", diffs)
	}
}

func TestCompare_UnchangedOmitted(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	mkTree(t, oldDir, map[string]string{"same.txt": "same"})
	mkTree(t, newDir, map[string]string{"same.txt": "same"})
	diffs, err := Compare(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 0 {
		t.Errorf("diffs = %+v", diffs)
	}
}

func TestCompare_LineDiffMarksPlusMinus(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	mkTree(t, oldDir, map[string]string{"values.yaml": "tag: v1\nreplicas: 1\n"})
	mkTree(t, newDir, map[string]string{"values.yaml": "tag: v2\nreplicas: 1\n"})
	diffs, _ := Compare(oldDir, newDir)
	if len(diffs) != 1 {
		t.Fatalf("diffs = %d", len(diffs))
	}
	var minus, plus bool
	for _, l := range diffs[0].Lines {
		switch l.Kind {
		case '-':
			minus = l.Text == "tag: v1"
		case '+':
			plus = l.Text == "tag: v2"
		}
	}
	if !minus || !plus {
		t.Errorf("minus=%v plus=%v lines=%+v", minus, plus, diffs[0].Lines)
	}
}

func TestCompare_HunkStartsPointToChanges(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	mkTree(t, oldDir, map[string]string{"f.yaml": "a\nb\nc\nd\ne\nf\nCHANGE\n"})
	mkTree(t, newDir, map[string]string{"f.yaml": "a\nb\nc\nd\ne\nf\nCHANGED\n"})
	diffs, _ := Compare(oldDir, newDir)
	if len(diffs) != 1 {
		t.Fatalf("diffs = %d", len(diffs))
	}
	if len(diffs[0].HunkStarts) == 0 {
		t.Fatal("no hunks")
	}
	for _, s := range diffs[0].HunkStarts {
		if s < 0 || s >= len(diffs[0].Lines) {
			t.Fatalf("hunk start %d out of range", s)
		}
		l := diffs[0].Lines[s]
		if l.Kind != '-' && l.Kind != '+' {
			t.Errorf("hunk start %d on kind %q", s, l.Kind)
		}
	}
}

func TestCompare_BinaryFileMarkedBinaryNoLines(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	bin1 := "hello\x00world1"
	bin2 := "hello\x00world2"
	if err := os.WriteFile(filepath.Join(oldDir, "blob.bin"), []byte(bin1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "blob.bin"), []byte(bin2), 0o644); err != nil {
		t.Fatal(err)
	}
	diffs, _ := Compare(oldDir, newDir)
	if len(diffs) != 1 || !diffs[0].Binary || len(diffs[0].Lines) != 0 {
		t.Errorf("diffs = %+v", diffs)
	}
}

func TestCompare_TgzMarkedBinary(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(oldDir, "c.tgz"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "c.tgz"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	diffs, _ := Compare(oldDir, newDir)
	if len(diffs) != 1 || !diffs[0].Binary {
		t.Errorf("diffs = %+v", diffs)
	}
}

func TestCompare_NestedDirectories(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	mkTree(t, oldDir, map[string]string{"templates/a/b.yaml": "one"})
	mkTree(t, newDir, map[string]string{"templates/a/b.yaml": "two"})
	diffs, _ := Compare(oldDir, newDir)
	if len(diffs) != 1 || diffs[0].Path != filepath.Join("templates", "a", "b.yaml") {
		t.Errorf("diffs = %+v", diffs)
	}
}

func TestCompare_EmptyDirsBothSidesNoDiffs(t *testing.T) {
	diffs, err := Compare(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 0 {
		t.Errorf("diffs = %+v", diffs)
	}
}

func TestCompare_FileVsDirectory(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	mkTree(t, oldDir, map[string]string{"x": "file"})
	mkTree(t, newDir, map[string]string{"x/inner": "dir"})
	diffs, _ := Compare(oldDir, newDir)
	st := statuses(diffs)
	if st["x"] != Modified && !(st[filepath.Join("x", "inner")] == Added && st["x"] == Removed) {
		t.Errorf("file->dir transition mishandled: %+v", diffs)
	}
}

func TestCompare_DirOnlyOnOneSideReportsChildFilesNotDir(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	mkTree(t, oldDir, map[string]string{
		"Chart.yaml":         "a",
		"crds/old/a.yaml":    "1",
		"crds/old/sub/b.yml": "2",
	})
	mkTree(t, newDir, map[string]string{
		"Chart.yaml":              "a",
		"templates/extra/c.yaml":  "3",
		"templates/extra/d/e.yml": "4",
	})
	diffs, err := Compare(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	st := statuses(diffs)
	want := map[string]Status{
		"crds/old/a.yaml":         Removed,
		"crds/old/sub/b.yml":      Removed,
		"templates/extra/c.yaml":  Added,
		"templates/extra/d/e.yml": Added,
	}
	for p, s := range want {
		if got, ok := st[p]; !ok || got != s {
			t.Errorf("%s: status=%v present=%v, want %v", p, got, ok, s)
		}
	}
	if len(diffs) != len(want) {
		t.Errorf("diffs = %v (directories must not be reported)", st)
	}
}

func TestCompare_AddedAndRemovedFilesCarryContent(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	mkTree(t, oldDir, map[string]string{"gone.yaml": "a\nb\n", "bin.dat": "x\x00y"})
	mkTree(t, newDir, map[string]string{"new.yaml": "c\nd\ne\n", "newbin.dat": "x\x00y", "empty.yaml": ""})
	diffs, err := Compare(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]FileDiff{}
	for _, d := range diffs {
		by[d.Path] = d
	}
	n := by["new.yaml"]
	if n.Status != Added || len(n.Lines) != 3 || n.Lines[0] != (Line{Kind: '+', Text: "c"}) || len(n.HunkStarts) != 1 || n.HunkStarts[0] != 0 {
		t.Errorf("added: %+v", n)
	}
	g := by["gone.yaml"]
	if g.Status != Removed || len(g.Lines) != 2 || g.Lines[1] != (Line{Kind: '-', Text: "b"}) {
		t.Errorf("removed: %+v", g)
	}
	for _, p := range []string{"bin.dat", "newbin.dat"} {
		if d := by[p]; !d.Binary || len(d.Lines) != 0 {
			t.Errorf("%s: %+v", p, d)
		}
	}
	if d := by["empty.yaml"]; d.Status != Added || len(d.Lines) != 0 {
		t.Errorf("empty: %+v", d)
	}
}

package diff

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Status of a file in the comparison.
type Status int

// File statuses.
const (
	Modified Status = iota
	Added
	Removed
)

// Mark returns a single-character status mark for the UI.
func (s Status) Mark() string {
	switch s {
	case Added:
		return "+"
	case Removed:
		return "-"
	default:
		return "~"
	}
}

// Line is one line of a per-line diff. Kind is ' ' (context), '+' or '-'.
type Line struct {
	Kind byte
	Text string
}

// FileDiff describes a changed file. For Binary files Lines is empty.
// HunkStarts holds indexes into Lines where change hunks begin.
type FileDiff struct {
	Path       string
	Status     Status
	Binary     bool
	Lines      []Line
	HunkStarts []int
}

const hunkContext = 3

// Compare recursively compares two directory trees. Unchanged files are not
// included; results are sorted by path.
func Compare(oldDir, newDir string) ([]FileDiff, error) {
	var diffs []FileDiff
	err := walkTrees(oldDir, newDir, "", func(rel string, oldPath, newPath string) error {
		switch {
		case oldPath == "":
			diffs = append(diffs, FileDiff{Path: rel, Status: Added})
		case newPath == "":
			diffs = append(diffs, FileDiff{Path: rel, Status: Removed})
		default:
			d, err := compareFiles(rel, oldPath, newPath)
			if err != nil {
				return err
			}
			if d != nil {
				diffs = append(diffs, *d)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Path < diffs[j].Path })
	return diffs, nil
}

// compareFiles returns nil when the files are identical.
func compareFiles(rel, oldPath, newPath string) (*FileDiff, error) {
	oldData, err := os.ReadFile(oldPath)
	if err != nil {
		return nil, err
	}
	newData, err := os.ReadFile(newPath)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(oldData, newData) {
		return nil, nil
	}
	binary := isBinary(oldData) || isBinary(newData) || isTgz(rel)
	d := &FileDiff{Path: rel, Status: Modified, Binary: binary}
	if !binary {
		d.Lines, d.HunkStarts = lineDiff(oldData, newData, hunkContext)
	}
	return d, nil
}

func isTgz(path string) bool {
	return strings.HasSuffix(path, ".tgz") || strings.HasSuffix(path, ".tar.gz")
}

func isBinary(data []byte) bool {
	limit := 8000
	if len(data) < limit {
		limit = len(data)
	}
	return bytes.IndexByte(data[:limit], 0) >= 0
}

type walkFn func(rel, oldPath, newPath string) error

// walkTrees walks both trees and calls fn for every relative path present in
// either tree, with "" for the side that lacks it.
func walkTrees(oldDir, newDir, rel string, fn walkFn) error {
	oldPath := filepath.Join(oldDir, rel)
	newPath := filepath.Join(newDir, rel)
	oldIsDir, newIsDir := isDir(oldPath), isDir(newPath)
	switch {
	case oldIsDir && newIsDir:
		return walkDirs(oldDir, newDir, rel, fn)
	case oldIsDir:
		// dir removed (or replaced by a file): the directory itself is never
		// reported, only a file that now exists at this path plus the children
		if exists(newPath) {
			if err := fn(rel, "", newPath); err != nil {
				return err
			}
		}
		return walkOne(oldDir, rel, true, fn)
	case newIsDir:
		// dir added (or a file replaced by a dir)
		if exists(oldPath) {
			if err := fn(rel, oldPath, ""); err != nil {
				return err
			}
		}
		return walkOne(newDir, rel, false, fn)
	default:
		oldExists, newExists := exists(oldPath), exists(newPath)
		switch {
		case oldExists && newExists:
			return fn(rel, oldPath, newPath)
		case newExists:
			return fn(rel, "", newPath)
		case oldExists:
			return fn(rel, oldPath, "")
		}
		return nil
	}
}

// walkOne walks a single side of the tree at rel, reporting every entry as
// Added (when old==true reports them as belonging to the old side → Removed).
func walkOne(root, rel string, oldSide bool, fn walkFn) error {
	dir := filepath.Join(root, rel)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		child := filepath.Join(rel, e.Name())
		if e.IsDir() {
			if err := walkOne(root, child, oldSide, fn); err != nil {
				return err
			}
			continue
		}
		path := filepath.Join(root, child)
		if oldSide {
			if err := fn(child, path, ""); err != nil {
				return err
			}
		} else {
			if err := fn(child, "", path); err != nil {
				return err
			}
		}
	}
	return nil
}

func walkDirs(oldDir, newDir, rel string, fn walkFn) error {
	names := map[string]bool{}
	for _, dir := range []string{oldDir, newDir} {
		if rel != "" {
			dir = filepath.Join(dir, rel)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, e := range entries {
			names[e.Name()] = true
		}
	}
	childRels := make([]string, 0, len(names))
	for n := range names {
		childRels = append(childRels, n)
	}
	sort.Strings(childRels)
	for _, n := range childRels {
		child := n
		if rel != "" {
			child = filepath.Join(rel, n)
		}
		if err := walkTrees(oldDir, newDir, child, fn); err != nil {
			return err
		}
	}
	return nil
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// lineDiff computes a per-line diff with hunk starts, showing up to
// context lines of unchanged text around each change.
func lineDiff(oldData, newData []byte, context int) ([]Line, []int) {
	oldLines := splitLines(oldData)
	newLines := splitLines(newData)

	// Full diff first (all changes, no context), then window it.
	full := diffLines(oldLines, newLines)

	// Mark changed positions.
	changed := make([]bool, len(full))
	any := false
	for i, l := range full {
		if l.Kind != ' ' {
			changed[i] = true
			any = true
		}
	}
	if !any {
		return nil, nil
	}

	// Build include mask with context window.
	include := make([]bool, len(full))
	for i, c := range changed {
		if !c {
			continue
		}
		for j := i - context; j <= i+context; j++ {
			if j >= 0 && j < len(full) {
				include[j] = true
			}
		}
	}

	var lines []Line
	var hunks []int
	prevIncluded := false
	for i, l := range full {
		if !include[i] {
			prevIncluded = false
			continue
		}
		if changed[i] && !prevIncluded {
			hunks = append(hunks, len(lines))
		}
		// hunk start on a '-' line when next line is '+'? keep simple: start at
		// first changed line; but if first included line is context and a
		// change follows, the change itself registers a new start only when
		// separated by context from a previous hunk.
		lines = append(lines, l)
		if changed[i] {
			prevIncluded = true
		}
	}
	return lines, hunks
}

// diffLines computes a full line diff using LCS (Hirschberg-free classic DP).
// Kind ' ' for common lines, '-' removed, '+' added.
func diffLines(a, b []string) []Line {
	n, m := len(a), len(b)
	// LCS table (guard against huge inputs: fall back to naive if too big)
	if n*m > 25_000_000 {
		return naiveDiff(a, b)
	}
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	lines := make([]Line, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			lines = append(lines, Line{Kind: ' ', Text: a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			lines = append(lines, Line{Kind: '-', Text: a[i]})
			i++
		default:
			lines = append(lines, Line{Kind: '+', Text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		lines = append(lines, Line{Kind: '-', Text: a[i]})
	}
	for ; j < m; j++ {
		lines = append(lines, Line{Kind: '+', Text: b[j]})
	}
	return lines
}

// naiveDiff is used for very large files: mark everything as replaced.
func naiveDiff(a, b []string) []Line {
	lines := make([]Line, 0, len(a)+len(b))
	for _, l := range a {
		lines = append(lines, Line{Kind: '-', Text: l})
	}
	for _, l := range b {
		lines = append(lines, Line{Kind: '+', Text: l})
	}
	return lines
}

func splitLines(data []byte) []string {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

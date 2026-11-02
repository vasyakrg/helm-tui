// Package diff pulls two chart versions into a temp workspace and compares them.
package diff

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/vasyakrg/helm-tui/internal/index"
)

// Workspace is a temporary directory holding pulled chart versions.
type Workspace struct {
	Dir string
}

// NewWorkspace creates <root>/helm-tui-<rand> (root=="" means /tmp) and writes
// the current pid into it for stale cleanup.
func NewWorkspace(root string) (*Workspace, error) {
	if root == "" {
		root = "/tmp"
	}
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "helm-tui-"+hex.EncodeToString(rnd[:]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "pid"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return &Workspace{Dir: dir}, nil
}

// Close removes the workspace directory. It is idempotent.
func (w *Workspace) Close() error {
	if w == nil || w.Dir == "" {
		return nil
	}
	err := os.RemoveAll(w.Dir)
	w.Dir = ""
	return err
}

// CleanStale removes helm-tui-* directories under root whose pid is dead.
// It returns the number of removed directories.
func CleanStale(root string) (int, error) {
	if root == "" {
		root = "/tmp"
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "helm-tui-") || !e.IsDir() {
			continue
		}
		path := filepath.Join(root, e.Name())
		data, err := os.ReadFile(filepath.Join(path, "pid"))
		if err != nil {
			continue // no pid file: not ours to judge
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			continue
		}
		if processAlive(pid) {
			continue
		}
		if err := os.RemoveAll(path); err == nil {
			removed++
		}
	}
	return removed, nil
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// Puller is satisfied by *helm.Client.
type Puller interface {
	Pull(ctx context.Context, repo, chart, version, destDir, proxyURL string) error
}

// Pair holds the two untarred chart versions, ordered old → new.
type Pair struct {
	OldVersion string
	NewVersion string
	OldDir     string
	NewDir     string
}

// PullPair pulls both versions into the workspace (each into its own
// version-named subdir) and returns them ordered by semver: Old is the
// smaller version regardless of the v1/v2 argument order.
func PullPair(ctx context.Context, p Puller, w *Workspace, repo, chart, v1, v2, proxyURL string) (Pair, error) {
	oldV, newV := v1, v2
	if index.Compare(oldV, newV) > 0 {
		oldV, newV = newV, oldV
	}
	if err := ctx.Err(); err != nil {
		return Pair{}, err
	}
	pair := Pair{OldVersion: oldV, NewVersion: newV}
	for _, v := range []string{oldV, newV} {
		dest := filepath.Join(w.Dir, v)
		if err := os.MkdirAll(dest, 0o700); err != nil {
			return Pair{}, err
		}
		if err := p.Pull(ctx, repo, chart, v, dest, proxyURL); err != nil {
			return Pair{}, fmt.Errorf("pull %s/%s@%s: %w", repo, chart, v, err)
		}
		sub := filepath.Join(dest, chart)
		if _, err := os.Stat(sub); err != nil {
			return Pair{}, fmt.Errorf("chart dir missing after pull: %w", err)
		}
		if v == oldV {
			pair.OldDir = sub
		} else {
			pair.NewDir = sub
		}
	}
	return pair, nil
}

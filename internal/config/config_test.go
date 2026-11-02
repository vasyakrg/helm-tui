package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_MissingFileIsEmpty(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "cfg.yaml")}
	cfg, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Proxies) != 0 {
		t.Errorf("want empty config, got %+v", cfg)
	}
}

func TestSetProxy_RoundTrip(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "cfg.yaml")}
	if err := s.SetProxy("repo1", "http://h:3128"); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Proxy("repo1") != "http://h:3128" {
		t.Errorf("Proxy = %q", cfg.Proxy("repo1"))
	}
	if cfg.Proxy("other") != "" {
		t.Errorf("unexpected proxy for other repo")
	}
}

func TestSetProxy_FileMode0600AndDir0700(t *testing.T) {
	dir := t.TempDir()
	s := &Store{Path: filepath.Join(dir, "helm-tui", "cfg.yaml")}
	if err := s.SetProxy("r", "http://h:1"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Dir(s.Path))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v, want 0700", di.Mode().Perm())
	}
}

func TestSetProxy_ExistingFileModeFixed(t *testing.T) {
	dir := t.TempDir()
	s := &Store{Path: filepath.Join(dir, "cfg.yaml")}
	os.WriteFile(s.Path, []byte("proxies: {}"), 0o644)
	if err := s.SetProxy("r", "http://h:1"); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(s.Path)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestSetProxy_EmptyRemoves(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "cfg.yaml")}
	_ = s.SetProxy("r", "http://h:1")
	if err := s.SetProxy("r", ""); err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.Load()
	if cfg.Proxy("r") != "" {
		t.Errorf("proxy not removed: %+v", cfg)
	}
}

func TestSetProxy_RejectsInvalidURL(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "cfg.yaml")}
	if err := s.SetProxy("r", "ftp://h"); err == nil {
		t.Error("want error for invalid proxy")
	}
	if err := s.SetProxy("r", "not a url"); err == nil {
		t.Error("want error for garbage proxy")
	}
}

func TestSetProxy_AtomicWrite(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "cfg.yaml")}
	if err := s.SetProxy("r", "http://h:1"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(s.Path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

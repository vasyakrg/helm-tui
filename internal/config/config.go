// Package config persists per-repository proxy settings for helm-tui.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/vasyakrg/helm-tui/internal/proxy"
)

// Config maps repository names to proxy URLs.
type Config struct {
	Proxies map[string]string `yaml:"proxies"`
}

// Proxy returns the proxy URL configured for repo, or "".
func (c Config) Proxy(repo string) string {
	if c.Proxies == nil {
		return ""
	}
	return c.Proxies[repo]
}

// Store reads and writes the config file at Path.
type Store struct {
	Path string
}

// DefaultPath returns $XDG_CONFIG_HOME (or ~/.config) + helm-tui/config.yaml.
func DefaultPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "helm-tui", "config.yaml"), nil
}

// Load reads the config; a missing file yields an empty Config and nil error.
func (s *Store) Load() (Config, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing %s: %w", s.Path, err)
	}
	return cfg, nil
}

// SetProxy sets (or removes, when proxyURL is empty) the proxy for a repo and
// writes the config atomically with restrictive permissions.
func (s *Store) SetProxy(repo, proxyURL string) error {
	if proxyURL != "" {
		if err := proxy.Validate(proxyURL); err != nil {
			return err
		}
	}
	cfg, err := s.Load()
	if err != nil {
		return err
	}
	if cfg.Proxies == nil {
		cfg.Proxies = map[string]string{}
	}
	if proxyURL == "" {
		delete(cfg.Proxies, repo)
	} else {
		cfg.Proxies[repo] = proxyURL
	}
	return s.write(cfg)
}

func (s *Store) write(cfg Config) error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.Path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

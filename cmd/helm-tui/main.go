// Command helm-tui is a terminal UI for browsing and managing helm repositories.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/config"
	"github.com/vasyakrg/helm-tui/internal/diff"
	"github.com/vasyakrg/helm-tui/internal/health"
	"github.com/vasyakrg/helm-tui/internal/helm"
	"github.com/vasyakrg/helm-tui/internal/tui"
)

var version = "dev"

// configAdapter adapts *config.Store to tui.ConfigAPI.
type configAdapter struct{ s *config.Store }

func (a configAdapter) Load() (tui.ConfigView, error) {
	cfg, err := a.s.Load()
	if err != nil {
		return tui.ConfigView{}, err
	}
	return tui.ConfigView{Proxies: cfg.Proxies}, nil
}

func (a configAdapter) SetProxy(repo, proxyURL string) error {
	return a.s.SetProxy(repo, proxyURL)
}

func run() error {
	client := &helm.Client{R: &helm.ExecRunner{}}
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return err
	}
	store := &config.Store{Path: cfgPath}
	env, err := client.Env(context.Background())
	if err != nil {
		return fmt.Errorf("helm env: %w", err)
	}
	if _, err := diff.CleanStale(""); err != nil {
		// best effort; non-fatal
		_ = err
	}

	deps := tui.Deps{
		Helm:   client,
		Config: configAdapter{store},
		Env:    env,
		Health: func(ctx context.Context, repos []helm.Repo, proxyFor func(string) string) <-chan health.Result {
			return health.CheckAll(ctx, repos, proxyFor, 5*time.Second)
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	p := tea.NewProgram(tui.New(deps))
	go func() {
		<-ctx.Done()
		p.Quit()
	}()
	_, err = p.Run()
	return err
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version") {
		fmt.Printf("helm-tui %s\n", version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "helm-tui:", err)
		os.Exit(1)
	}
}

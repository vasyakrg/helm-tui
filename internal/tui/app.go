// Package tui implements the helm-tui terminal interface.
package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/health"
	"github.com/vasyakrg/helm-tui/internal/helm"
)

// HelmAPI is the subset of *helm.Client the TUI needs.
type HelmAPI interface {
	List(ctx context.Context) ([]helm.Repo, error)
	Add(ctx context.Context, a helm.AddArgs) error
	Remove(ctx context.Context, name string) error
	Update(ctx context.Context, name, proxyURL string) error
	Pull(ctx context.Context, repo, chart, version, destDir, proxyURL string) error
}

// ConfigAPI is the subset of *config.Store the TUI needs.
type ConfigAPI interface {
	Load() (ConfigView, error)
	SetProxy(repo, proxyURL string) error
}

// ConfigView is the read model of the config file (kept local so the TUI does
// not depend on the concrete config type in tests).
type ConfigView struct {
	Proxies map[string]string
}

func (c ConfigView) Proxy(repo string) string {
	if c.Proxies == nil {
		return ""
	}
	return c.Proxies[repo]
}

// DiffAPI is the subset of the diff package the TUI needs.
type DiffAPI interface {
	CleanStale(root string) (int, error)
}

// Deps wires the TUI to its backends.
type Deps struct {
	Helm   HelmAPI
	Config ConfigAPI
	Env    helm.Env
	Health func(ctx context.Context, repos []helm.Repo, proxyFor func(repo string) string) <-chan health.Result
}

// reposLoadedMsg carries fully-built repo rows (list + index counts).
type reposLoadedMsg struct {
	Rows []RepoRow
}

// healthMsg delivers one availability check result.
type healthMsg struct {
	Result health.Result
}

// opDoneMsg reports completion of a mutating operation (add/remove/update).
type opDoneMsg struct {
	Desc string
	Err  error
	// Refresh indicates the repo list should be reloaded.
	Refresh bool
}

// statusMsg sets a transient status line.
type statusMsg struct {
	Text string
}

// pushScreenMsg pushes a new screen onto the stack.
type pushScreenMsg struct {
	Screen screen
}

// popScreenMsg pops the top screen.
type popScreenMsg struct{}

type quitMsg struct{}

// screen is one TUI screen in the stack.
type screen interface {
	Init(*app) tea.Cmd
	Update(tea.Msg, *app) (screen, tea.Cmd)
	View() string
}

// app is the root model: a stack of screens plus shared state.
type app struct {
	deps  Deps
	stack []screen

	width, height int
	status        string
	statusSetAt   time.Time
}

// New builds the root model with the Repos screen on top.
func New(d Deps) tea.Model {
	return &app{deps: d, stack: []screen{newReposScreen()}}
}

func (a *app) top() screen   { return a.stack[len(a.stack)-1] }
func (a *app) push(s screen) { a.stack = append(a.stack, s) }
func (a *app) replaceTop(s screen) {
	a.stack[len(a.stack)-1] = s
}

// pop removes the top screen; returns true when it popped.
func (a *app) pop() bool {
	if len(a.stack) > 1 {
		a.stack = a.stack[:len(a.stack)-1]
		return true
	}
	return false
}

// proxyFor returns the configured proxy URL for a repo.
func (a *app) proxyFor(repo string) string {
	if a.deps.Config == nil {
		return ""
	}
	cfg, err := a.deps.Config.Load()
	if err != nil {
		return ""
	}
	return cfg.Proxy(repo)
}

func (a *app) Init() tea.Cmd {
	return a.top().Init(a)
}

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		return a, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "esc":
			if c, ok := a.top().(escConsumer); ok && c.consumeEsc(a) {
				return a, nil
			}
			if a.pop() {
				return a, nil
			}
			return a, tea.Quit
		}

	case pushScreenMsg:
		a.push(msg.Screen)
		return a, msg.Screen.Init(a)

	case popScreenMsg:
		a.pop()
		return a, nil

	case quitMsg:
		return a, tea.Quit

	case statusMsg:
		a.status = msg.Text
		a.statusSetAt = time.Now()
		return a, nil

	case opDoneMsg:
		if msg.Err != nil {
			a.status = "error: " + maskURLCredentials(msg.Err.Error())
		} else if msg.Desc != "" {
			a.status = msg.Desc
		}
		if msg.Refresh {
			return a, a.top().Init(a)
		}
		return a, nil
	}

	s, cmd := a.top().Update(msg, a)
	a.replaceTop(s)
	return a, cmd
}

func (a *app) View() tea.View {
	content := a.top().View()
	if a.status != "" {
		content = errStyle.Render(a.status) + "\n" + content
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// renderChrome wraps a screen body with title, status and hint lines.
func (a *app) renderChrome(title, body, hints string) string {
	out := titleStyle.Render(title) + "\n"
	if a.status != "" {
		out += errStyle.Render(a.status) + "\n"
	}
	out += body
	if hints != "" {
		out += "\n" + hintStyle.Render(truncate(hints, maxInt(a.width, 20)))
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Package tui implements the helm-tui terminal interface.
package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/health"
	"github.com/vasyakrg/helm-tui/internal/helm"
	"github.com/vasyakrg/helm-tui/internal/proxy"
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
	Diff   func() (DiffSession, error)
}

// reposLoadedMsg carries fully-built repo rows (list + index counts).
type reposLoadedMsg struct {
	Rows []RepoRow
}

// reposLoadFailedMsg reports that the repo list could not be loaded.
type reposLoadFailedMsg struct {
	Err error
}

// healthMsg delivers one availability check result. Gen identifies the check
// run it belongs to; results of a superseded run are dropped.
type healthMsg struct {
	Gen    int
	Result health.Result
}

// opDoneMsg reports completion of a mutating operation (add/remove/update).
type opDoneMsg struct {
	Desc string
	Err  error
	// Refresh indicates the repo list should be reloaded.
	Refresh bool
	// RefreshIndexes re-reads the cached indexes (chart counts) without
	// reloading the repo list or restarting the availability check.
	RefreshIndexes bool
	// Source is the form/dialog that started the operation (optional). It is
	// closed on success and shows the (masked) error on failure.
	Source screen
}

// opErrorReceiver is implemented by forms/dialogs that show operation errors.
type opErrorReceiver interface {
	setOpError(text string)
}

// sizeAware screens are told the window size.
type sizeAware interface {
	setSize(width, height int)
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

func (a *app) top() screen { return a.stack[len(a.stack)-1] }
func (a *app) push(s screen) {
	a.stack = append(a.stack, s)
	if sa, ok := s.(sizeAware); ok && a.width > 0 {
		sa.setSize(a.width, a.bodyHeight())
	}
}

// repoNames returns the names of the repos currently shown on the Repos screen.
func (a *app) repoNames() []string {
	names := []string{}
	for _, s := range a.stack {
		if rs, ok := s.(*reposScreen); ok {
			for _, r := range rs.rows {
				names = append(names, r.Repo.Name)
			}
			break
		}
	}
	return names
}

// removeScreen removes a specific (non-root) screen from the stack wherever
// it is, notifying it via onClose.
func (a *app) removeScreen(target screen) {
	for i := len(a.stack) - 1; i > 0; i-- {
		if a.stack[i] == target {
			if c, ok := target.(closer); ok {
				c.onClose()
			}
			a.stack = append(a.stack[:i], a.stack[i+1:]...)
			return
		}
	}
}

// routeToRoot delivers an async message to the Repos screen (stack bottom)
// regardless of which screen is on top.
func (a *app) routeToRoot(msg tea.Msg) tea.Cmd {
	s, cmd := a.stack[0].Update(msg, a)
	a.stack[0] = s
	return cmd
}
func (a *app) replaceTop(s screen) {
	a.stack[len(a.stack)-1] = s
}

// pop removes the top screen; returns true when it popped.
func (a *app) pop() bool {
	if len(a.stack) > 1 {
		if c, ok := a.top().(closer); ok {
			c.onClose()
		}
		a.stack = a.stack[:len(a.stack)-1]
		return true
	}
	if c, ok := a.top().(closer); ok {
		c.onClose()
	}
	return false
}

// closeAll notifies every screen that the app is shutting down.
func (a *app) closeAll() {
	for _, s := range a.stack {
		if c, ok := s.(closer); ok {
			c.onClose()
		}
	}
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
		for _, sc := range a.stack {
			if sa, ok := sc.(sizeAware); ok {
				sa.setSize(a.width, a.bodyHeight())
			}
		}
		return a, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			a.closeAll()
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
		a.closeAll()
		return a, tea.Quit

	case statusMsg:
		a.status = msg.Text
		a.statusSetAt = time.Now()
		return a, nil

	case reposLoadedMsg:
		return a, a.routeToRoot(msg)

	case healthMsg:
		return a, a.routeToRoot(msg)

	case indexCountsMsg:
		return a, a.routeToRoot(msg)

	case reposLoadFailedMsg:
		a.status = "error: " + proxy.MaskText(msg.Err.Error())
		return a, a.routeToRoot(msg)

	case opDoneMsg:
		if msg.Err != nil {
			text := proxy.MaskText(msg.Err.Error())
			if r, ok := msg.Source.(opErrorReceiver); ok && a.onStack(msg.Source) {
				r.setOpError(text)
			} else {
				a.status = "error: " + text
			}
		} else {
			if msg.Desc != "" {
				a.status = msg.Desc
			}
			if msg.Source != nil {
				a.removeScreen(msg.Source)
			}
		}
		if msg.Refresh {
			return a, loadReposCmd(a)
		}
		if msg.RefreshIndexes {
			return a, loadIndexCountsCmd(a)
		}
		return a, nil
	}

	s, cmd := a.top().Update(msg, a)
	a.replaceTop(s)
	return a, cmd
}

// bodyHeight is the terminal height available to a screen body.
func (a *app) bodyHeight() int {
	if a.height <= 0 {
		return 0
	}
	return maxInt(a.height-chromeRows, 1)
}

func (a *app) onStack(target screen) bool {
	for _, s := range a.stack {
		if s == target {
			return true
		}
	}
	return false
}

func (a *app) View() tea.View {
	w := a.width
	if w <= 0 {
		w = 80
	}
	var content string
	if _, ok := a.top().(modal); ok {
		content = overlayCenter(a.frame(a.baseScreen(), w), renderDialog(a.top(), a.width), a.width, a.height)
	} else {
		content = a.frame(a.top(), w)
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// baseScreen is the nearest non-modal screen below the top one; dialogs are
// drawn over it.
func (a *app) baseScreen() screen {
	for i := len(a.stack) - 1; i >= 0; i-- {
		if _, ok := a.stack[i].(modal); !ok {
			return a.stack[i]
		}
	}
	return a.stack[0]
}

// frame renders a screen with the status line on top and the hint line at the
// bottom.
func (a *app) frame(s screen, w int) string {
	content := strings.TrimRight(s.View(), "\n")
	used := strings.Count(content, "\n") + 1
	if a.status != "" {
		content = errStyle.Render(truncate(a.status, w)) + "\n" + content
		used++
	}
	if h, ok := s.(hinter); ok {
		if a.height > 0 && used < a.height-1 {
			content += strings.Repeat("\n", a.height-1-used)
		}
		content += "\n" + hintStyle.Render(truncate(h.hints(), w))
	}
	return content
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

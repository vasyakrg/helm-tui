package tui

import (
	"context"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/vasyakrg/helm-tui/internal/helm"
	"github.com/vasyakrg/helm-tui/internal/proxy"
)

// field is a labeled text input.
type field struct {
	label    string
	input    textinput.Model
	password bool
}

func newField(label, placeholder string, password bool) field {
	ti := textinput.New()
	ti.Placeholder = placeholder
	if password {
		ti.EchoMode = textinput.EchoPassword
	}
	return field{label: label, input: ti}
}

// form is a stack of fields with one focused input.
type form struct {
	fields []field
	focus  int
	err    string
	busy   bool // an operation is in flight; Enter is ignored
}

// setOpError shows a (already masked) operation error and re-enables submit.
func (f *form) setOpError(text string) {
	f.err = text
	f.busy = false
}

func (f *form) refocus() {
	for i := range f.fields {
		if i == f.focus {
			f.fields[i].input.Focus()
		} else {
			f.fields[i].input.Blur()
		}
	}
}

func (f *form) next() {
	f.focus = (f.focus + 1) % len(f.fields)
	f.refocus()
}

func (f *form) prev() {
	f.focus = (f.focus - 1 + len(f.fields)) % len(f.fields)
	f.refocus()
}

func (f *form) value(i int) string { return strings.TrimSpace(f.fields[i].input.Value()) }

// handleFormKeys processes shared navigation keys. Returns true when consumed.
func (f *form) handleFormKeys(msg tea.Msg) bool {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false
	}
	switch key.String() {
	case "tab", "down":
		f.next()
		return true
	case "shift+tab", "up":
		f.prev()
		return true
	case "ctrl+u", "\x15":
		f.fields[f.focus].input.SetValue("")
		return true
	}
	return false
}

// updateFields forwards the message to the focused input.
func (f *form) updateFields(msg tea.Msg, a *app) tea.Cmd {
	var cmds []tea.Cmd
	for i := range f.fields {
		var cmd tea.Cmd
		f.fields[i].input, cmd = f.fields[i].input.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (f *form) viewFields() string {
	var b strings.Builder
	for i, fld := range f.fields {
		cursor := "  "
		st := normalStyle
		if i == f.focus {
			cursor = "> "
			st = selectedStyle
		}
		b.WriteString(cursor + st.Render(pad(fld.label, 10)) + " " + fld.input.View() + "\n")
	}
	if f.err != "" {
		b.WriteString(errStyle.Render(f.err) + "\n")
	}
	return b.String()
}

// addForm collects name, URL, credentials and proxy for a new repository.
type addForm struct {
	form
}

func newAddForm() *addForm {
	f := &addForm{}
	f.fields = []field{
		newField("name", "repo name", false),
		newField("url", "https://charts.example.com", false),
		newField("username", "optional", false),
		newField("password", "optional", true),
		newField("proxy", "http:// or socks5:// (optional)", false),
	}
	f.refocus()
	return f
}

func (f *addForm) Init(*app) tea.Cmd { return nil }

func (f *addForm) Update(msg tea.Msg, a *app) (screen, tea.Cmd) {
	if f.handleFormKeys(msg) {
		return f, nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "enter":
			if f.busy {
				return f, nil
			}
			return f, f.submit(a)
		}
		return f, f.updateFields(msg, a)
	}
	return f, f.updateFields(msg, a)
}

func (f *addForm) submit(a *app) tea.Cmd {
	name, url := f.value(0), f.value(1)
	if name == "" || url == "" {
		f.err = "name and URL are required"
		return nil
	}
	proxyURL := f.value(4)
	if proxyURL != "" {
		if err := proxy.Validate(proxyURL); err != nil {
			f.err = "proxy: " + err.Error()
			return nil
		}
	}
	f.err = ""
	f.busy = true
	args := helm.AddArgs{
		Name:     name,
		URL:      url,
		Username: f.value(2),
		Password: f.fields[3].input.Value(),
		ProxyURL: proxyURL,
	}
	return func() tea.Msg {
		err := a.deps.Helm.Add(context.Background(), args)
		if err == nil && proxyURL != "" {
			if cerr := a.deps.Config.SetProxy(name, proxyURL); cerr != nil {
				err = cerr
			}
		}
		return opDoneMsg{Desc: "added " + name, Err: err, Refresh: true, Source: f}
	}
}

func (f *addForm) View() string {
	return "add repository\n" + f.viewFields()
}

func (f *addForm) hints() string { return "tab next  enter submit  ctrl+u clear field  esc cancel" }

// confirmDialog asks a yes/no question.
type confirmDialog struct {
	title    string
	onYes    func(a *app, self screen) tea.Cmd
	answered bool
	err      string
}

func (d *confirmDialog) setOpError(text string) {
	d.err = text
	d.answered = false
}

func newConfirmDialog(title string, onYes func(a *app, self screen) tea.Cmd) *confirmDialog {
	return &confirmDialog{title: title, onYes: onYes}
}

func (d *confirmDialog) Init(*app) tea.Cmd { return nil }

func (d *confirmDialog) Update(msg tea.Msg, a *app) (screen, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	switch key.String() {
	case "y", "Y":
		if !d.answered {
			d.answered = true
			d.err = ""
			return d, d.onYes(a, d)
		}
	}
	return d, nil
}

func (d *confirmDialog) View() string {
	out := d.title + "\n"
	if d.err != "" {
		out += errStyle.Render(d.err) + "\n"
	}
	return out
}

func (d *confirmDialog) hints() string { return "y confirm  esc cancel" }

// newDeleteDialog confirms and performs repo deletion.
func newDeleteDialog(row RepoRow) *confirmDialog {
	name := row.Repo.Name
	return newConfirmDialog("delete repository "+name+"?", func(a *app, self screen) tea.Cmd {
		return func() tea.Msg {
			err := a.deps.Helm.Remove(context.Background(), name)
			if err == nil {
				_ = a.deps.Config.SetProxy(name, "")
			}
			return opDoneMsg{Desc: "deleted " + name, Err: err, Refresh: true, Source: self}
		}
	})
}

// proxyForm edits the proxy URL for one repository. The current proxy is
// shown masked; the input starts empty so a stored password never reaches the
// screen. Empty Enter keeps the proxy, ctrl+d clears it.
type proxyForm struct {
	form
	repo    string
	current string // masked
}

func newProxyForm(repo, current string) *proxyForm {
	f := &proxyForm{repo: repo, current: proxy.Mask(current)}
	f.fields = []field{newField("new proxy", "http://, https:// or socks5://", false)}
	f.refocus()
	return f
}

func (f *proxyForm) Init(*app) tea.Cmd { return nil }

func (f *proxyForm) Update(msg tea.Msg, a *app) (screen, tea.Cmd) {
	if f.handleFormKeys(msg) {
		return f, nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "enter":
			if f.busy {
				return f, nil
			}
			if f.value(0) == "" {
				return f, func() tea.Msg { return popScreenMsg{} } // no change
			}
			return f, f.submit(a, f.value(0))
		case "ctrl+d":
			if f.busy {
				return f, nil
			}
			return f, f.submit(a, "")
		}
	}
	return f, f.updateFields(msg, a)
}

// submit stores proxyURL ("" clears the proxy).
func (f *proxyForm) submit(a *app, proxyURL string) tea.Cmd {
	if proxyURL != "" {
		if err := proxy.Validate(proxyURL); err != nil {
			f.err = proxy.MaskText(err.Error())
			return nil
		}
	}
	f.err = ""
	f.busy = true
	repo := f.repo
	return func() tea.Msg {
		err := a.deps.Config.SetProxy(repo, proxyURL)
		desc := "proxy for " + repo + " set"
		if proxyURL == "" {
			desc = "proxy for " + repo + " cleared"
		}
		return opDoneMsg{Desc: desc, Err: err, Refresh: true, Source: f}
	}
}

func (f *proxyForm) View() string {
	cur := f.current
	if cur == "" {
		cur = "none"
	}
	return "proxy for " + f.repo + "\n" +
		dimStyle.Render("current: "+cur) + "\n" + f.viewFields()
}

func (f *proxyForm) hints() string {
	return "enter save (empty = no change)  ctrl+d clear proxy  esc cancel"
}

// updateOneCmd runs `helm repo update` for a single repo.
func updateOneCmd(a *app, name string) tea.Cmd {
	proxyURL := a.proxyFor(name)
	return func() tea.Msg {
		err := a.deps.Helm.Update(context.Background(), name, proxyURL)
		return opDoneMsg{Desc: "updated " + name, Err: err, RefreshIndexes: true}
	}
}

// updateAllCmd updates every repo, collecting all failures.
func updateAllCmd(a *app) tea.Cmd {
	names := make([]string, 0)
	if rs, ok := a.top().(*reposScreen); ok {
		for _, r := range rs.rows {
			names = append(names, r.Repo.Name)
		}
	}
	return func() tea.Msg {
		var failed []string
		for _, n := range names {
			if err := a.deps.Helm.Update(context.Background(), n, a.proxyFor(n)); err != nil {
				failed = append(failed, n+": "+err.Error())
			}
		}
		if len(failed) > 0 {
			return opDoneMsg{Err: &multiError{msgs: failed}, RefreshIndexes: true}
		}
		return opDoneMsg{Desc: "updated " + strconv.Itoa(len(names)) + " repositories", RefreshIndexes: true}
	}
}

// multiError joins several errors, masking URL credentials in each.
type multiError struct {
	msgs []string
}

func (m *multiError) Error() string { return strings.Join(m.msgs, "; ") }

const hintForms = "enter submit  esc cancel"

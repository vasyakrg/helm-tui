package tui

// chromeRows is the number of terminal rows the app reserves around the
// screen body: one for the status line (top) and one for the hint line (bottom).
const chromeRows = 2

// winSize is embedded by screens that adapt to the terminal size. The height
// it holds is the body height (terminal height minus chrome); 0 = unknown.
type winSize struct {
	width, height int
}

func (w *winSize) setSize(width, height int) { w.width, w.height = width, height }

// widthOr returns the known width or def.
func (w *winSize) widthOr(def int) int {
	if w.width > 0 {
		return w.width
	}
	return def
}

// listRows returns how many list rows fit after reserving `reserved` rows for
// headers; 0 means unlimited (size unknown).
func (w *winSize) listRows(reserved int) int {
	if w.height <= 0 {
		return 0
	}
	return maxInt(w.height-reserved, 1)
}

// windowTop returns the first visible index of a list of `total` items shown
// in `rows` rows, given the previous top, so that cursor stays visible.
// rows <= 0 means no limit.
func windowTop(top, cursor, total, rows int) int {
	if rows <= 0 || rows >= total {
		return 0
	}
	if cursor < top {
		top = cursor
	}
	if cursor >= top+rows {
		top = cursor - rows + 1
	}
	if top > total-rows {
		top = total - rows
	}
	if top < 0 {
		top = 0
	}
	return top
}

// windowEnd returns the exclusive end index of the visible window.
func windowEnd(top, total, rows int) int {
	if rows <= 0 || top+rows > total {
		return total
	}
	return top + rows
}

// hinter is implemented by screens that show a key hint line at the bottom.
type hinter interface {
	hints() string
}

func padLeft(s string, w int) string {
	if lipglossWidth(s) >= w {
		return s
	}
	return spaces(w-lipglossWidth(s)) + s
}

func spaces(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

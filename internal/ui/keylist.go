package ui

import (
	"strings"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/nav"
)

// KeyRow is one line of the key-list overlay: the key or key alias joined
// with "/", and what it does.
type KeyRow struct {
	Keys string
	Desc string
}

// KeyList is the floating panel that lists every shortcut, styled after the
// fuzzy finder: same margin box, accent frame when focused, accented keys
// column beside muted descriptions. It is purely informational and
// keyboard-driven, so it carries no animation or cursor state.
type KeyList struct {
	// Focused selects the accent frame instead of the neutral one.
	Focused bool
	Width   int
	Height  int
	Theme   config.Theme
	// Rows override the entries derived from nav.Keybinds; nil means the
	// table itself is the content.
	Rows []KeyRow
}

func NewKeyList(width, height int, theme config.Theme) KeyList {
	return KeyList{Width: width, Height: height, Theme: theme}
}

// BoxWidth matches the fuzzy finder's margins so the two overlays feel like
// the same window system. Content narrower than the key column plus some
// description is not worth rendering, hence the floor.
func (k KeyList) BoxWidth() int {
	w := k.Width - 8
	if w > 100 {
		w = 100
	}
	if w < 24 {
		w = 24
	}
	if w > k.Width {
		w = k.Width
	}
	return w
}

// contentWidth is the space inside the border and padding.
func (k KeyList) contentWidth() int {
	w := k.BoxWidth() - 2 /*border*/ - 4 /*padding*/
	if w < 1 {
		w = 1
	}
	return w
}

// rows falls back to the live keybind table when the caller has not injected
// content, so the overlay can never render hypothetically rather than
// documenting what actually dispatches.
func (k KeyList) rows() []KeyRow {
	if k.Rows != nil {
		return k.Rows
	}
	var rows []KeyRow
	for _, r := range nav.KeyRows() {
		rows = append(rows, KeyRow{Keys: r.Keys, Desc: r.Desc})
	}
	return rows
}

// The key column tracks the widest alias in the rows, clamped so very long
// custom entries cannot push descriptions off panel in a two-column table.
// Somewhere between single letters and ctrl-combos: aliases top out at six
// cells ("ctrl+r"), hence the ceiling.
func keyColumnWidth(rows []KeyRow) int {
	widest := 0
	for _, row := range rows {
		if w := DisplayWidth(row.Keys); w > widest {
			widest = w
		}
	}
	if widest < 8 {
		widest = 8
	}
	if widest > 16 {
		widest = 16
	}
	return widest
}

// Render draws the panel, meant to be overlaid on the live layout. Rows are
// all shown; the ~20 bindings fit any terminal tall enough to run the rest
// of the UI, and overflow simply degrades gracefully via the panel's own
// height clamping.
func (k KeyList) Render() string {
	style := k.Theme.PreviewPanel()
	if k.Focused {
		style = k.Theme.PreviewPanelFocused()
	}

	rows := k.rows()
	contentWidth := k.contentWidth()
	keyCol := keyColumnWidth(rows)

	// The header shares the same left edge as the key column so the title
	// and the table read as one block rather than two indent levels. The
	// leading space mirrors the fuzzy findings' marker space so the two
	// overlays breathe with the same left indentation.
	header := k.Theme.AccentText().Bold(true).Render(" key list ")

	// Descriptions right-align against a line that sits descGap cells from
	// the panel's right edge, so the kept space reads as a deliberate
	//   clean margin rather than a run-on sentence.
	const descGap = 25
	descEnd := contentWidth - descGap
	// Narrow panels cannot afford the gap: keep a small but readable
	// description slot instead, still ending short of the content width.
	if descEnd < keyCol+11 {
		descEnd = min(keyCol+11, contentWidth-1)
	}
	descCell := descEnd - keyCol - 1 // one gap column after the key text

	lines := []string{header, ""}
	for _, row := range rows {
		// Keys fill the key column cell-padded; the extra one-column gutter
		// keeps the two columns from ever touching.
		keys := row.Keys
		if overflow := DisplayWidth(keys) - keyCol; overflow >= 0 {
			keys = TruncateEnd(keys, keyCol)
		}
		pad := keyCol - DisplayWidth(keys)
		keysRendered := k.Theme.AccentText().Render(keys + strings.Repeat(" ", pad+1))

		desc := row.Desc
		if overflow := DisplayWidth(desc) - descCell; overflow > 0 {
			desc = TruncateEnd(desc, descCell)
		}
		descRendered := k.Theme.StatusText().Render(strings.Repeat(" ", descCell-DisplayWidth(desc)) + desc)
		lines = append(lines, " "+keysRendered+descRendered)
	}

	footer := k.Theme.StatusText().Render(" ctrl+k/esc:close")
	lines = append(lines, "", footer)

	// Same graceful degradation as the finder: on a short terminal the blank
	// separators go first so header, rows, and footer stay in view.
	budget := k.Height - 4
	if budget < 1 {
		budget = 1
	}
	for len(lines) > budget {
		dropped := false
		for i := len(lines) - 2; i >= 0; i-- {
			if lines[i] == "" {
				lines = append(lines[:i], lines[i+1:]...)
				dropped = true
				break
			}
		}
		if !dropped {
			break
		}
	}

	// The border adds two columns over the styled content width, mirroring
	// the finder so the outer box cannot exceed the terminal.
	return style.
		Width(k.BoxWidth() - 2).
		MaxWidth(k.BoxWidth() - 2).
		Render(strings.Join(lines, "\n"))
}

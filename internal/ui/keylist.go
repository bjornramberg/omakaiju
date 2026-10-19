package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/nav"
)

// KeyRow is one line of the key-list overlay: the key or the merged key
// aliases, and what the action does.
type KeyRow struct {
	Keys string
	Desc string
}

// KeyList is the floating panel listing every shortcut, styled and driven
// like the fuzzy finder: type to search across keys and descriptions, move
// with the arrow keys while the cursor row highlights, and nothing in the
// panel takes an action — it is a browsable reference, not a launcher.
type KeyList struct {
	// Focused selects the accent frame instead of the neutral one.
	Focused bool
	Width   int
	Height  int
	Theme   config.Theme
	// Rows override the entries derived from nav.Keybinds; nil means the
	// table itself is the content.
	Rows []KeyRow
	// Input is the live search, matched case-insensitively against both
	// the key text and the description.
	Input string
	// Cursor is the highlighted row in the filtered list; render clamps it.
	Cursor int
}

func NewKeyList(width, height int, theme config.Theme) KeyList {
	return KeyList{Width: width, Height: height, Theme: theme}
}

// BoxWidth matches the fuzzy finder's margins so the two overlays feel like
// the same window system.
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

// baseRows are the unfiltered rows. An injected Rows slice wins so tests and
// future panels can override; otherwise the live keybind table is the
// source, and the overlay can never document a binding that does not
// actually dispatch.
func (k KeyList) baseRows() []KeyRow {
	if k.Rows != nil {
		return k.Rows
	}
	return TableKeyRows()
}

// TableKeyRows hands the model the unfiltered rows derived from the live
// keybind table — the same source the panel renders — so cursor bounds at
// dispatch time always match what the panel shows.
func TableKeyRows() []KeyRow {
	rows := make([]KeyRow, 0, len(nav.Keybinds))
	for _, r := range nav.KeyRows() {
		rows = append(rows, KeyRow{Keys: r.Keys, Desc: r.Desc})
	}
	return rows
}

// FilterKeyRows keeps rows whose key text or description contains the query,
// case-insensitively, so searching "yank" surfaces the copy binding while
// "pane" surfaces everything mentioning panes. An empty query passes all.
func FilterKeyRows(rows []KeyRow, query string) []KeyRow {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return rows
	}
	var out []KeyRow
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row.Keys), q) ||
			strings.Contains(strings.ToLower(row.Desc), q) {
			out = append(out, row)
		}
	}
	return out
}

// rows is the filtered, drawable set.
func (k KeyList) rows() []KeyRow {
	return FilterKeyRows(k.baseRows(), k.Input)
}

// keyColumnWidth tracks the widest alias in the unfiltered set, so the
// description column does not hop sideways as search results narrow.
// Clamped so a rogue custom entry cannot shove content off the panel.
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

// maxShownRows caps the visible window like the finder, but with room for
// the extra prompt line: a short terminal scrolls rather than spills.
func (k KeyList) maxShownRows() int {
	quota := k.Height - 11
	if quota > 24 {
		quota = 24
	}
	if quota < 1 {
		quota = 1
	}
	return quota
}

// visibleWindow is the finder's window logic verbatim: cursor-centred on
// overflow, with "..." markers showing where the list continues.
func (k KeyList) visibleWindow(total int) (start, end int, above, below bool) {
	quota := k.maxShownRows()
	if total <= quota {
		return 0, total, false, false
	}

	markers := 0
	switch {
	case quota >= 3:
		markers = 2
	case quota == 2:
		markers = 1
	}
	capacity := quota - markers
	if quota >= 3 && capacity < 1 {
		capacity = 1
	}

	start = k.Cursor - capacity/2
	if start < 0 {
		start = 0
	}
	if start > total-capacity {
		start = total - capacity
	}
	end = start + capacity
	if end > total {
		end = total
	}

	above = start > 0
	below = end < total
	if markers == 1 && above && below {
		above = false
	}
	if markers == 0 {
		above, below = false, false
	}
	return start, end, above, below
}

// emphasizeText renders text in a base style with every case-insensitive
// occurrence of the query accented, restoring the finder's
// matched-character treatment inside the key column and the descriptions
// alike.
func emphasizeText(text, query string, base, accent lipgloss.Style) string {
	if query == "" {
		return base.Render(text)
	}
	lower := strings.ToLower(text)
	lq := strings.ToLower(query)

	var out []string
	for lower != "" {
		if i := strings.Index(lower, lq); i >= 0 {
			out = append(out,
				base.Render(text[:i]),
				accent.Render(text[i:i+len(query)]),
			)
			text = text[i+len(query):]
			lower = strings.ToLower(text)
			continue
		}
		out = append(out, base.Render(text))
		break
	}
	return strings.Join(out, "")
}

// Render draws the panel; the result is meant to be overlaid on the live
// layout exactly like the finder's panel.
func (k KeyList) Render() string {
	style := k.Theme.PreviewPanel()
	if k.Focused {
		style = k.Theme.PreviewPanelFocused()
	}

	rows := k.rows()
	total := len(rows)

	// The key column stays anchored on the unfiltered widths so the
	// description column does not dance between keystrokes.
	keyCol := keyColumnWidth(k.baseRows())
	descCol := k.contentWidth() - 1 /*marker*/ - keyCol - 2 /*gap*/
	if descCol < 1 {
		descCol = 1
	}

	cursor := k.Cursor
	if cursor >= total {
		cursor = total - 1
	}
	if cursor < 0 {
		cursor = 0
	}

	search := strings.TrimSpace(k.Input)
	start, end, above, below := k.visibleWindow(total)

	header := k.Theme.AccentText().Bold(true).Render(" key list ")
	prompt := k.Theme.AccentText().Render("> ")
	input := k.Theme.Text().Render(TruncateEnd(search, k.contentWidth()-2))

	var rowLines []string
	switch {
	case total == 0:
		rowLines = append(rowLines, k.Theme.StatusText().Render("no matching key"))
	default:
		if above {
			rowLines = append(rowLines, k.Theme.StatusText().Render("  ..."))
		}
		for i := start; i < end; i++ {
			rowLines = append(rowLines, k.renderRow(rows[i], i == cursor, keyCol, descCol, search))
		}
		if below {
			rowLines = append(rowLines, k.Theme.StatusText().Render("  ..."))
		}
	}

	// The footer keeps the same shape as the finder: position first, then
	// hints, so a user fluent in one panel is fluent in the other.
	var parts []string
	if total > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d", cursor+1, total))
	}
	parts = append(parts, "type to search", "up/down:move", "esc:close")
	footer := k.Theme.StatusText().Render(strings.Join(parts, " | "))

	lines := []string{header, "", prompt + input, ""}
	lines = append(lines, rowLines...)
	lines = append(lines, "", footer)

	// Same graceful degradation as the finder: blank separators go first so
	// the header, rows, and footer stay in view on short terminals.
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

	// Border plus two columns wider, word for word the finder's outer
	// sizing so the window never exceeds the terminal.
	return style.
		Width(k.BoxWidth() - 2).
		MaxWidth(k.BoxWidth() - 2).
		Render(strings.Join(lines, "\n"))
}

// renderRow shows the key prominent with the description riding right after —
// the same two-part row shape as the finder, cells budgeted so the row
// cannot wrap. The cursor row uses the fileitem-active treatment
// (selection background + bold); inactive rows stay plain.
func (k KeyList) renderRow(row KeyRow, active bool, keyCol, descCol int, query string) string {
	keys := row.Keys
	if d := DisplayWidth(keys); d > keyCol {
		keys = TruncateEnd(keys, keyCol)
	} else {
		keys += strings.Repeat(" ", keyCol-d+1)
	}
	desc := row.Desc
	if d := DisplayWidth(desc); d > descCol {
		desc = TruncateEnd(desc, descCol)
	} else {
		desc += strings.Repeat(" ", descCol-d)
	}

	keysPart := emphasizeText(keys, query, k.Theme.Text(), k.Theme.AccentText())
	descPart := emphasizeText(desc, query, k.Theme.StatusText(), k.Theme.AccentText())

	style := k.Theme.FileItem()
	if active {
		style = k.Theme.FileItemActive()
	}
	return style.UnsetPaddingLeft().UnsetPaddingRight().Render(" " + keysPart + descPart)
}

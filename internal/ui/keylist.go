package ui

import (
	"fmt"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/nav"
)

// sectionOrder defines the display order of sections in the key list overlay.
var sectionOrder = []nav.Section{
	nav.SectionNavigation,
	nav.SectionFileOps,
	nav.SectionPaneView,
	nav.SectionJump,
	nav.SectionSystem,
}

// sectionHeaders provides the display label for each section.
var sectionHeaders = map[nav.Section]string{
	nav.SectionNavigation: "Navigation",
	nav.SectionFileOps:    "File operations",
	nav.SectionPaneView:   "UI / View",
	nav.SectionJump:       "Jump",
	nav.SectionSystem:     "System",
}

// sectionSep is the separator line used under section headers.
const sectionSep = "-------------------"

// KeyRow is one line of the key-list overlay: the key or the merged key
// aliases, and what the action does.
type KeyRow struct {
	Keys    string
	Desc    string
	Section nav.Section
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

// contentWidth is the space inside the border and padding. Empirically the
// style's Width() sizes the whole block including the border (a panel built
// at 90 cells renders a 90-cell outer box with 84 content cells inside), so
// both are subtracted here — omitting the border showed up as phantom wraps
// on rows two cells "inside" what the math had promised.
func (k KeyList) contentWidth() int {
	w := k.BoxWidth() - 8 /*border + padding, see comment*/
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
		rows = append(rows, KeyRow{Keys: r.Keys, Desc: r.Desc, Section: r.Section})
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
	if quota > 40 {
		quota = 40
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
//
// The match is done rune by rune rather than by slicing bytes. A case fold
// can change a string's byte length without changing its rune count — the
// Kelvin sign is three bytes and folds to one — so a byte offset into the
// lowercased text does not address the same character in the original. The
// old byte-based version sliced the original text with the original query's
// length and ran off the end of the string, which ended the session.
func emphasizeText(text, query string, base, accent lipgloss.Style) string {
	if query == "" {
		return base.Render(text)
	}

	textRunes := []rune(text)
	queryRunes := []rune(query)

	var out []string
	// pending is the start of the run of unmatched characters. It is flushed
	// as one segment so the common case emits a handful of styled pieces
	// rather than one per character.
	pending := 0

	for i := 0; i < len(textRunes); {
		end, ok := matchRunes(textRunes, queryRunes, i)
		if !ok {
			i++
			continue
		}
		if pending < i {
			out = append(out, base.Render(string(textRunes[pending:i])))
		}
		out = append(out, accent.Render(string(textRunes[i:end])))
		i = end
		pending = i
	}
	if pending < len(textRunes) {
		out = append(out, base.Render(string(textRunes[pending:])))
	}
	return strings.Join(out, "")
}

// matchRunes reports whether query matches text at position i, comparing
// rune by rune so a case fold that changes the byte length cannot shift the
// match out of bounds. It returns the position just past the match.
func matchRunes(text, query []rune, i int) (int, bool) {
	if len(query) == 0 || i+len(query) > len(text) {
		return i, false
	}
	for k, q := range query {
		if unicode.ToLower(text[i+k]) != unicode.ToLower(q) {
			return i, false
		}
	}
	return i + len(query), true
}

// renderItem represents a line in the rendered key list: a key row, a section
// header, or a section separator. Only key rows are selectable.
type renderItem struct {
	kind    renderItemKind
	row     KeyRow      // valid when kind == itemKeyRow
	section nav.Section // valid when kind == itemHeader
}

type renderItemKind int

const (
	itemKeyRow renderItemKind = iota
	itemHeader
	itemSeparator
	itemBlank
)

// buildRenderItems constructs the flat list of render items from filtered rows,
// grouping by section in sectionOrder and inserting headers/separators.
func (k KeyList) buildRenderItems(rows []KeyRow) []renderItem {
	// Group rows by section
	bySection := make(map[nav.Section][]KeyRow)
	for _, r := range rows {
		bySection[r.Section] = append(bySection[r.Section], r)
	}

	var items []renderItem
	firstSection := true
	for _, sec := range sectionOrder {
		secRows, ok := bySection[sec]
		if !ok || len(secRows) == 0 {
			continue
		}
		// Blank line before each section except the first
		if !firstSection {
			items = append(items, renderItem{kind: itemBlank})
		}
		firstSection = false
		// Section header
		items = append(items, renderItem{kind: itemHeader, section: sec})
		// Separator
		items = append(items, renderItem{kind: itemSeparator})
		// Rows in this section
		for _, r := range secRows {
			items = append(items, renderItem{kind: itemKeyRow, row: r})
		}
	}
	return items
}

// countSelectable returns the number of selectable (key row) items.
func countSelectable(items []renderItem) int {
	n := 0
	for _, it := range items {
		if it.kind == itemKeyRow {
			n++
		}
	}
	return n
}

// selectableIndexToItemIndex converts a selectable-row index (0-based) to
// the corresponding index in the full items slice.
func selectableIndexToItemIndex(items []renderItem, selIdx int) int {
	count := 0
	for i, it := range items {
		if it.kind == itemKeyRow {
			if count == selIdx {
				return i
			}
			count++
		}
	}
	return len(items) - 1
}

// itemIndexToSelectableIndex converts an items index to the selectable-row index.
func itemIndexToSelectableIndex(items []renderItem, itemIdx int) int {
	count := 0
	for i, it := range items {
		if i == itemIdx {
			return count
		}
		if it.kind == itemKeyRow {
			count++
		}
	}
	return count
}

// Render draws the panel; the result is meant to be overlaid on the live
// layout exactly like the finder's panel.
func (k KeyList) Render() string {
	style := k.Theme.PreviewPanel()
	if k.Focused {
		style = k.Theme.PreviewPanelFocused()
	}

	filteredRows := k.rows()

	// Build render items (headers, separators, key rows)
	items := k.buildRenderItems(filteredRows)
	totalSelectable := countSelectable(items)
	totalItems := len(items)

	// The key column stays anchored on the unfiltered widths so the
	// description column does not dance between keystrokes.
	keyCol := keyColumnWidth(k.baseRows())
	// One spare cell keeps the composed row clear of the style's internal
	// padding arithmetic — the widest line stays one cell inside the
	// content edge rather than riding it, so nothing ever re-wraps.
	descW := k.contentWidth() - 3 - keyCol
	if descW < 1 {
		descW = 1
	}

	search := strings.TrimSpace(k.Input)

	// Clamp cursor to selectable rows
	cursor := k.Cursor
	if cursor >= totalSelectable {
		cursor = totalSelectable - 1
	}
	if cursor < 0 {
		cursor = 0
	}

	// Convert selectable cursor to item index for visibleWindow
	itemCursor := selectableIndexToItemIndex(items, cursor)

	// visibleWindow on the full items list (headers/separators count as lines)
	start, end, above, below := k.visibleWindowItems(totalItems, itemCursor)

	header := k.Theme.AccentText().Bold(true).Render(" key list ")
	prompt := k.Theme.AccentText().Render("> ")
	input := k.Theme.Text().Render(TruncateEnd(search, k.contentWidth()-2))

	var rowLines []string
	switch {
	case totalSelectable == 0:
		rowLines = append(rowLines, k.Theme.StatusText().Render("no matching key"))
	default:
		if above {
			rowLines = append(rowLines, k.Theme.StatusText().Render("  ..."))
		}
		for i := start; i < end; i++ {
			active := (i == itemCursor)
			rowLines = append(rowLines, k.renderItem(items[i], active, keyCol, descW, search))
		}
		if below {
			rowLines = append(rowLines, k.Theme.StatusText().Render("  ..."))
		}
	}

	// The footer keeps the same shape as the finder: position first, then
	// hints, so a user fluent in one panel is fluent in the other.
	var parts []string
	if totalSelectable > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d", cursor+1, totalSelectable))
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

// visibleWindowItems is like visibleWindow but operates on the full items list
// (including headers/separators). The cursor is an item index.
func (k KeyList) visibleWindowItems(total int, cursor int) (start, end int, above, below bool) {
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

	start = cursor - capacity/2
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

// renderItem renders a single item (key row, header, or separator).
func (k KeyList) renderItem(it renderItem, active bool, keyCol, descW int, query string) string {
	switch it.kind {
	case itemHeader:
		label := sectionHeaders[it.section] + ":"
		return k.Theme.AccentText().Bold(true).Render(" " + label)
	case itemSeparator:
		// Fixed-width separator matching the longest header (same tone as footer)
		sep := sectionSep
		return k.Theme.StatusText().Render(" " + sep)
	case itemBlank:
		return ""
	case itemKeyRow:
		return k.renderRow(it.row, active, keyCol, descW, query)
	}
	return ""
}

// renderRow shows the key prominent with the description right-aligned
// along the panel's right edge. The cursor row uses the fileitem-active
// treatment (selection background + bold), and the inner segment styles
// carry that background themselves so the highlight paints continuously
// through the description rather than stopping at the style boundaries.
func (k KeyList) renderRow(row KeyRow, active bool, keyCol, descW int, query string) string {
	keys := row.Keys
	if d := DisplayWidth(keys); d > keyCol {
		keys = TruncateEnd(keys, keyCol)
	} else {
		keys += strings.Repeat(" ", keyCol-d+1)
	}
	desc := row.Desc
	if d := DisplayWidth(desc); d > descW {
		desc = TruncateEnd(desc, descW)
	} else {
		desc = strings.Repeat(" ", descW-d) + desc
	}

	keyBase, descBase, accent := k.Theme.Text(), k.Theme.StatusText(), k.Theme.AccentText()
	if active {
		bg := lipgloss.Color(k.Theme.SelectionBG)
		keyBase = keyBase.Bold(true).Background(bg)
		descBase = descBase.Background(bg)
		accent = accent.Background(bg)
	}
	keysPart := emphasizeText(keys, query, keyBase, accent)
	descPart := emphasizeText(desc, query, descBase, accent)

	style := k.Theme.FileItem()
	if active {
		style = k.Theme.FileItemActive()
	}
	return style.UnsetPaddingLeft().UnsetPaddingRight().Render(" " + keysPart + descPart)
}

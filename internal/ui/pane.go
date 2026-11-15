package ui

import (
	"fmt"
	"strings"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/icons"

	"charm.land/lipgloss/v2"
)

// DropState says what would happen if the pointer were released over the
// pane while a drag is in progress. A single value carries all three states
// so the app can set the target pane in one assignment and the pane can say
// "nothing here" without needing a second field.
type DropState int

const (
	// DropNone means the pane is not a valid drop target right now: the
	// pointer is over chrome, the other pane is the source, or no drag is
	// happening. The pane draws its ordinary border and header.
	DropNone DropState = iota
	// DropMove means releasing the pointer over this pane would move the
	// dragged entry into this pane's directory.
	DropMove
	// DropCopy means releasing the pointer over this pane would leave a copy
	// of the dragged entry in this pane's directory.
	DropCopy
)

type Pane struct {
	Width      int
	Height     int
	Active     bool
	Theme      config.Theme
	Path       string
	Filter     string
	Files      []fs.Entry
	TotalFiles int
	Cursor     int
	// Scroll is the index of the first row the pane draws. The window is the
	// model's state, not a function of the cursor: a pane scrolled to the
	// middle of a long list must stay there when the cursor moves to a row
	// inside it.
	Scroll        int
	VisibleHeight int
	Status        string
	Marked        map[string]bool
	// Parent is the path of the directory above the one the pane shows, and
	// empty when the pane is at the root and there is none. A pane with a
	// parent draws a ".." row above its listing: the way back up, which the
	// keyboard reaches with the up arrow and Enter and the mouse by clicking
	// the row.
	Parent string
	// Drop says what a release over this pane would do during a drag. The
	// visual side uses it to tint the target pane and to show whether the
	// operation would move or copy; the app side sets it every frame.
	Drop DropState
}

func NewPane(width, height int, active bool, theme config.Theme) Pane {
	return Pane{
		Width:  width,
		Height: height,
		Active: active,
		Theme:  theme,
	}
}

// ContentWidth is the usable width inside the pane border and its padding.
// Width alone is a minimum in Lipgloss, so rows must be sized to this or they
// spill past the border and wrap into phantom rows.
func (p Pane) ContentWidth() int {
	w := p.Width - 2 /*border*/ - 4 /*padding*/
	if w < 1 {
		return 1
	}
	return w
}

// paneRowsTop is the pane-local row the first file row occupies: the top
// border, the top padding, the path header, and the blank line under it.
//
// A pane with a ".." row draws it here and starts its listing one row lower,
// which is why the hit-test asks for this row before it asks for a file row.
const paneRowsTop = 4

// ParentCursor is the cursor position that means the pane is on its ".." row:
// the way back to the directory above, which sits above the listing and is not
// one of its entries, so it has no index among them.
//
// The files keep the indices they always had; this position lives outside that
// index space, which is why every place that reads the cursor's entry has to
// rule it out before indexing. A pane at the filesystem root has no ".." row,
// so its cursor is never ParentCursor.
const ParentCursor = -1

// Rows is the window of Files a pane draws: the index of the first entry, how
// many rows the body has, and where the first one sits inside the pane frame.
//
// Render walks this window, and the mouse hit-test maps a screen row back
// through it. Both read the same numbers, so the entry under the pointer is
// always the entry the renderer drew there.
type Rows struct {
	Start int
	Count int
	Top   int
}

// Rows computes the listing window the pane draws. It is the single source of
// truth for the pane's row math; Render must not compute its own.
//
// The window comes from Scroll, not from the cursor. Deriving it from the
// cursor made every cursor move scroll the listing, so a click on a row in the
// middle of a long list dragged the window down and left the clicked entry on
// the last row. Here the window stays put and the cursor moves inside it.
//
// The ".." row is not part of the window: it takes a row of the body above the
// listing and never scrolls, so Top moves down by one when the pane has a
// parent and the hit-test resolves that row as its own target.
func (p Pane) Rows() Rows {
	visible := p.fileRows()
	top := paneRowsTop
	if p.Parent != "" {
		top++
	}
	// An empty listing draws one placeholder row, which is not an entry and
	// cannot be selected.
	if len(p.Files) == 0 {
		return Rows{Top: top}
	}
	start := p.Scroll
	if start < 0 {
		start = 0
	}
	// The window cannot reach past the end of the listing.
	if maxStart := len(p.Files) - visible; maxStart > 0 && start > maxStart {
		start = maxStart
	}
	// The cursor rides inside the window. The app keeps that true; this is
	// the safety net for a pane whose state has not been reconciled yet.
	//
	// A cursor on the ".." row is above the listing and is not one of its
	// entries, so it has no say in which window is drawn. Indexing the
	// listing with it would reach a row that does not exist.
	if p.Cursor >= 0 {
		if p.Cursor < start {
			start = p.Cursor
		}
		if p.Cursor >= start+visible {
			start = p.Cursor - visible + 1
		}
	}
	return Rows{Start: start, Count: visible, Top: top}
}

// ListRows is how many file rows a pane of the given height has, once its
// frame, its header, its status row and its parent row have been taken off it.
// It is at least one row, so a degenerate height cannot produce an empty body.
//
// The app uses it to decide how much of the listing a pane can show without
// building a Pane and copying the file list just to ask. It must agree with
// the pane's own row math exactly, or the model and the renderer would scroll
// by different amounts.
func ListRows(height int, filter string, hasStatus bool, hasParent bool) int {
	p := Pane{Height: height, Filter: filter}
	if hasStatus {
		p.Status = "x"
	}
	if hasParent {
		p.Parent = "parent"
	}
	return p.fileRows()
}

// fileRows is how many rows the pane's body gives to the listing, once the
// parent row has taken its share of it.
func (p Pane) fileRows() int {
	visible := p.visibleRows()
	if p.Parent != "" {
		visible--
	}
	if visible < 1 {
		visible = 1
	}
	return visible
}

// visibleRows is how many rows the pane's body has room for after its frame,
// its header, and any status row, before the parent row has taken its share.
// At least one row is always reported so a degenerate height cannot produce an
// empty body.
func (p Pane) visibleRows() int {
	listRows := p.Height - 4 - 2 - p.statusRows()
	if listRows < 1 {
		listRows = 1
	}
	visible := p.VisibleHeight
	if visible <= 0 || visible > listRows {
		visible = listRows
	}
	return visible
}

// statusRows is how many rows the status area takes at the bottom of the pane:
// a filter gets a blank line plus its count, a status alone gets one row.
func (p Pane) statusRows() int {
	if p.Filter != "" {
		return 2
	}
	if p.Status != "" {
		return 1
	}
	return 0
}

func (p Pane) Render() string {
	var borderStyle lipgloss.Style
	if p.Active {
		borderStyle = p.Theme.PaneActive()
	} else {
		borderStyle = p.Theme.PaneInactive()
	}
	// A pane that would receive the drop gets the accent colour on its border,
	// not the focus colour. The drag started in the focused pane, so leaving
	// the focused pane's primary border unchanged would make the source and
	// target look identical; the accent border points at the destination.
	if p.Drop != DropNone {
		borderStyle = borderStyle.BorderForeground(lipgloss.Color(p.Theme.Accent))
	}
	// Both dimensions are bounded. Width alone left the frame free to grow
	// taller than the terminal, which scrolls the view and makes the whole UI
	// jump while a window is being resized.
	borderStyle = borderStyle.MaxWidth(p.Width).MaxHeight(p.Height)

	inner := p.ContentWidth()

	// Rows available for file entries: the frame (border 2 + padding 2), the
	// path header plus its blank line, and an optional status line. Rows
	// shares the status-row count with the hit-test.
	statusRows := p.statusRows()
	status := ""
	if p.Filter != "" {
		status = fmt.Sprintf("%d/%d", len(p.Files), p.TotalFiles)
		if p.Status != "" {
			// The status can quote a filesystem name, which may contain
			// control bytes. Clean it before it reaches the terminal.
			status += "  " + SanitizeName(p.Status)
		}
	} else if p.Status != "" {
		status = SanitizeName(p.Status)
	}

	// The header reserves a marker column in both panes, so the path starts
	// in the same column either way and the marker is a shape cue first: it
	// says which pane has focus even where the colours are too close to tell
	// apart. A pane too narrow for a marker keeps the whole width for the
	// path.
	marker, sep := "", ""
	pathWidth := inner
	if inner >= 3 {
		marker = " "
		if p.Active {
			marker = "▸"
		}
		sep = " "
		pathWidth = inner - 2
	}

	// A drop target explains what would happen if the pointer were released.
	// The tag lives in the header so it cannot push the listing down, and it
	// is right-aligned so the path still starts in the same column whether the
	// tag is shown or not.
	tag := p.dropTag()
	tagWidth := DisplayWidth(tag)
	if tagWidth > 0 && pathWidth > tagWidth+1 {
		// A cell is kept for the gap between the path and the tag, so a long
		// path does not run straight into the word.
		pathWidth -= tagWidth + 1
	} else {
		// The pane is too narrow for both the path and the tag; keep the
		// border colour as the cue and drop the word so the header does not
		// wrap and steal a row from the body.
		tag = ""
		tagWidth = 0
	}

	path := TruncateName(SanitizeName(p.Path), pathWidth)
	left := marker + sep + path
	leftWidth := DisplayWidth(left)
	pad := inner - leftWidth - tagWidth
	if pad < 0 {
		pad = 0
	}

	// The header is rendered as a single run, so the focused pane's band
	// covers the marker cell along with the path.
	headerStyle := p.Theme.PaneHeaderInactive()
	if p.Active {
		headerStyle = p.Theme.PaneHeaderActive()
	}
	// Tint the header text with the accent colour on a drop target. The
	// border already changed, but a colour-blind user or a monochrome
	// terminal still needs the word and the glyph; the tint makes the header
	// read as one continuous cue with the border.
	if p.Drop != DropNone {
		headerStyle = headerStyle.Foreground(lipgloss.Color(p.Theme.Accent))
	}
	header := headerStyle.Width(inner).MaxWidth(inner).Render(left + strings.Repeat(" ", pad) + tag)

	var listing []string
	if len(p.Files) == 0 {
		// One placeholder row, padded out so the frame keeps its height.
		msg := "empty directory"
		if p.Filter != "" {
			msg = "no matches for " + SanitizeName(p.Filter)
		}
		listing = append(listing, p.Theme.StatusText().Render(TruncateName(msg, inner)))
	} else {
		// The row window comes from Rows, the same one the mouse hit-test
		// resolves a click through.
		window := p.Rows()
		start := window.Start
		end := start + window.Count
		if end > len(p.Files) {
			end = len(p.Files)
		}

		listing = make([]string, 0, window.Count)
		for i := start; i < end; i++ {
			item := NewFileItem(p.Files[i], i == p.Cursor, p.Marked[p.Files[i].Path], p.Theme, inner)
			item.Unfocused = !p.Active
			listing = append(listing, item.Render())
		}
		// Pad short lists so the pane always occupies its full column height.
		for len(listing) < window.Count {
			listing = append(listing, "")
		}
	}
	// The parent row goes above the listing rather than being one of its
	// rows, so it cannot scroll away and the entries stay exactly as they
	// were on the rows below it.
	if p.Parent != "" {
		listing = append([]string{p.parentRow(inner)}, listing...)
	}
	body := strings.Join(listing, "\n")

	content := header + "\n\n" + body
	if status != "" {
		content += "\n" + strings.Repeat("\n", statusRows-1) + p.Theme.AccentText().Render(status)
	}

	// Height pads a short pane out to its allocation; MaxHeight clips one that
	// somehow still runs long.
	return borderStyle.Width(p.Width).Height(p.Height).Render(content)
}

// parentRow is the ".." row: the way back to the directory above the one the
// pane shows.
//
// It is drawn apart from the entries because it is not one of them: the
// listing's copy, mark, rename and delete never see it, and it answers only to
// Enter and to a double-click, both of which go up a level. It still draws with
// the selection bar when the cursor is parked on it, because a cursor the user
// cannot see is worse than no cursor at all.
func (p Pane) parentRow(width int) string {
	style := p.Theme.FileItemDir()
	if !p.Active {
		style = p.Theme.FileItemDirDimmed()
	}
	// The cursor reaches this row with the up arrow, so it has to look
	// selected there. The styles are the ones a file row uses, so the
	// selection looks the same on either side of the listing.
	if p.Cursor == ParentCursor {
		if p.Active {
			style = p.Theme.FileItemActive()
		} else {
			style = p.Theme.FileItemCursorUnfocused()
		}
	}
	prefix := ""
	if arrow := icons.Glyph(icons.UpIcon); arrow != "" {
		prefix = arrow + " "
	}
	content := prefix + ".."
	pad := width - DisplayWidth(content)
	if pad < 0 {
		pad = 0
	}
	// The row's style carries horizontal padding, but the row spaces itself,
	// so leaving padding on would add unaccounted columns.
	row := style.UnsetPaddingLeft().UnsetPaddingRight()
	return row.Width(width).MaxWidth(width).Render(content + strings.Repeat(" ", pad))
}

// dropTag is the header hint that shows what releasing the pointer would do.
// It returns an empty string when the pane is not a drop target, so the header
// keeps its ordinary layout.
func (p Pane) dropTag() string {
	switch p.Drop {
	case DropMove:
		// A rightward arrow says the entry is travelling into this pane.
		return "→ move"
	case DropCopy:
		// The copy glyph suggests duplication without removing the original;
		// the word removes any ambiguity the glyph might carry. It comes from
		// the icon set, so a terminal without the Nerd Font shows the word
		// alone rather than a box.
		return icons.Glyph(icons.CopyIcon) + " copy"
	default:
		return ""
	}
}

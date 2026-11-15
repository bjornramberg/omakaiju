package ui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/bjornramberg/omakaiju/internal/config"
)

// MenuItem is one action in a context menu.
//
// A separator is declared with Separator set to true; its Label is ignored and
// it is never selectable. Keeping separators inside Items means the index the
// app uses for dispatch is always the index into the original slice, so a
// separator never shifts the numbering of the real actions.
type MenuItem struct {
	Label string
	// Disabled marks an item that cannot be chosen right now.
	Disabled bool
	// Separator marks a divider between groups of items. It is rendered as a
	// horizontal rule and is skipped by the highlight and the hit-test.
	Separator bool
}

// Menu is a floating panel of actions that opens at the pointer.
//
// Selected is the keyboard cursor; Hovered is the item the pointer is
// currently over. They are kept separate so a user aiming with the mouse can
// still see which row the keyboard would act on, and vice versa.
type Menu struct {
	Items []MenuItem
	// Title names the file the menu was opened on, so the user knows which
	// row the actions belong to while the menu floats over a listing.
	Title string
	// Selected is the keyboard highlight. -1 means no item is selected.
	Selected int
	// Hovered is the item under the pointer. -1 means the pointer is not
	// over an item.
	Hovered int
	// Theme supplies the frame, accent and selection colours.
	Theme config.Theme
	// Width is the maximum outer width the menu may occupy. Zero means the
	// menu sizes itself to its content.
	Width int
}

// menuMinOuterWidth leaves room for a short title, the border and the padding.
const menuMinOuterWidth = 14

// menuMinContentWidth is the narrowest content area worth drawing; below this
// labels become almost illegible.
const menuMinContentWidth = 8

// Render draws the menu as a bordered panel.
//
// The panel uses the same rounded border as the other floating panels, with
// the accent colour on the frame to show it is active. The title is rendered
// in the accent colour; the selected item uses the theme's selection
// background; the hovered item shows a bright accent marker so the target is
// obvious even when the pointer and the keyboard cursor are on different rows.
func (m Menu) Render() string {
	outerW := m.outerWidth()
	contentW := m.contentWidth()

	panel := m.Theme.PreviewPanelFocused().
		Background(lipgloss.Color(m.Theme.BGDark)).
		Width(outerW).
		MaxWidth(outerW)

	var lines []string
	if m.Title != "" {
		title := m.Theme.AccentText().
			Bold(true).
			Background(lipgloss.Color(m.Theme.BGDark)).
			Render(TruncateEnd(m.Title, contentW))
		lines = append(lines, title)
		lines = append(lines, m.separatorLine(contentW))
	}

	for i, it := range m.Items {
		if it.Separator {
			lines = append(lines, m.separatorLine(contentW))
			continue
		}
		lines = append(lines, m.renderItem(i, contentW))
	}

	return panel.Render(strings.Join(lines, "\n"))
}

// Size returns the menu's outer dimensions in cells. The app uses these to
// position the menu and to hit-test the pointer against the items.
func (m Menu) Size() (width, height int) {
	outerW := m.outerWidth()
	return outerW, m.contentRows() + 4 // border + padding top and bottom.
}

// ItemAt returns the index of the item under the screen position, or -1 when
// the position is outside the menu or over the title, a separator, or the
// border and padding.
func (m Menu) ItemAt(screenX, screenY, originX, originY int) int {
	outerW, outerH := m.Size()
	if screenX < originX || screenX >= originX+outerW {
		return -1
	}
	if screenY < originY || screenY >= originY+outerH {
		return -1
	}

	// Content lives inside the rounded border and the two-cell side padding.
	left := originX + 3
	right := originX + outerW - 3
	if screenX < left || screenX >= right {
		return -1
	}

	row := screenY - (originY + 2) // skip border and top padding.
	if row < 0 {
		return -1
	}

	header := m.headerHeight()
	if row < header {
		return -1
	}
	row -= header

	for i, it := range m.Items {
		if row < 0 {
			return -1
		}
		if row == 0 {
			if it.Separator {
				return -1
			}
			return i
		}
		row--
	}
	return -1
}

// outerWidth is the width the panel actually occupies. It grows to fit the
// title and the longest label, but never exceeds the caller's requested cap.
func (m Menu) outerWidth() int {
	contentW := m.minContentWidth()
	outer := contentW + 6 // border (2) + left/right padding (4).
	if outer < menuMinOuterWidth {
		outer = menuMinOuterWidth
	}
	if m.Width > 0 && outer > m.Width {
		outer = m.Width
	}
	if outer < menuMinOuterWidth {
		outer = menuMinOuterWidth
	}
	return outer
}

// contentWidth is the usable horizontal space inside the border and padding.
func (m Menu) contentWidth() int {
	w := m.outerWidth() - 6
	if w < menuMinContentWidth {
		w = menuMinContentWidth
	}
	return w
}

// minContentWidth is driven by the title and the longest non-separator label,
// with a floor so very short menus still look like panels.
func (m Menu) minContentWidth() int {
	w := DisplayWidth(m.Title)
	for _, it := range m.Items {
		if it.Separator {
			continue
		}
		if lw := DisplayWidth(it.Label); lw > w {
			w = lw
		}
	}
	if w < menuMinContentWidth {
		w = menuMinContentWidth
	}
	return w
}

// headerHeight is two content rows (title + rule) when a title is present.
func (m Menu) headerHeight() int {
	if m.Title != "" {
		return 2
	}
	return 0
}

// contentRows is the number of lines inside the panel, excluding the border
// and padding.
func (m Menu) contentRows() int {
	return m.headerHeight() + len(m.Items)
}

// separatorLine draws a horizontal divider across the content width.
func (m Menu) separatorLine(contentW int) string {
	return m.Theme.StatusText().
		Background(lipgloss.Color(m.Theme.BGDark)).
		Render(strings.Repeat("─", contentW))
}

// renderItem draws one selectable row. The selected row gets the selection
// background; the hovered row gets a bright accent marker so the pointer
// target is visible even when it is not the keyboard cursor.
func (m Menu) renderItem(idx, contentW int) string {
	it := m.Items[idx]
	label := it.Label
	if m.isHovered(idx) && !m.isSelected(idx) && !it.Disabled {
		label = "▸ " + label
	}
	label = " " + label
	label = TruncateEnd(label, contentW)

	style := m.itemStyle(idx).UnsetPadding().Width(contentW)
	return style.Render(label)
}

// itemStyle picks the style for a row. Selected wins over hovered so the
// keyboard cursor does not disappear when the pointer crosses it.
func (m Menu) itemStyle(idx int) lipgloss.Style {
	it := m.Items[idx]
	switch {
	case it.Disabled:
		return m.Theme.StatusText().Background(lipgloss.Color(m.Theme.BGDark))
	case m.isSelected(idx):
		return m.Theme.FileItemActive()
	case m.isHovered(idx):
		return m.Theme.AccentText().
			Bold(true).
			Background(lipgloss.Color(m.Theme.BGDark))
	default:
		return m.Theme.Text().Background(lipgloss.Color(m.Theme.BGDark))
	}
}

// isSelected reports whether idx is the current keyboard cursor.
func (m Menu) isSelected(idx int) bool {
	return idx == m.Selected && m.Selected >= 0 && m.Selected < len(m.Items)
}

// isHovered reports whether idx is the item the pointer is currently over.
func (m Menu) isHovered(idx int) bool {
	return idx == m.Hovered && m.Hovered >= 0 && m.Hovered < len(m.Items)
}

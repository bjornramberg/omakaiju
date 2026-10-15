package app

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

// v must hide the preview, give the panes the full width, and show again on
// the next press. i on a hidden preview must restore it.
func TestPreviewToggleHidesAndRestores(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 120, 30

	if m.previewHidden {
		t.Fatal("preview should start visible")
	}

	updated, _ := m.Update(key('v'))
	m = updated.(Model)
	if !m.previewHidden {
		t.Fatal("v should hide the preview")
	}
	if m.previewFocused {
		t.Fatal("hiding must clear preview focus")
	}

	updated, _ = m.Update(key('v'))
	m = updated.(Model)
	if m.previewHidden {
		t.Fatal("second v should show the preview again")
	}

	// Hide, then i restores.
	updated, _ = m.Update(key('v'))
	m = updated.(Model)
	updated, _ = m.Update(key('i'))
	m = updated.(Model)
	if m.previewHidden {
		t.Fatal("i should unhide a hidden preview")
	}
	if !m.previewFocused {
		t.Fatal("i should focus the preview")
	}
}

// v must work while the preview is focused, and focus must never lock
// navigation: j/tab/q keep working with the highlight on.
func TestPreviewFocusDoesNotLockKeys(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 120, 30

	updated, _ := m.Update(key('i'))
	m = updated.(Model)
	if !m.previewFocused {
		t.Fatal("i should focus the preview")
	}

	// v while focused hides and unfocuses.
	updated, _ = m.Update(key('v'))
	m = updated.(Model)
	if !m.previewHidden || m.previewFocused {
		t.Fatalf("v while focused: hidden=%v focused=%v, want true/false", m.previewHidden, m.previewFocused)
	}

	// Restore via v, focus again, navigation still moves the cursor.
	updated, _ = m.Update(key('v'))
	m = updated.(Model)
	updated, _ = m.Update(key('i'))
	m = updated.(Model)
	before := m.leftCursor
	updated, _ = m.Update(key('j'))
	m = updated.(Model)
	if m.leftCursor != before+1 {
		t.Fatalf("j while focused: cursor %d -> %d, want +1", before, m.leftCursor)
	}
	if !m.previewFocused {
		t.Fatal("navigation must not clear preview focus")
	}

	// i toggles focus off again.
	updated, _ = m.Update(key('i'))
	m = updated.(Model)
	if m.previewFocused {
		t.Fatal("second i should unfocus the preview")
	}
}

// Shift+V must toggle too, so the key never appears dead with caps/shift.
func TestPreviewToggleUppercase(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 120, 30

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'V', Text: "V"})
	m = updated.(Model)
	if !m.previewHidden {
		t.Fatal("V should hide the preview like v does")
	}
}

// Hiding the preview must hand its width back to the file panes: at 120
// columns each pane grows from a third (40) to a half (60) of the terminal,
// and to the full width when stacked. Showing again (via v, or via i
// restoring a hidden preview) must shrink them back. The rendered view must
// keep fitting the terminal exactly in every state, proving the panes take
// the full estate with no preview column left behind.
func TestPreviewToggleExpandsFilePanes(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 120, 30

	visible := ui.NewLayout(m.width, m.height, m.theme)
	if got := visible.PreviewWidth(); got != 40 {
		t.Fatalf("visible PreviewWidth = %d, want 40", got)
	}
	if got := visible.PaneWidth(); got != 40 {
		t.Fatalf("visible PaneWidth = %d, want 40", got)
	}

	checkFits := func() {
		t.Helper()
		out := stripTestANSI(m.View().Content)
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(lines) != m.height {
			t.Fatalf("hidden=%v stacked=%v: rendered %d lines, want %d",
				m.previewHidden, m.panesStacked, len(lines), m.height)
		}
		for i, l := range lines {
			if got := visibleWidth(l); got > m.width {
				t.Fatalf("hidden=%v stacked=%v: line %d is %d cells wide, want <= %d",
					m.previewHidden, m.panesStacked, i, got, m.width)
			}
		}
	}

	// Hide: panes must widen and the view must still fit exactly.
	updated, _ := m.Update(key('v'))
	m = updated.(Model)
	if !m.previewHidden {
		t.Fatal("v should hide the preview")
	}
	hidden := ui.NewLayout(m.width, m.height, m.theme)
	hidden.PreviewHidden = m.previewHidden
	if got := hidden.PreviewWidth(); got != 0 {
		t.Fatalf("hidden PreviewWidth = %d, want 0", got)
	}
	if got := hidden.PaneWidth(); got != 60 {
		t.Fatalf("hidden PaneWidth = %d, want 60 (half of 120)", got)
	}
	if hidden.PaneWidth() <= visible.PaneWidth() {
		t.Fatalf("hidden PaneWidth = %d, must exceed visible %d",
			hidden.PaneWidth(), visible.PaneWidth())
	}
	checkFits()

	// Stacked panes span the whole panes area, so hidden + stacked is the
	// full terminal width.
	m.panesStacked = true
	stackedHidden := ui.NewLayout(m.width, m.height, m.theme)
	stackedHidden.PanesStacked = true
	stackedHidden.PreviewHidden = true
	if got := stackedHidden.PaneWidth(); got != 120 {
		t.Fatalf("stacked hidden PaneWidth = %d, want 120", got)
	}
	checkFits()

	// Show again via v: the panes shrink back and the view still fits.
	updated, _ = m.Update(key('v'))
	m = updated.(Model)
	if m.previewHidden {
		t.Fatal("second v should show the preview again")
	}
	shown := ui.NewLayout(m.width, m.height, m.theme)
	shown.PanesStacked = true
	if got := shown.PaneWidth(); got != 80 {
		t.Fatalf("stacked visible PaneWidth = %d, want 80", got)
	}
	checkFits()

	// i on a hidden preview restores it, shrinking the panes back too.
	updated, _ = m.Update(key('v'))
	m = updated.(Model)
	updated, _ = m.Update(key('i'))
	m = updated.(Model)
	if m.previewHidden {
		t.Fatal("i should unhide a hidden preview")
	}
	restored := ui.NewLayout(m.width, m.height, m.theme)
	restored.PanesStacked = m.panesStacked
	if got := restored.PaneWidth(); got != 80 {
		t.Fatalf("restored PaneWidth = %d, want 80", got)
	}
	checkFits()
}

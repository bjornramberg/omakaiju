package app

import (
	"testing"

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

package app

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// decodeKey runs raw bytes through the same input decoder Bubble Tea uses and
// returns the resulting key press. Driving the model with these rather than a
// hand-built message proves the physical key works regardless of how a
// terminal spells it: a plain press, a shifted/caps press, or an escape-coded
// form all have to reach the same action. Earlier attempts matched only one
// spelling and left the key dead in real terminals while the unit tests passed.
func decodeKey(t *testing.T, raw string) tea.KeyMsg {
	t.Helper()
	var dec uv.EventDecoder
	n, ev := dec.Decode([]byte(raw))
	if n == 0 || ev == nil {
		t.Fatalf("decoder recognized no event in %q", raw)
	}
	kp, ok := ev.(uv.KeyPressEvent)
	if !ok {
		t.Fatalf("decoded %q into %T, want a key press", raw, ev)
	}
	return tea.KeyPressMsg(kp)
}

// The raw bytes a terminal emits for a physical v must hide the preview, give
// the third of the screen it owned back to the file panes, and show it again on
// the next press. This is the behavior the user sees, checked from the bytes
// the terminal actually sends.
func TestPreviewToggleFromRawTerminalBytes(t *testing.T) {
	for _, raw := range []string{"v", "V"} {
		m, _ := fixture(t)
		m.width, m.height = 120, 30

		visible := ui.NewLayout(m.width, m.height, m.theme)
		if got := visible.PaneWidth(); got != 40 {
			t.Fatalf("raw %q: visible PaneWidth = %d, want 40", raw, got)
		}

		updated, _ := m.Update(decodeKey(t, raw))
		m = updated.(Model)
		if !m.previewHidden {
			t.Fatalf("raw %q: should hide the preview", raw)
		}

		hidden := ui.NewLayout(m.width, m.height, m.theme)
		hidden.PreviewHidden = m.previewHidden
		if got := hidden.PreviewWidth(); got != 0 {
			t.Fatalf("raw %q: hidden PreviewWidth = %d, want 0", raw, got)
		}
		if got := hidden.PaneWidth(); got != 60 {
			t.Fatalf("raw %q: hidden PaneWidth = %d, want 60 (half of 120)", raw, got)
		}
		if hidden.PaneWidth() <= visible.PaneWidth() {
			t.Fatalf("raw %q: hidden PaneWidth = %d must exceed visible %d",
				raw, hidden.PaneWidth(), visible.PaneWidth())
		}

		rendered := stripTestANSI(m.View().Content)
		for i, line := range strings.Split(strings.TrimRight(rendered, "\n"), "\n") {
			if got := visibleWidth(line); got > m.width {
				t.Fatalf("raw %q: hidden line %d is %d cells, want <= %d", raw, i, got, m.width)
			}
		}

		updated, _ = m.Update(decodeKey(t, raw))
		m = updated.(Model)
		if m.previewHidden {
			t.Fatalf("raw %q: second press should show the preview again", raw)
		}
	}
}

// i, decoded from its raw bytes, restores a hidden preview and shrinks the file
// panes back to their original width.
func TestIToggledFromRawTerminalBytes(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 120, 30

	updated, _ := m.Update(decodeKey(t, "v"))
	m = updated.(Model)
	if !m.previewHidden {
		t.Fatal("setup: v should hide the preview")
	}

	updated, _ = m.Update(decodeKey(t, "i"))
	m = updated.(Model)
	if m.previewHidden {
		t.Fatal("i should show a hidden preview")
	}
	if got := ui.NewLayout(m.width, m.height, m.theme).PaneWidth(); got != 40 {
		t.Fatalf("after i: PaneWidth = %d, want 40", got)
	}
}

// A key release must never drive an action. Terminals that report event types
// (Kitty) or Windows consoles deliver releases too; if those reached the
// dispatch a toggle would fire twice and v would look broken.
func TestKeyReleaseDoesNotDriveActions(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 120, 30

	updated, _ := m.Update(tea.KeyReleaseMsg{Code: 'v', Text: "v"})
	m = updated.(Model)
	if m.previewHidden {
		t.Fatal("a v release must not hide the preview")
	}

	before := m.leftCursor
	updated, _ = m.Update(tea.KeyReleaseMsg{Code: 'j', Text: "j"})
	m = updated.(Model)
	if m.leftCursor != before {
		t.Fatalf("a j release moved the cursor %d -> %d", before, m.leftCursor)
	}
}

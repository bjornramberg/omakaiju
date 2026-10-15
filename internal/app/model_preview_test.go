package app

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

// realistic builds the key event a real terminal produces: Code plus Text.
// The old key('v') helper only sets Code, which is why the toggle looked fine
// in tests while missing spellings in real use. Every test below drives the
// model with both spellings.
func realistic(r rune) tea.KeyMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// v must hide the preview and show it again on the next press, for both the
// synthetic (Code-only) and realistic (Code+Text) spellings, lower and upper.
func TestPreviewToggleHidesAndRestores(t *testing.T) {
	for _, mk := range []struct {
		name string
		key  func(rune) tea.KeyMsg
	}{
		{"code-only", key},
		{"realistic", realistic},
	} {
		m, _ := fixture(t)
		m.width, m.height = 120, 30

		if m.previewHidden {
			t.Fatalf("%s: preview should start visible", mk.name)
		}

		updated, _ := m.Update(mk.key('v'))
		m = updated.(Model)
		if !m.previewHidden {
			t.Fatalf("%s: v should hide the preview", mk.name)
		}

		updated, _ = m.Update(mk.key('v'))
		m = updated.(Model)
		if m.previewHidden {
			t.Fatalf("%s: second v should show the preview again", mk.name)
		}
	}
}

// Shift+V must toggle too, in every spelling terminals produce.
func TestPreviewToggleUppercase(t *testing.T) {
	keys := []tea.KeyMsg{
		tea.KeyPressMsg{Code: 'V', Text: "V"},
		tea.KeyPressMsg{Code: 'v', Text: "V"},
		tea.KeyPressMsg{Code: 'V'},
		tea.KeyPressMsg{Code: 'v', Text: "V", Mod: tea.ModShift},
	}
	for i, k := range keys {
		m, _ := fixture(t)
		m.width, m.height = 120, 30
		updated, _ := m.Update(k)
		m = updated.(Model)
		if !m.previewHidden {
			t.Fatalf("key %d (%q): V should hide the preview like v does", i, k.String())
		}
	}
}

// Ctrl+v must NOT toggle: modified combos keep their meaning.
func TestPreviewToggleIgnoresModifiers(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 120, 30
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'v', Text: "v", Mod: tea.ModCtrl})
	m = updated.(Model)
	if m.previewHidden {
		t.Fatal("ctrl+v must not hide the preview")
	}
}

// i on a hidden preview restores it; i on a visible preview is a no-op that
// changes nothing (no highlight state, no width change, no swallowed keys).
func TestIShowsHiddenPreviewOtherwiseNoop(t *testing.T) {
	for _, mk := range []struct {
		name string
		key  func(rune) tea.KeyMsg
	}{
		{"code-only", key},
		{"realistic", realistic},
	} {
		m, _ := fixture(t)
		m.width, m.height = 120, 30

		// i while visible: no-op.
		updated, _ := m.Update(mk.key('i'))
		m = updated.(Model)
		if m.previewHidden {
			t.Fatalf("%s: i while visible must not hide the preview", mk.name)
		}

		// Hide with v, restore with i.
		updated, _ = m.Update(mk.key('v'))
		m = updated.(Model)
		if !m.previewHidden {
			t.Fatalf("%s: setup: v should hide", mk.name)
		}
		updated, _ = m.Update(mk.key('i'))
		m = updated.(Model)
		if m.previewHidden {
			t.Fatalf("%s: i should unhide a hidden preview", mk.name)
		}

		// Uppercase I restores too.
		updated, _ = m.Update(mk.key('v'))
		m = updated.(Model)
		updated, _ = m.Update(tea.KeyPressMsg{Code: 'I', Text: "I"})
		m = updated.(Model)
		if m.previewHidden {
			t.Fatalf("%s: I should unhide a hidden preview", mk.name)
		}
	}
}

// There is no preview focus mode anymore: pressing i must never swallow the
// next key. j/tab/q work identically immediately after i as without it.
func TestINeverSwallowsKeys(t *testing.T) {
	for _, k := range []tea.KeyMsg{key('i'), realistic('i')} {
		m, _ := fixture(t)
		m.width, m.height = 120, 30

		updated, _ := m.Update(k)
		m = updated.(Model)

		// j still moves.
		before := m.leftCursor
		updated, _ = m.Update(realistic('j'))
		m = updated.(Model)
		if m.leftCursor != before+1 {
			t.Fatalf("key %q: j after i moved %d -> %d, want +1", k.String(), before, m.leftCursor)
		}

		// tab still switches panes.
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		m = updated.(Model)
		if m.activePane != 1 {
			t.Fatalf("key %q: tab after i should switch panes", k.String())
		}

		// q still quits (returns a quit command).
		m2, _ := fixture(t)
		m2.width, m2.height = 120, 30
		updated, _ = m2.Update(k)
		m2 = updated.(Model)
		_, cmd := m2.Update(realistic('q'))
		if cmd == nil {
			t.Fatalf("key %q: q after i should still quit", k.String())
		}
	}
}

// Hiding the preview must hand its width back to the file panes, driven ONLY
// by v: at 120 columns each pane grows from a third (40) to a half (60) of
// the terminal, and to the full width when stacked. Showing again via v
// shrinks them back. i is deliberately absent from this test: it must never
// change pane widths on its own (see TestIShowsHiddenPreviewOtherwiseNoop for
// the restore path). The rendered view must keep fitting the terminal exactly
// in every state, proving the panes take the full estate with no preview
// column left behind.
func TestPreviewToggleExpandsFilePanes(t *testing.T) {
	for _, mk := range []struct {
		name string
		key  func(rune) tea.KeyMsg
	}{
		{"code-only", key},
		{"realistic", realistic},
	} {
		m, _ := fixture(t)
		m.width, m.height = 120, 30

		visible := ui.NewLayout(m.width, m.height, m.theme)
		if got := visible.PreviewWidth(); got != 40 {
			t.Fatalf("%s: visible PreviewWidth = %d, want 40", mk.name, got)
		}
		if got := visible.PaneWidth(); got != 40 {
			t.Fatalf("%s: visible PaneWidth = %d, want 40", mk.name, got)
		}

		checkFits := func() {
			t.Helper()
			out := stripTestANSI(m.View().Content)
			lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
			if len(lines) != m.height {
				t.Fatalf("%s hidden=%v stacked=%v: rendered %d lines, want %d",
					mk.name, m.previewHidden, m.panesStacked, len(lines), m.height)
			}
			for i, l := range lines {
				if got := visibleWidth(l); got > m.width {
					t.Fatalf("%s hidden=%v stacked=%v: line %d is %d cells wide, want <= %d",
						mk.name, m.previewHidden, m.panesStacked, i, got, m.width)
				}
			}
		}

		// i while visible must NOT change pane widths.
		updated, _ := m.Update(mk.key('i'))
		m = updated.(Model)
		afterI := ui.NewLayout(m.width, m.height, m.theme)
		afterI.PreviewHidden = m.previewHidden
		if got := afterI.PaneWidth(); got != 40 {
			t.Fatalf("%s: i while visible changed PaneWidth to %d, want 40", mk.name, got)
		}

		// Hide via v: panes must widen and the view must still fit exactly.
		updated, _ = m.Update(mk.key('v'))
		m = updated.(Model)
		if !m.previewHidden {
			t.Fatalf("%s: v should hide the preview", mk.name)
		}
		hidden := ui.NewLayout(m.width, m.height, m.theme)
		hidden.PreviewHidden = m.previewHidden
		if got := hidden.PreviewWidth(); got != 0 {
			t.Fatalf("%s: hidden PreviewWidth = %d, want 0", mk.name, got)
		}
		if got := hidden.PaneWidth(); got != 60 {
			t.Fatalf("%s: hidden PaneWidth = %d, want 60 (half of 120)", mk.name, got)
		}
		if hidden.PaneWidth() <= visible.PaneWidth() {
			t.Fatalf("%s: hidden PaneWidth = %d, must exceed visible %d",
				mk.name, hidden.PaneWidth(), visible.PaneWidth())
		}
		checkFits()

		// Stacked panes span the whole panes area, so hidden + stacked is
		// the full terminal width.
		m.panesStacked = true
		stackedHidden := ui.NewLayout(m.width, m.height, m.theme)
		stackedHidden.PanesStacked = true
		stackedHidden.PreviewHidden = true
		if got := stackedHidden.PaneWidth(); got != 120 {
			t.Fatalf("%s: stacked hidden PaneWidth = %d, want 120", mk.name, got)
		}
		checkFits()

		// Show again via v: the panes shrink back and the view still fits.
		updated, _ = m.Update(mk.key('v'))
		m = updated.(Model)
		if m.previewHidden {
			t.Fatalf("%s: second v should show the preview again", mk.name)
		}
		shown := ui.NewLayout(m.width, m.height, m.theme)
		shown.PanesStacked = true
		if got := shown.PaneWidth(); got != 80 {
			t.Fatalf("%s: stacked visible PaneWidth = %d, want 80", mk.name, got)
		}
		checkFits()
	}
}

// Restoring a hidden preview via i must shrink the panes back, and the view
// must fit exactly after the restore.
func TestIRestoreShrinksFilePanes(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 120, 30

	updated, _ := m.Update(realistic('v'))
	m = updated.(Model)
	if !m.previewHidden {
		t.Fatal("setup: v should hide the preview")
	}
	hidden := ui.NewLayout(m.width, m.height, m.theme)
	hidden.PreviewHidden = true
	if got := hidden.PaneWidth(); got != 60 {
		t.Fatalf("hidden PaneWidth = %d, want 60", got)
	}

	updated, _ = m.Update(realistic('i'))
	m = updated.(Model)
	if m.previewHidden {
		t.Fatal("i should unhide a hidden preview")
	}
	restored := ui.NewLayout(m.width, m.height, m.theme)
	if got := restored.PaneWidth(); got != 40 {
		t.Fatalf("restored PaneWidth = %d, want 40", got)
	}

	out := stripTestANSI(m.View().Content)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != m.height {
		t.Fatalf("restored view rendered %d lines, want %d", len(lines), m.height)
	}
	for i, l := range lines {
		if got := visibleWidth(l); got > m.width {
			t.Fatalf("restored view line %d is %d cells wide, want <= %d", i, got, m.width)
		}
	}
}

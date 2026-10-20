package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

// keyListModel builds a model over a small populated directory, ready to
// dispatch key presses.
func keyListModel(t *testing.T, w, h int) Model {
	t.Helper()

	dir := t.TempDir()
	for _, name := range []string{"alpha.go", "beta.go", "gamma.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "nonexistent.toml")})
	m.width, m.height = w, h
	m.leftPath, m.rightPath = dir, dir
	m.leftFiles, m.rightFiles = files, files
	return m
}

// Shift+K opens the overlay; the same key closes it again, mirroring the fuzzy panel.
func TestKeyListOpensOnShiftK(t *testing.T) {
	m := keyListModel(t, 120, 40)

	m2, _ := m.Update(shiftKey('k'))
	m = m2.(Model)
	if !m.keyListActive {
		t.Fatal("Shift+K should open the key overlay")
	}

	out := m.View().Content
	if !strings.Contains(out, "key list") {
		t.Error("key overlay should render while open")
	}

	// The same key closes it, and the overlay leaves the view for good.
	m3, _ := m.Update(shiftKey('k'))
	m = m3.(Model)
	if m.keyListActive {
		t.Fatal("second Shift+K should close the overlay")
	}
	if strings.Contains(m.View().Content, "key list") {
		t.Error("overlay should not render after closing")
	}
}

// Typing while the overlay is open runs the search across keys and
// descriptions, arrows move the cursor, and nothing reaches the panes
// underneath — previewing keys does not act on files.
func TestKeyListSearchesAndNavigates(t *testing.T) {
	m := keyListModel(t, 120, 40)

	m2, _ := m.Update(shiftKey('k'))
	m = m2.(Model)

	// Type "copy": the search fills and the view narrows to the copy row.
	for _, r := range []rune{'c', 'o', 'p', 'y'} {
		next, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: r}))
		m = next.(Model)
	}
	if m.keyListQuery != "copy" {
		t.Errorf("search input not captured, got %q", m.keyListQuery)
	}

	// The panes under the overlay must not have lost their cursor or run
	// the keys as actions.
	if m.leftCursor != 0 {
		t.Errorf("cursor should not move while overlay is open, got %d", m.leftCursor)
	}
	if m.clipboard.Files != nil {
		t.Error("yank key must be captured as search text, not applied")
	}
	if m.mode != inputNone {
		t.Errorf("prompt modes must stay off, got input mode %d", m.mode)
	}

	view := plainView.ReplaceAllString(m.View().Content, "")
	if !strings.Contains(view, "copy (yank)") {
		t.Error("copy row should show for the copy search")
	}
	if strings.Contains(view, "switch pane") {
		t.Error("unrelated rows should be filtered away")
	}

	// Backspace edits the query back off again.
	next, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	m = next.(Model)
	if m.keyListQuery != "cop" {
		t.Errorf("backspace broke the query, got %q", m.keyListQuery)
	}

	// esc closes and the pane dispatch resumes right after.
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = next.(Model)
	if m.keyListActive {
		t.Fatal("esc should close the overlay")
	}
	next, _ = m.Update(arrow(tea.KeyDown))
	m = next.(Model)
	if m.leftCursor != 1 {
		t.Errorf("navigation should resume after close, cursor %d", m.leftCursor)
	}
}

// Arrows move the highlight across the full list, clamped at both ends.
func TestKeyListArrowsNavigateBeforeFiltering(t *testing.T) {
	m := keyListModel(t, 120, 40)

	m2, _ := m.Update(shiftKey('k'))
	m = m2.(Model)

	next, _ := m.Update(arrow(tea.KeyDown))
	m = next.(Model)
	if m.keyListCursor != 1 {
		t.Errorf("down should move the cursor on the full list, got %d", m.keyListCursor)
	}
	next, _ = m.Update(arrow(tea.KeyUp))
	m = next.(Model)
	next, _ = m.Update(arrow(tea.KeyUp))
	m = next.(Model)
	if m.keyListCursor != 0 {
		t.Errorf("up should clamp at the top, got %d", m.keyListCursor)
	}
}

// Description matches: a search for "pane" surfaces the pane-related
// bindings, and their rows stay visible in the rendered panel.
func TestKeyListSearchesDescriptions(t *testing.T) {
	m := keyListModel(t, 120, 40)

	m2, _ := m.Update(shiftKey('k'))
	m = m2.(Model)
	for _, r := range []rune{'p', 'a', 'n', 'e'} {
		next, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: r}))
		m = next.(Model)
	}

	view := stripKey(m.View().Content)
	if !strings.Contains(view, "switch pane") || !strings.Contains(view, "stack panes") {
		t.Errorf("pane rows missing from the view, got %q", view)
	}
	if strings.Contains(view, "rename") {
		t.Errorf("unrelated rows should be filtered away, got %q", view)
	}
}

// q is no longer a global quit while the panel is up: it falls into the
// search. Only esc and Shift+K close the overlay.
func TestKeyListQGoesToSearch(t *testing.T) {
	m := keyListModel(t, 120, 40)

	m2, _ := m.Update(shiftKey('k'))
	m = m2.(Model)
	next, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: 'q'}))
	m = next.(Model)
	if !m.keyListActive {
		t.Fatal("q should feed the search, not close the overlay")
	}
	if m.keyListQuery != "q" {
		t.Errorf("q should be in the query, got %q", m.keyListQuery)
	}

	// A real quit key is absorbed while the overlay pins the screen: the
	// user has to get out of the panel first.
	if strings.Contains(stripKey(m.View().Content), "bye") {
		t.Error("q must not quit the app while the overlay is up")
	}

	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = next.(Model)
	if m.keyListActive {
		t.Fatal("esc should close the overlay")
	}
}

func stripKey(s string) string {
	return plainView.ReplaceAllString(s, "")
}
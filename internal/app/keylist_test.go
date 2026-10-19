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

// ctrl+k lives on the ordinary nav dispatch path, so it opens the overlay;
// the same key closes it again, mirroring the fuzzy panel.
func TestKeyListOpensOnCtrlK(t *testing.T) {
	m := keyListModel(t, 120, 40)

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	m = m2.(Model)
	if !m.keyListActive {
		t.Fatal("ctrl+k should open the key overlay")
	}

	out := m.View().Content
	if !strings.Contains(out, "key list") {
		t.Error("key overlay should render while open")
	}

	// The same key closes it, and the overlay leaves the view for good.
	m3, _ := m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	m = m3.(Model)
	if m.keyListActive {
		t.Fatal("second ctrl+k should close the overlay")
	}
	if strings.Contains(m.View().Content, "key list") {
		t.Error("overlay should not render after closing")
	}
}

// While the overlay is up every dispatch key is absorbed: nothing behind the
// user's back may move. Absorbed keys are freed once it closes.
func TestKeyListAbsorbsKeysWhileOpen(t *testing.T) {
	m := keyListModel(t, 120, 40)

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	m = m2.(Model)
	if !m.keyListActive {
		t.Fatal("overlay should open")
	}

	// Nav and action keys must not reach the panes.
	for _, msg := range []tea.KeyMsg{
		tea.KeyPressMsg{Code: tea.KeyDown},
		tea.KeyPressMsg{Code: 'y'},
		tea.KeyPressMsg{Code: 'a'},
	} {
		next, _ := m.Update(msg)
		m = next.(Model)
	}

	if m.keyListActive != true {
		t.Fatal("ordinary keys must not close the overlay")
	}
	if m.leftCursor != 0 {
		t.Errorf("cursor should not move while overlay is open, got %d", m.leftCursor)
	}
	if m.clipboard.Files != nil {
		t.Error("yank key must be absorbed, not applied")
	}
	if m.mode != inputNone {
		t.Errorf("action keys must be absorbed, got input mode %d", m.mode)
	}

	// Closing with esc unblocks dispatch: the key reaching the pane after
	// the escape must actually take effect.
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.keyListActive {
		t.Fatal("esc should close the overlay")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(Model)
	if m.leftCursor != 1 {
		t.Errorf("navigation should resume after close, cursor %d", m.leftCursor)
	}
}

// q closes the key overlay like esc; no other key may dismiss it.
func TestKeyListClosesOnQNotOnAction(t *testing.T) {
	m := keyListModel(t, 120, 40)

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	m = m2.(Model)
	next, _ := m.Update(tea.KeyPressMsg{Code: 'l'})
	m = next.(Model)
	if !m.keyListActive {
		t.Fatal("an action key pushed through while open should be absorbed, not close")
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: 'q'})
	m = next.(Model)
	if m.keyListActive {
		t.Fatal("q should close the overlay")
	}
}

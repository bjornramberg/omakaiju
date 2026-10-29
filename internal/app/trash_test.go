package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

// trashModel builds a model whose trash is a directory of its own, so a test
// never looks at (or empties) the real user's trash.
func trashModel(t *testing.T) (Model, string) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	m, dir := fixture(t)
	m.width, m.height = 120, 40
	return m, dir
}

// trashOne moves a file from dir into the home trash.
func trashOne(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Trash(path); err != nil {
		t.Fatalf("trashing %s: %v", name, err)
	}
}

// assertQuits runs the command and checks that it ends the session.
func assertQuits(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected the app to quit, got no command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("expected a quit message, got nil")
	} else if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected a quit message, got %T", msg)
	}
}

// Shift+T opens the trash in the active pane: once the listing it asks for
// lands, the pane's path is the trash directory.
func TestShiftTOpensTheTrashInTheActivePane(t *testing.T) {
	m, dir := trashModel(t)
	trashOne(t, dir, "alpha.txt")

	updated, cmd := m.Update(shiftKey('t'))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("shift+t should load the trash")
	}

	root, err := fs.TrashRoot()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.targetPath(m.activePane); got != root {
		t.Fatalf("active pane heading to %q, want the trash %q", got, root)
	}

	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if got := m.rawPath(m.activePane); got != root {
		t.Errorf("active pane is at %q, want the trash %q", got, root)
	}
}

// With no trash yet, Shift+T says so instead of navigating to a directory
// that does not exist.
func TestShiftTWithoutATrashSaysSo(t *testing.T) {
	m, _ := trashModel(t)
	before := m.rawPath(m.activePane)

	updated, cmd := m.Update(shiftKey('t'))
	m = updated.(Model)
	if cmd != nil {
		t.Error("shift+t should not load a trash that does not exist")
	}
	if got := m.rawPath(m.activePane); got != before {
		t.Errorf("pane moved to %q without a trash to show", got)
	}
	view := plainView.ReplaceAllString(m.View().Content, "")
	if !strings.Contains(view, "no trash yet") {
		t.Errorf("the bottom bar should say there is no trash, got:\n%s", view)
	}
}

// Quitting with a non-empty trash does not quit at once: the question is
// asked and the session stays up until it is answered.
func TestQuitWithANonEmptyTrashAsksFirst(t *testing.T) {
	m, dir := trashModel(t)
	trashOne(t, dir, "alpha.txt")

	updated, cmd := m.Update(key('q'))
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("the app quit before the trash question was answered")
	}
	if !m.confirmEmptyTrash {
		t.Fatal("quitting with a non-empty trash should ask about emptying it")
	}

	view := plainView.ReplaceAllString(m.View().Content, "")
	if !strings.Contains(view, "empty it") {
		t.Errorf("the question should be on screen, got:\n%s", view)
	}
}

// Answering yes empties the home trash and then quits.
func TestQuitAnsweringYesEmptiesTheTrash(t *testing.T) {
	m, dir := trashModel(t)
	trashOne(t, dir, "alpha.txt")

	updated, _ := m.Update(key('q'))
	m = updated.(Model)

	updated, cmd := m.Update(key('y'))
	m = updated.(Model)
	assertQuits(t, cmd)
	if m.confirmEmptyTrash {
		t.Error("the question should be closed after the answer")
	}

	entries, err := fs.ListTrash()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("trash still holds %d entry/entries after answering yes", len(entries))
	}
}

// Answering no, or pressing Esc, quits and leaves the trash exactly as it
// was.
func TestQuitAnsweringNoLeavesTheTrashAlone(t *testing.T) {
	answers := []struct {
		name string
		msg  tea.KeyMsg
	}{
		{"n", key('n')},
		{"esc", tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})},
	}

	for _, answer := range answers {
		t.Run(answer.name, func(t *testing.T) {
			m, dir := trashModel(t)
			trashOne(t, dir, "alpha.txt")

			updated, _ := m.Update(key('q'))
			m = updated.(Model)

			updated, cmd := m.Update(answer.msg)
			m = updated.(Model)
			assertQuits(t, cmd)
			if m.confirmEmptyTrash {
				t.Error("the question was not dismissed by the answer")
			}

			entries, err := fs.ListTrash()
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Errorf("trash holds %d entry/entries, want the one that was left alone", len(entries))
			}
		})
	}
}

// With nothing in the trash, quitting asks nothing and quits at once.
func TestQuitWithAnEmptyTrashAsksNothing(t *testing.T) {
	emptyTrash := []struct {
		name string
		make func(t *testing.T)
	}{
		{"no trash directory", func(t *testing.T) {}},
		{"empty trash directory", func(t *testing.T) {
			root, err := fs.TrashRoot()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, "info"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, "files"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	}

	for _, tc := range emptyTrash {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := trashModel(t)
			tc.make(t)

			updated, cmd := m.Update(key('q'))
			m = updated.(Model)
			assertQuits(t, cmd)
			if m.confirmEmptyTrash {
				t.Error("an empty trash should not be asked about")
			}
		})
	}
}

// A trash that cannot be emptied is reported, and the quit the user asked for
// still happens.
func TestQuitReportsAFailedEmptyAndStillQuits(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", base)
	m, _ := fixture(t)
	m.width, m.height = 120, 40

	// A trash whose files directory is a symlink is refused by fs.EmptyTrash.
	// The info file still makes the entry listable, so the question is asked
	// and the answer reaches the failing empty.
	root := filepath.Join(base, "Trash")
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "info"), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(outside, "doomed.txt")
	if err := os.WriteFile(payload, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "files")); err != nil {
		t.Fatal(err)
	}
	info := "[Trash Info]\nPath=" + payload + "\nDeletionDate=2026-01-02T03:04:05\n"
	if err := os.WriteFile(filepath.Join(root, "info", "doomed.txt.trashinfo"), []byte(info), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := fs.ListTrash()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("test setup: ListTrash returned %d entries, want 1", len(entries))
	}

	updated, _ := m.Update(key('q'))
	m = updated.(Model)
	if !m.confirmEmptyTrash {
		t.Fatal("the question should be asked when the trash is not empty")
	}

	updated, cmd := m.Update(key('y'))
	m = updated.(Model)
	assertQuits(t, cmd)
	if m.opErr == nil {
		t.Error("a trash that cannot be emptied must be reported")
	} else if !strings.Contains(m.opErr.Error(), "empty trash") {
		t.Errorf("the report should name the failed empty, got %q", m.opErr)
	}
}

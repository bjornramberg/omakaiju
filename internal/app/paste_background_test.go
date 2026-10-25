package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

// A paste must return to the event loop before it measures the tree. The
// estimate walks and stats every entry, and doing that inside Update froze the
// UI before the progress notice could be drawn.
func TestPasteEstimatesSizeInTheCommand(t *testing.T) {
	m, _ := fixture(t)
	dst := t.TempDir()
	dstFiles, err := fs.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	m.rightPath, m.rightFiles = dst, dstFiles

	m.leftCursor = indexOf(m.visibleFiles(0), "alpha.txt")
	updated, _ := m.Update(shiftKey('c'))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)

	// Observe the estimate instead of timing it. If Update walked the tree,
	// the flag is set before it returns; if the walk lives in the command, it
	// is only set when the command runs.
	original := pasteSizeWalk
	defer func() { pasteSizeWalk = original }()
	walked := false
	pasteSizeWalk = func(files []fs.Entry) int64 {
		walked = true
		return 0
	}

	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)

	if walked {
		t.Fatal("Update ran the paste size walk on the event loop")
	}
	if cmd == nil {
		t.Fatal("paste should dispatch a command")
	}
	// The user is told the work is starting while the command is pending.
	if want := "copying 1 file(s)..."; m.opResult != want {
		t.Errorf("opResult = %q, want %q", m.opResult, want)
	}

	msg := cmd()
	if !walked {
		t.Fatal("the paste command did not measure the selection")
	}
	result, ok := msg.(fileOpMsg)
	if !ok {
		t.Fatalf("a small paste returned %T, want fileOpMsg", msg)
	}
	if len(result.failures) != 0 {
		t.Fatalf("paste failed: %s", result.result())
	}
	if _, err := os.Stat(filepath.Join(dst, "alpha.txt")); err != nil {
		t.Errorf("the paste did not land: %v", err)
	}
}

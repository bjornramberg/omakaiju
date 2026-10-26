package app

import (
	"os"
	"path/filepath"

	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

// finishCopy releases the copier and restores normal operation.
//
// The generation is bumped so a message still in flight from the copy that
// just ended is recognised as stale and dropped rather than reviving the
// copier.
func (m *Model) finishCopy() {
	m.copier = nil
	m.copyFiles = 0
	m.copyClipboard = nil
	m.copyMarks = nil
	m.copySkipped = 0
	m.copyGen++
}

// cancelCopy aborts an in-flight copy or move.
//
// The cancel signals the abort at once and leaves the part of the cleanup that
// can wait to a goroutine. Copier.Close waits for a step that is still in
// flight, and a step can be stuck in a read or a sync on a stalled network
// filesystem; waiting for it here — this runs on the event loop — would freeze
// the whole UI until the filesystem returned.
//
// The cleanup still finishes. Abort is non-blocking: it sets the flag, and an
// in-flight step unwinds when it observes it. The goroutine's Close then waits
// for that step, so the scratch file is removed, the directories the copy made
// are taken away and the handles are closed, just later than the keypress.
//
// The goroutine captures the copier and nothing else. Model is copied by value
// on every update, so a write to a model copy from here would race the app and
// change nothing the user sees; the teardown touches only the copier.
//
// Because nothing landed, the selection the transfer consumed is put back.
// For a move, an entry is restored only if its source is still on disk: one
// that was already renamed away did move, and must not come back.
func (m Model) cancelCopy() (tea.Model, tea.Cmd) {
	if m.copier == nil {
		return m, nil
	}
	copier := m.copier
	verb := "copy"
	if copier.IsMove() {
		verb = "move"
	}

	marks := m.copyMarks
	clipboard := m.copyClipboard

	// Signal the abort before returning to the loop, so the step stops as
	// soon as it looks at the flag rather than copying another chunk first.
	_ = copier.Abort()
	go func() {
		// A goroutine the app owns sits outside Bubble Tea's panic net, so
		// a panic in it would kill the process with the terminal still in
		// raw mode. There is no UI to report it to from here; the recover
		// keeps a teardown bug from taking the session with it.
		defer func() { _ = recover() }()
		_ = copier.Close()
	}()
	m.finishCopy()

	if len(marks) > 0 {
		pending := make([]string, 0, len(marks))
		for path := range marks {
			if _, err := os.Stat(path); err == nil {
				pending = append(pending, path)
			}
		}
		m.restoreMarks(marks, pending)
	}
	m.restoreClipboard(clipboard)

	m.opResult = verb + " cancelled"
	return m, m.reloadPanesCmd()
}

// copySizeWalkLimit bounds how many entries the pre-copy size walk visits, so
// starting a paste cannot stall on an enormous tree. It mirrors the finder's
// limit for the same reason: the estimate only decides which copy path to use,
// and erring toward the simple path is cheaper than blocking the UI.
const copySizeWalkLimit = 200000

// pasteSizeWalk is the pre-paste size estimate. It is a variable so a test
// can observe that the walk runs inside the paste command rather than on the
// event loop.
var pasteSizeWalk = copyTotalBytes

// copyTotalBytes estimates how many bytes a paste will move, so the copy path
// can decide whether the work deserves a progress bar.
//
// A directory entry's Size is the inode's own size — a few kilobytes — not the
// payload inside it. Summing that made a multi-gigabyte folder look tiny and
// sent it down the blocking path with no progress at all. Directories are
// therefore walked; files still just contribute their recorded size.
//
// The walk is disk work and must not run in Update: it stats every entry up to
// copySizeWalkLimit, and on a large tree that would freeze the UI before the
// progress notice could even be drawn. pasteCopyCmd calls it from the command
// goroutine, where the other disk work happens.
func copyTotalBytes(files []fs.Entry) int64 {
	var total int64
	budget := copySizeWalkLimit
	for _, f := range files {
		if !f.IsDir {
			total += f.Size
			continue
		}
		total += dirContentSize(f.Path, &budget)
	}
	return total
}

// dirContentSize sums the files beneath root. budget is shared across every
// directory in the batch; once it is exhausted the walk stops. A truncated
// walk can only understate the size, which at worst keeps a copy on the simple
// path rather than stalling the key press.
func dirContentSize(root string, budget *int) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if *budget <= 0 {
			return filepath.SkipAll
		}
		*budget--
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// pasteCopyCmd estimates the paste and performs it, all in the background.
//
// The size walk decides whether the selection gets the steppable copier (and
// its progress bar) or the simple blocking path. Doing that walk in Update
// froze the whole UI on a large folder before the progress notice could be
// shown; here it happens where the copy itself happens.
func pasteCopyCmd(files []fs.Entry, dstDir string, gen int, clipboard *Clipboard) tea.Cmd {
	return safeCmd("paste-copy", func() tea.Msg {
		if pasteSizeWalk(files) >= copyProgressThreshold {
			srcs := make([]string, 0, len(files))
			for _, f := range files {
				srcs = append(srcs, f.Path)
			}
			return startCopyCmd(srcs, dstDir, gen, clipboard)()
		}

		msg := fileOpMsg{verb: "pasted", clipboard: clipboard}
		for _, f := range files {
			dst := filepath.Join(dstDir, f.Name)
			if filepath.Clean(dst) == filepath.Clean(f.Path) {
				continue
			}
			if err := fs.CopyRecursive(f.Path, dst); err != nil {
				msg.failures = append(msg.failures, opFailure{name: f.Name, path: f.Path, err: err})
				continue
			}
			msg.succeeded++
		}
		return msg
	})
}

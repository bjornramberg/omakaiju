package app

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

func loadDirCmd(pane int, path string) tea.Cmd {
	return safeCmd("load-dir", func() tea.Msg {
		files, err := fs.ReadDir(path)
		return dirLoadedMsg{pane: pane, path: path, files: files, err: err}
	})
}

// batchOpCmd applies one operation to every item in a single command and
// reports one aggregate result. Running the whole batch here rather than
// dispatching one command per entry is what keeps the outcome independent of
// completion order: there is exactly one message to apply.
//
// marks and clipboard are the selection the batch consumes. They ride along on
// the result so a failure can restore what the user would otherwise have to
// select again.
func batchOpCmd(verb string, items []opItem, skipped int, marks map[string]fs.Entry, clipboard *Clipboard, op func(src, dst string) error) tea.Cmd {
	return safeCmd(verb, func() tea.Msg {
		msg := fileOpMsg{verb: verb, skipped: skipped, marks: marks, clipboard: clipboard}
		for _, item := range items {
			if err := op(item.src, item.dst); err != nil {
				msg.failures = append(msg.failures, opFailure{name: item.name, path: item.src, err: err})
				continue
			}
			msg.succeeded++
		}
		return msg
	})
}

// singleOpCmd runs one path-based operation and reports it as a one-entry
// result. The name is reported even when the operation fails, so the user can
// tell which file was involved.
func singleOpCmd(verb, name, path string, op func(string) error) tea.Cmd {
	return safeCmd(verb, func() tea.Msg {
		return singularResult(verb, name, op(path))
	})
}

// copyProgressThreshold is the size below which a copy runs synchronously.
// Showing a bar for a handful of bytes only produces a one-frame flash, so
// small copies keep the simple path and just report a result.
const copyProgressThreshold = 1 << 20

// startCopyCmd builds the copier and performs the first step. clipboard is the
// paste selection the copy consumed, restored if the job cannot start.
func startCopyCmd(srcs []string, dstDir string, gen int, clipboard *Clipboard) tea.Cmd {
	return safeCmd("copy-start", func() tea.Msg {
		copier, err := fs.NewCopier(srcs, dstDir)
		if err != nil {
			// Nothing was copied. Name each source so the failure reads
			// like any other aggregate result.
			msg := fileOpMsg{verb: "copied", clipboard: clipboard}
			for _, src := range srcs {
				msg.failures = append(msg.failures, opFailure{name: filepath.Base(src), path: src, err: err})
			}
			return msg
		}
		return copyProgressMsg{copier: copier, gen: gen}
	})
}

// startMoveCmd builds the mover and performs the first step.
//
// A move within one filesystem is a rename, and it costs nothing. A move
// onto another filesystem — a share, say — cannot be a rename, so the entry
// is copied across, and that copy has to travel. It therefore runs through
// the same steppable machinery as a copy, and gets a progress bar and a
// cancel of its own.
//
// The marks ride along on the result so a move that fails can put the
// selection back the way it was.
func startMoveCmd(srcs []string, dstDir string, gen int, marks map[string]fs.Entry, skipped int) tea.Cmd {
	return safeCmd("move-start", func() tea.Msg {
		copier, err := fs.NewMover(srcs, dstDir)
		if err != nil {
			msg := fileOpMsg{verb: "moved", skipped: skipped, marks: marks}
			for _, src := range srcs {
				msg.failures = append(msg.failures, opFailure{name: filepath.Base(src), path: src, err: err})
			}
			return msg
		}
		// A move that had nothing to copy is already finished, so it reports
		// straight away rather than flashing a progress bar for a frame.
		//
		// A rename-only move is that case even though Pending() is not zero:
		// its renames are outstanding work, but no bytes have to travel, so
		// the move is driven to completion here instead of handing a job with
		// nothing to draw to the progress loop. Every step runs until the
		// mover says it is done, and a step that fails is reported the same
		// way the progress path reports one.
		if copier.CopyPending() == 0 && copier.Pending() > 0 {
			for {
				// Capture the entry before Step runs: a failed Step drops the
				// copier's pointer to the current job.
				name := copier.CurrentName()
				path := copier.CurrentSource()
				done, stepErr := copier.Step()
				if stepErr != nil {
					return copyProgressMsg{
						copier: copier,
						gen:    gen,
						move:   true,
						err:    stepErr,
						name:   name,
						path:   path,
					}
				}
				if done {
					return copyProgressMsg{copier: copier, gen: gen, move: true, done: true}
				}
			}
		}
		return copyProgressMsg{copier: copier, gen: gen, move: true, done: copier.Pending() == 0}
	})
}

// stepCopyCmd advances the copier by one chunk.
func stepCopyCmd(copier *fs.Copier, gen int) tea.Cmd {
	return safeCmd("copy-step", func() tea.Msg {
		// A nil copier would dereference into a panic. It cannot happen
		// today, but the guard costs nothing and turns a future mistake
		// into a reported error instead of a dead session.
		if copier == nil {
			return singularResult("copied", "copy", errors.New("no copier in flight"))
		}
		// Capture the file before Step runs: a failed Step discards the
		// copier's pointer to the current job, which is exactly the file
		// the user needs named in the error.
		name := copier.CurrentName()
		path := copier.CurrentSource()
		done, err := copier.Step()
		return copyProgressMsg{copier: copier, done: done, err: err, name: name, path: path, gen: gen, move: copier.IsMove()}
	})
}

func deleteCmd(path string) tea.Cmd {
	return singleOpCmd("deleted", filepath.Base(path), path, fs.Delete)
}

func openCmd(path, name string) tea.Cmd {
	return safeCmd("open", func() tea.Msg {
		return openMsg{name: name, err: fs.OpenFile(path)}
	})
}

// trashCmd moves a path into the trash. Trash reports the payload's new
// location alongside an error: once the rename has happened, a failure to
// write the .trashinfo record leaves the file in the trash with nothing
// pointing back to where it came from. That destination must be shown, or the
// user is told the delete failed while the file is no longer where it was.
func trashCmd(path string) tea.Cmd {
	return safeCmd("trash", func() tea.Msg {
		name := filepath.Base(path)
		newPath, err := fs.Trash(path)
		if err == nil {
			return fileOpMsg{verb: "trashed", name: name, succeeded: 1}
		}

		reason := readableOpError(err)
		if newPath == "" {
			return fileOpMsg{
				verb:     "trashed",
				failures: []opFailure{{name: name, path: path, err: err}},
				text:     fmt.Sprintf("trash failed: %s did not move (%s)", name, reason),
			}
		}
		return fileOpMsg{
			verb:     "trashed",
			failures: []opFailure{{name: name, path: path, err: err}},
			text:     fmt.Sprintf("trashed %s to %s (write failed: %s)", name, newPath, reason),
		}
	})
}

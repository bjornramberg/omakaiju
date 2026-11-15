package app

import (
	"path/filepath"

	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

// dragState is a drag in progress: the pane it started in, the row it started
// on, and the entry under the pointer.
//
// The pointer is held down and moving; without this the app would have no way
// of telling a drag from a click that ended somewhere else.
type dragState struct {
	// pane is the pane the drag started in.
	pane int
	// row is the row the drag started on.
	row int
	// entry is the entry under the pointer when the drag started. It is the
	// file the drop will act on.
	entry fs.Entry
	// target is the pane the pointer is over now, or -1 when it is over
	// nothing that can receive a drop.
	target int
	// copy says the drop would copy rather than move.
	copy bool
}

// dragMotion follows the pointer while a drag is in progress and says which
// pane would receive the drop.
//
// The motion is only reported while a button is down, so a motion with no drag
// in progress is not a drag at all. The cursor is deliberately left alone: it
// belongs to the pane the drag started in and has to stay on the row it started
// on, so the release can still be understood. A motion over the other pane
// therefore resolves its rows for the gesture without selecting anything there.
func (m Model) dragMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	if m.drag == nil {
		return m, nil
	}
	layout := m.layout()
	hit, ok := layout.HitPane(m.paneViews(layout), msg.X, msg.Y)
	target := -1
	// Only the other pane can receive the drop: releasing over the pane the
	// drag came from would copy or move an entry onto itself, which is a
	// no-op the UI should not offer.
	if ok && hit.Pane != m.drag.pane {
		target = hit.Pane
	}

	// The state is replaced rather than written through the pointer. Model is
	// copied by value on every update, but a pointer is shared with the state
	// Update hands back after a panic in a handler, so a write in place would
	// leak a half-finished drag out of the update that failed.
	next := *m.drag
	next.target = target
	// The modifier is tracked here as well as on the release, so the
	// indicator says "copy" the moment the user presses Ctrl instead of only
	// when the button comes up.
	next.copy = copyModifier(msg.Mouse().Mod)
	m.drag = &next
	return m, nil
}

// dragRelease ends a drag. A release over the other pane moves (or copies) the
// entry into that pane's directory; a release anywhere else cancels the drag
// and leaves the click's own work standing.
func (m Model) dragRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	drag := m.drag
	m.drag = nil
	if drag == nil {
		return m, nil
	}

	// The release position is resolved again instead of trusting the last
	// motion: the pointer can travel a cell between the final motion the
	// terminal reported and the button coming up, and the position the user
	// let go at is the one they meant. The motion's target is the fallback for
	// a release that lands where the hit-test has no answer — a cell past the
	// pane's edge, say — so the pane the pointer was last reported over still
	// receives the drop.
	layout := m.layout()
	hit, ok := layout.HitPane(m.paneViews(layout), msg.X, msg.Y)
	target := drag.target
	if ok && hit.Pane != drag.pane {
		target = hit.Pane
	}
	if target < 0 {
		return m, nil
	}

	// The release is the event that decides what the drop does, so the
	// modifier is read from it: Ctrl can be pressed or let go between the last
	// motion the terminal reported and the button coming up.
	drop := *drag
	drop.target = target
	drop.copy = copyModifier(msg.Mouse().Mod)
	return m.dropEntry(&drop)
}

// dropEntry moves the dragged entry into the target pane's directory, or copies
// it when the drop was made with a copy modifier.
//
// The machinery is the one the paste already uses: a move goes through the
// steppable mover, so it gets a progress bar and a cancel, and a copy goes
// through the copier. The target is the pane under the pointer, not the active
// pane, so the pane the user dropped into is the one that changes. The
// operation's own result is what reloads the panes, which is why the drop only
// has to start it.
func (m Model) dropEntry(drag *dragState) (tea.Model, tea.Cmd) {
	// One transfer at a time, for the same reason the paste refuses to start a
	// second one: the copier is a single field on the model, and a transfer
	// that is replaced loses the messages that would have closed it. Its
	// scratch files and handles would then be left for the process to reclaim
	// instead of being cleaned up by the cancel or the finish.
	if m.copier != nil {
		what := "copy"
		if m.copier.IsMove() {
			what = "move"
		}
		m.opResult = what + " in progress"
		return m, nil
	}

	dstDir := m.rawPath(drag.target)
	dst := filepath.Join(dstDir, drag.entry.Name)
	if filepath.Clean(dst) == filepath.Clean(drag.entry.Path) {
		// Both panes are showing the same directory, so the entry is already
		// where a drop would put it. Saying so is better than starting an
		// operation that can only refuse itself.
		m.opResult = "already in this directory"
		return m, nil
	}

	// The entry lands in the pane it was dropped on, so that is where the
	// reload that follows the operation has to put the cursor. Without this
	// the pane the user dropped into would jump back to the top of its
	// listing the way it does when a copy is started from the keyboard.
	m.restoreLanded(drag.target, []string{drag.entry.Name})

	if drag.copy {
		// A drag is not a paste: no selection was consumed, so there is no
		// clipboard to re-arm if the copy fails. The entry is still in the
		// pane it was dragged from, and the same drag can simply be made
		// again.
		//
		// The file count is what a finished copy reports in the bottom bar.
		// Nothing else sets it for a drag, so without this the copy would end
		// with "copied 0 file(s)".
		m.copyFiles = 1
		m.copyGen++
		m.opResult = "copying " + drag.entry.Name + "..."
		return m, startCopyCmd([]string{drag.entry.Path}, dstDir, m.copyGen, nil)
	}

	// The dragged entry's own mark goes with it, because the entry is leaving
	// the directory the mark was made in and a mark on a path that is no
	// longer there is a mark the user cannot act on. Every other mark stays
	// where it is, so a bulk move staged before the drag is not disturbed by
	// it. The same snapshot rides along on the result, so a move that fails
	// can put back what did not go.
	marks := m.marked
	if _, marked := marks[drag.entry.Path]; marked {
		next := make(map[string]fs.Entry, len(marks))
		for path, entry := range marks {
			if path == drag.entry.Path {
				continue
			}
			next[path] = entry
		}
		marks = next
		m.marked = next
	}

	m.copyMarks = marks
	m.copySkipped = 0
	m.copyGen++
	m.opResult = "moving " + drag.entry.Name + "..."
	return m, startMoveCmd([]string{drag.entry.Path}, dstDir, m.copyGen, marks, 0)
}

// copyModifier reports whether the modifier that turns a drop into a copy is
// held.
//
// Ctrl is the modifier the desktop file managers use for it, and Alt is
// accepted as well: a terminal can take Ctrl for its own selection, and a user
// who cannot make Ctrl reach the application still needs a way to copy.
func copyModifier(mod tea.KeyMod) bool {
	return mod&tea.ModCtrl != 0 || mod&tea.ModAlt != 0
}

// dropStateFor says what a release over the pane would do right now.
//
// It is read from the drag rather than from the pointer's position because the
// drag already resolved the position against the panes, and the indicator has
// to agree with what the release would actually do: the pane the pointer is
// over is a target, the pane the drag came from never is, and with no drag
// nobody is a target.
func (m Model) dropStateFor(pane int) ui.DropState {
	if m.drag == nil {
		return ui.DropNone
	}
	if m.drag.target != pane {
		return ui.DropNone
	}
	if m.drag.copy {
		return ui.DropCopy
	}
	return ui.DropMove
}

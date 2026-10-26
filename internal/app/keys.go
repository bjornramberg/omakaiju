package app

import (
	"path/filepath"
	"unicode"

	"github.com/bjornramberg/omakaiju/internal/nav"

	tea "charm.land/bubbletea/v2"
)

// keyID is the canonical identity of a key press: the modifiers and the base
// key, e.g. "shift+d", "ctrl+r", "esc".
//
// It is not msg.String(). A terminal delivers a shifted letter as the
// printable character ("D"), so String() hides the shift that the binding
// table is written in, and a lookup for "shift+d" never matches. Keystroke()
// keeps the modifier and reports the base key, so the identity is the same
// whichever way the terminal delivered it.
//
// The text paths keep using String(): a prompt that takes a name has to
// receive the character that was typed, not the key that produced it.
func keyID(msg tea.KeyMsg) string {
	return msg.Key().Keystroke()
}

// printableRune returns the single printable rune a key press carries, and
// whether it carried one at all.
//
// The check is rune-based on purpose. A byte-length test rejects every
// non-ASCII character, and filenames are full of them: a file manager that
// cannot type "café" into its own filter is not much use.
func printableRune(key string) (rune, bool) {
	runes := []rune(key)
	if len(runes) != 1 {
		return 0, false
	}
	if !unicode.IsPrint(runes[0]) {
		return 0, false
	}
	return runes[0], true
}

// printableKeyRune returns the printable rune a key event carries, however the
// terminal chose to describe it, and whether it carried one at all.
//
// The text the terminal delivered comes first: that is the character the user
// typed. The String() path stays as the fallback for events that carry no
// Text, so every key that already worked keeps working. The space bar needs
// the separate check because Key.String hides its printable character behind
// the key's name: it reports "space", not " ", and a prompt that only looked
// at the string silently dropped every space.
func printableKeyRune(msg tea.KeyMsg) (rune, bool) {
	if r, ok := printableRune(msg.Key().Text); ok {
		return r, true
	}
	if r, ok := printableRune(msg.String()); ok {
		return r, true
	}
	if keyID(msg) == "space" {
		return ' ', true
	}
	return 0, false
}

// handleKey dispatches a key press that does not belong to an overlay,
// input prompt, or confirmation. It is the normal-mode key path.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A status message reports what just happened, so it stops being
	// current once the user acts. Clearing ahead of dispatching the key
	// means a message produced by this very keypress stays visible until
	// the next one.
	m.opResult = ""
	m.loadErr = nil
	m.opErr = nil
	m.crashErr = nil

	// The same applies to a pending cursor placement: an operation that
	// has not landed yet must not yank the cursor back to the entry it
	// named once the user has already chosen a different one. The keys
	// that set a restore of their own set it after this, so it survives.
	m.clearRestores()

	if m.confirmDelete {
		return m.handleConfirmDelete(msg)
	}

	if m.fuzzyActive {
		return m.handleFuzzyKeys(msg)
	}

	if m.keyListActive {
		return m.handleKeyListKeys(msg)
	}

	if m.mode != inputNone {
		return m.handleInputKeys(msg)
	}

	// An in-flight copy claims esc, but only once the modal prompts above
	// have had their chance.
	if m.copier != nil {
		if keyID(msg) == "esc" {
			return m.cancelCopy()
		}
		return m, nil
	}

	keyStr := keyID(msg)

	// Handle jump mode: Space was pressed, now waiting for a letter
	if m.jumpActive {
		m.jumpActive = false
		if len(keyStr) == 1 && keyStr[0] >= 'a' && keyStr[0] <= 'z' {
			return m.handleJump(rune(keyStr[0]))
		}
		return m, nil
	}

	// Handle Space to activate jump mode
	if keyStr == "space" {
		m.jumpActive = true
		return m, nil
	}

	// Handle all other keys via nav.Lookup (includes shift+key combos)
	action := nav.Lookup(keyStr)
	switch action {
	case nav.ActionQuit:
		if m.themeWatcher != nil {
			m.themeWatcher.Close()
		}
		return m, tea.Quit
	case nav.ActionTab:
		m.activePane = 1 - m.activePane
		return m, m.updatePreview()
	case nav.ActionStack:
		// The layout falls back to side by side when the terminal is
		// too short for the stacked layout, so the choice can always be
		// made: refusing the key here would leave a user who stacked
		// the panes in a small window unable to unstack them.
		m.panesStacked = !m.panesStacked
		return m, nil
	case nav.ActionSort:
		mode := m.cycleSort(m.activePane)
		m.opResult = "sort: " + mode.Label()
		return m, m.updatePreview()
	case nav.ActionUp, nav.ActionDown, nav.ActionLeft, nav.ActionRight:
		return m.handleNavAction(action)
	case nav.ActionEnter:
		files := m.visibleFiles(m.activePane)
		cursor := m.rawCursor(m.activePane)
		if cursor < len(files) && files[cursor].IsDir {
			return m, m.beginLoad(m.activePane, files[cursor].Path)
		}
		if cursor < len(files) {
			return m, openCmd(files[cursor].Path, files[cursor].Name)
		}
		return m, nil
	case nav.ActionRefresh:
		// Refresh the listing and the preview: Shift+E is the way to
		// pick up a file another program has changed without moving
		// the cursor off it.
		return m, tea.Batch(
			m.reloadPaneCmd(m.activePane),
			m.updatePreview(),
		)
	case nav.ActionFuzzyFind:
		root := m.rawPath(m.activePane)
		m.fuzzyActive = true
		m.fuzzyInput = ""
		m.fuzzyResults = nil
		m.fuzzyCursor = 0
		// The previous tree is dropped rather than shown while the new
		// walk runs, so results never appear to belong to the wrong root.
		m.fuzzyAllFiles = nil
		m.fuzzyLoading = true
		m.fuzzyRoot = root
		m.fuzzySpinner = 0
		m.fuzzySpinning = false
		m.fuzzyGen++
		return m, loadFuzzyFilesCmd(root)
	case nav.ActionFilter:
		m.mode = inputFilter
		m.inputBuf = ""
		m.setFilter(m.activePane, "")
		m.setCursor(m.activePane, 0)
		return m, m.updatePreview()
	case nav.ActionClear:
		m.setFilter(m.activePane, "")
		m.setCursor(m.activePane, 0)
		return m, m.updatePreview()
	case nav.ActionAdd:
		m.mode = inputAdd
		m.inputBuf = ""
		return m, nil
	case nav.ActionRename:
		files := m.visibleFiles(m.activePane)
		cursor := m.rawCursor(m.activePane)
		if cursor >= len(files) {
			return m, nil
		}
		m.mode = inputRename
		m.inputBuf = files[cursor].Name
		m.renameTarget = files[cursor].Path
		return m, nil
	case nav.ActionCopy:
		return m.handleYank()
	case nav.ActionMark:
		return m.handleMove()
	case nav.ActionPaste:
		return m.handlePaste()
	case nav.ActionDelete:
		return m.handleDelete()
	case nav.ActionTogglePreview:
		m.previewHidden = !m.previewHidden
		return m, nil
	case nav.ActionGoto:
		m.mode = inputGoto
		m.inputBuf = ""
		m.loadErr = nil
		return m, nil
	case nav.ActionKeyList:
		m.keyListActive = !m.keyListActive
		if m.keyListActive {
			m.keyListQuery = ""
			m.keyListCursor = 0
		}
		return m, nil
	}
	return m, nil
}

// handleJump moves the cursor to the first visible file whose name starts
// with the given letter (case-insensitive). The search wraps around.
func (m Model) handleJump(c rune) (tea.Model, tea.Cmd) {
	files := m.visibleFiles(m.activePane)
	if len(files) == 0 {
		return m, nil
	}
	target := byte(c)
	if target >= 'a' && target <= 'z' {
		target -= 'a' - 'A' // also match uppercase
	}
	for i, f := range files {
		if len(f.Name) > 0 && (f.Name[0] == byte(c) || f.Name[0] == target) {
			m.setCursor(m.activePane, i)
			return m, m.updatePreview()
		}
	}
	return m, nil
}

// handleNavAction performs a cursor or directory movement. It is shared by
// normal mode and the filter prompt so the arrows behave identically in both,
// and the prompt stays open while they are used.
func (m Model) handleNavAction(action nav.Action) (tea.Model, tea.Cmd) {
	switch action {
	case nav.ActionUp:
		if cursor := m.rawCursor(m.activePane); cursor > 0 {
			m.setCursor(m.activePane, cursor-1)
		}
		return m, m.updatePreview()
	case nav.ActionDown:
		cursor := m.rawCursor(m.activePane)
		if cursor < len(m.visibleFiles(m.activePane))-1 {
			m.setCursor(m.activePane, cursor+1)
		}
		return m, m.updatePreview()
	case nav.ActionLeft:
		currentPath := m.targetPath(m.activePane)
		parent := filepath.Dir(currentPath)
		if parent != currentPath {
			// Record the directory being left so the parent listing can
			// put the cursor back on it. The name comes from the path,
			// not the current listing, which may have changed since entry.
			m.setRestore(m.activePane, filepath.Base(currentPath))
			return m, m.beginLoad(m.activePane, parent)
		}
		return m, nil
	case nav.ActionRight:
		// Entering a directory is a navigation step, but launching a file is
		// not: that belongs to enter alone, so an arrow press can never
		// unexpectedly hand a file to the desktop handler.
		files := m.visibleFiles(m.activePane)
		cursor := m.rawCursor(m.activePane)
		if cursor < len(files) && files[cursor].IsDir {
			return m, m.beginLoad(m.activePane, files[cursor].Path)
		}
		return m, nil
	}
	return m, nil
}

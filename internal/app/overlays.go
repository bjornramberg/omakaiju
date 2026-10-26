package app

import (
	"path/filepath"
	"runtime"

	"github.com/bjornramberg/omakaiju/internal/fuzzy"
	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

// refilterFuzzy re-applies the query in the background and reports whether
// it started the loading animation, which the caller then has to schedule.
//
// Filtering a large tree is slow enough to be felt: every keystroke scanned
// the whole tree on the UI thread, so the finder's input lagged behind the
// user's typing. The work happens in a command now, and the results are
// applied when they arrive.
//
// While the walk is still running there is nothing to filter, so the query
// is simply held until it lands.
func (m *Model) refilterFuzzy() (cmd tea.Cmd, started bool) {
	m.fuzzyCursor = 0
	if m.fuzzyLoading {
		// The animation only starts once there is a query the user is
		// actually waiting on.
		if m.fuzzyInput != "" && !m.fuzzySpinning {
			m.fuzzySpinning = true
			m.fuzzySpinner = 0
			return nil, true
		}
		return nil, false
	}
	m.fuzzyGen++
	gen := m.fuzzyGen
	query := m.fuzzyInput
	files := m.fuzzyAllFiles
	return filterFuzzyCmd(query, files, gen), false
}

// filterFuzzyCmd matches a query against a file list in the background.
func filterFuzzyCmd(query string, files []string, gen int) tea.Cmd {
	return safeCmd("fuzzy-filter", func() tea.Msg {
		return fuzzyResultsMsg{
			query:   query,
			results: fuzzy.Filter(query, files),
			gen:     gen,
		}
	})
}

// clearFuzzy drops the walk tree and filtered results and hands the memory
// back to the runtime in the background. The finder is allowed to be
// expensive while it runs; nil-ing on close lets the GC reclaim the heap.
// Called on esc and on enter (selection), the two ways a search ends.
func (m *Model) clearFuzzy() {
	m.fuzzyActive = false
	m.fuzzyInput = ""
	clear(m.fuzzyResults)
	m.fuzzyResults = nil
	m.fuzzyCursor = 0
	clear(m.fuzzyAllFiles)
	m.fuzzyAllFiles = nil
	m.fuzzyLoading = false
	m.fuzzyRoot = ""
	reclaimFuzzyMemory()
}

// reclaimFuzzyMemory asks the runtime to collect the tree the finder walked.
//
// It runs on its own goroutine so the UI never waits for it, and it carries
// its own recover: a goroutine the app owns sits outside Bubble Tea's panic
// net, so a panic here would kill the process with the terminal still in raw
// mode — the one failure mode that leaves the user with a broken shell.
//
// runtime.GC is preferred over debug.FreeOSMemory here. The latter also
// madvises every region back to the OS, which is a long stop-the-world pause
// the user would feel the instant the finder closes.
func reclaimFuzzyMemory() {
	go func() {
		defer func() {
			_ = recover()
		}()
		runtime.GC()
	}()
}

// handleKeyListKeys drives the key overlay, fuzzy-style: printable text
// feeds the query (matched against both the key and the description),
// arrows move the cursor with no side effects, backspace edits, and
// esc/shift+k closes. Enter does nothing — the panel is a reference, not a
// launcher.
func (m Model) handleKeyListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch keyID(msg) {
	case "shift+k", "esc":
		m.keyListActive = false
		m.keyListQuery = ""
		m.keyListCursor = 0
	case "backspace":
		if r := []rune(m.keyListQuery); len(r) > 0 {
			m.keyListQuery = string(r[:len(r)-1])
			m.keyListCursor = 0
		}
	case "up":
		m.moveKeyListCursor(-1)
	case "down":
		m.moveKeyListCursor(1)
	default:
		if r, ok := printableKeyRune(msg); ok {
			m.keyListQuery += string(r)
			m.keyListCursor = 0
		}
	}
	return m, nil
}

// moveKeyListCursor walks within the filtered rows, stopping at the edges
// rather than wrapping so the highlight never vanishes on a short list.
func (m *Model) moveKeyListCursor(step int) {
	rows := ui.FilterKeyRows(ui.TableKeyRows(), m.keyListQuery)
	if len(rows) == 0 {
		m.keyListCursor = 0
		return
	}
	cursor := m.keyListCursor + step
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	if cursor < 0 {
		cursor = 0
	}
	m.keyListCursor = cursor
}

func (m Model) handleFuzzyKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch keyID(msg) {
	case "esc":
		m.clearFuzzy()
		return m, nil
	case "enter":
		if m.fuzzyCursor < len(m.fuzzyResults) {
			selected := m.fuzzyResults[m.fuzzyCursor]
			dir := filepath.Dir(selected)
			// Land the cursor on the result, not on the top of its
			// directory. The listing load commits the path; the restore
			// puts the cursor on the entry once its rows arrive.
			m.setRestore(m.activePane, filepath.Base(selected))
			m.clearFuzzy()
			return m, m.beginLoad(m.activePane, dir)
		}
		return m, nil
	case "down", "ctrl+j":
		if m.fuzzyCursor < len(m.fuzzyResults)-1 {
			m.fuzzyCursor++
		}
		return m, nil
	case "up", "ctrl+k":
		if m.fuzzyCursor > 0 {
			m.fuzzyCursor--
		}
		return m, nil
	case "backspace":
		// Runes, not bytes: cutting the last byte of a multi-byte
		// character leaves invalid UTF-8 in the query, which then flows
		// into the matcher and the width calculations.
		if r := []rune(m.fuzzyInput); len(r) > 0 {
			m.fuzzyInput = string(r[:len(r)-1])
			cmd, started := m.refilterFuzzy()
			if started {
				return m, tea.Batch(cmd, fuzzyTickCmd())
			}
			return m, cmd
		}
		return m, nil
	}

	// Every other printable key — including j and k, which used to move the
	// cursor — is part of the query. Navigation stays on the arrows, and on
	// ctrl+j/ctrl+k for the users who reach for them; a terminal may send
	// ctrl+j as a line feed, which is why the arrows remain.
	if r, ok := printableKeyRune(msg); ok {
		m.fuzzyInput += string(r)
		cmd, started := m.refilterFuzzy()
		if started {
			return m, tea.Batch(cmd, fuzzyTickCmd())
		}
		return m, cmd
	}
	return m, nil
}

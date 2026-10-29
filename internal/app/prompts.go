package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/nav"

	tea "charm.land/bubbletea/v2"
)

// expandPath expands ~ to home directory, $VAR/${VAR} to environment variables,
// and resolves relative paths against the given base directory.
func expandPath(path, base string) string {
	// Expand ~
	if strings.HasPrefix(path, "~") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[1:])
	}
	// Expand $VAR and ${VAR}
	path = os.ExpandEnv(path)
	// Relative to base
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return filepath.Clean(path)
}

func (m Model) handleInputKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch keyID(msg) {
	case "up", "down", "left", "right":
		// Movement stays available while the prompt is open; filtering is
		// unaffected, so the user can walk the narrowed list and carry on
		// typing.
		if m.mode == inputFilter || m.mode == inputGoto {
			return m.handleNavAction(nav.Lookup(keyID(msg)))
		}
		return m, nil
	case "esc":
		if m.mode == inputFilter {
			m.setFilter(m.activePane, "")
		}
		m.mode = inputNone
		m.inputBuf = ""
		m.renameTarget = ""
		m.setCursor(m.activePane, 0)
		m.clampCursor(m.activePane)
		return m, m.updatePreview()
	case "enter":
		if m.mode == inputGoto {
			return m.commitGoto()
		}
		return m.commitInput()
	case "backspace":
		// Runes, not bytes: cutting the last byte of a multi-byte
		// character leaves invalid UTF-8 in the buffer, which then flows
		// into the filter and the width calculations.
		if r := []rune(m.inputBuf); len(r) > 0 {
			m.inputBuf = string(r[:len(r)-1])
			if m.mode == inputFilter {
				m.setFilter(m.activePane, m.inputBuf)
			}
			m.setCursor(m.activePane, 0)
			m.clampCursor(m.activePane)
			return m, m.updatePreview()
		}
		return m, nil
	}

	if r, ok := printableKeyRune(msg); ok {
		m.inputBuf += string(r)
		if m.mode == inputFilter {
			m.setFilter(m.activePane, m.inputBuf)
			m.setCursor(m.activePane, 0)
		}
		return m, m.updatePreview()
	}
	return m, nil
}

// entryNameError reports why name cannot be the name of one entry in a pane.
//
// A name is one component, not a path. A separator would make the prompt
// create something outside the pane the user is looking at, and "." and ".."
// would name the directory itself or its parent. A trailing slash is refused
// separately: it used to be how a directory was requested, so the refusal
// points at the key that now does that job.
//
// An empty name is not an error here; callers treat it as a cancelled prompt.
func entryNameError(name string) error {
	if strings.HasSuffix(name, string(filepath.Separator)) || strings.HasSuffix(name, "/") {
		return fmt.Errorf("a directory is made with Shift+N, not a trailing slash")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%q is not a valid name", name)
	}
	if strings.ContainsRune(name, filepath.Separator) || strings.ContainsRune(name, '/') {
		return fmt.Errorf("a name must not contain a path separator")
	}
	return nil
}

func (m Model) commitInput() (tea.Model, tea.Cmd) {
	switch m.mode {
	case inputFilter:
		m.mode = inputNone
		m.inputBuf = ""
		return m, nil

	case inputAdd, inputAddDir:
		name := m.inputBuf
		isDir := m.mode == inputAddDir
		m.mode = inputNone
		m.inputBuf = ""
		if name == "" {
			return m, nil
		}
		if err := entryNameError(name); err != nil {
			m.recordOpResult(singularResult("created", name, err))
			return m, nil
		}

		path := filepath.Join(m.rawPath(m.activePane), name)
		var err error
		verb := "created"
		if isDir {
			verb = "created dir"
			err = fs.CreateDir(path)
		} else {
			err = fs.CreateFile(path)
		}
		m.recordOpResult(singularResult(verb, name, err))
		if err != nil {
			return m, nil
		}
		// Land the cursor on the entry that was just made, the way a copy
		// or a move lands on what it created. The reload applies it.
		m.setRestore(m.activePane, name)
		return m, m.loadActive()

	case inputRename:
		name := m.inputBuf
		target := m.renameTarget
		m.mode = inputNone
		m.inputBuf = ""
		m.renameTarget = ""
		if name == "" || target == "" {
			return m, nil
		}
		// The rename prompt is the same escape in the same place: a name
		// with a separator or ".." would move the entry out of the pane.
		if err := entryNameError(name); err != nil {
			m.recordOpResult(singularResult("renamed", name, err))
			return m, nil
		}
		if name == filepath.Base(target) {
			return m, nil
		}
		err := fs.Move(target, filepath.Join(filepath.Dir(target), name))
		m.recordOpResult(singularResult("renamed", name, err))
		if err != nil {
			return m, nil
		}
		return m, m.loadActive()
	}

	m.mode = inputNone
	m.inputBuf = ""
	return m, nil
}

// commitGoto resolves the typed path and navigates the active pane to it.
//
// A failure is shown rather than swallowed: the prompt closes, so the bottom
// bar can present the error, and the message names both the path and the
// reason. The path is not committed until its listing arrives, so a directory
// that cannot be read leaves the pane exactly where it was.
func (m Model) commitGoto() (tea.Model, tea.Cmd) {
	path := m.inputBuf
	if path == "" {
		m.mode = inputNone
		m.inputBuf = ""
		return m, nil
	}

	expanded := expandPath(path, m.targetPath(m.activePane))

	info, err := os.Stat(expanded)
	if err != nil {
		m.mode = inputNone
		m.inputBuf = ""
		m.loadErr = fmt.Errorf("goto %s: %w", expanded, err)
		return m, nil
	}
	if !info.IsDir() {
		m.mode = inputNone
		m.inputBuf = ""
		m.loadErr = fmt.Errorf("goto %s: not a directory", expanded)
		return m, nil
	}

	m.mode = inputNone
	m.inputBuf = ""
	return m, m.beginLoad(m.activePane, expanded)
}

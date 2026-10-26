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

func (m Model) commitInput() (tea.Model, tea.Cmd) {
	switch m.mode {
	case inputFilter:
		m.mode = inputNone
		m.inputBuf = ""
		return m, nil

	case inputAdd:
		name := m.inputBuf
		m.mode = inputNone
		m.inputBuf = ""
		if name == "" {
			return m, nil
		}
		isDir := strings.HasSuffix(name, "/")
		name = strings.TrimSuffix(name, "/")
		if name == "" {
			return m, nil
		}

		path := filepath.Join(m.rawPath(m.activePane), name)
		if isDir {
			m.recordOpResult(singularResult("created dir", name, fs.CreateDir(path)))
		} else {
			m.recordOpResult(singularResult("created", name, fs.CreateFile(path)))
		}
		return m, m.loadActive()

	case inputRename:
		name := strings.TrimSuffix(m.inputBuf, "/")
		target := m.renameTarget
		m.mode = inputNone
		m.inputBuf = ""
		m.renameTarget = ""
		if name == "" || target == "" {
			return m, nil
		}
		if name == filepath.Base(target) {
			return m, nil
		}
		err := fs.Move(target, filepath.Join(filepath.Dir(target), name))
		m.recordOpResult(singularResult("renamed", name, err))
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

package app

import (
	"fmt"
	"os"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

// updatePreview points the preview at the file under the cursor and asks for
// it to be loaded.
//
// The load happens in the background. Reading a file and colouring it is slow
// enough to be felt — a thousand lines of source takes tens of milliseconds —
// and the user pressed an arrow key to move the cursor. Doing it here would
// make every keypress wait for the disk.
//
// The preview is identified by its path and its stamp (size and modification
// time). Keying it on the path alone meant a file edited, overwritten or
// replaced on disk kept showing its old contents until the cursor left it and
// came back.
//
// The returned command must be dispatched by the caller.
func (m *Model) updatePreview() tea.Cmd {
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor < len(files) {
		m.previewPath = files[cursor].Path
	} else {
		m.previewPath = ""
	}

	if m.previewPath == "" {
		// No preview is being shown, so any load still in flight no longer
		// matters and must not leave the panel marked as loading.
		m.previewLoading = false
		if m.previewCachePath != "" {
			m.previewContent = ui.Content{}
			m.previewCachePath = ""
			m.previewCacheStamp = ""
			m.previewVersion++
		}
		return nil
	}

	stamp := previewStamp(m.previewPath)
	if m.previewPath == m.previewCachePath && stamp == m.previewCacheStamp {
		return nil
	}

	// The cached preview belongs to another file, or to an older version of
	// this one. Drop it so the panel shows something honest rather than the
	// previous contents.
	m.previewContent = ui.Content{}
	m.previewCachePath = ""
	m.previewCacheStamp = ""
	m.previewStamp = stamp
	m.previewVersion++
	m.previewLoading = true

	return loadPreviewCmd(m.previewPath, stamp, m.theme)
}

// previewStamp identifies a file's contents. Size and modification time are
// enough to notice an edit, an overwrite or a rename over the old file, all of
// which leave the path unchanged.
func previewStamp(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}

// loadPreviewCmd gathers a preview in the background.
func loadPreviewCmd(path, stamp string, theme config.Theme) tea.Cmd {
	return safeCmd("preview", func() tea.Msg {
		return previewLoadedMsg{path: path, stamp: stamp, content: ui.Load(path, theme)}
	})
}

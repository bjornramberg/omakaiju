package app

import (
	"fmt"
	"os"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

// previewRequest identifies the background load that is in flight.
//
// The model keeps the request so a second updatePreview for the same file and
// version does not read the file again, and so a result can be recognised as
// the answer to the request that is still wanted. There is at most one; a load
// whose version is no longer the model's previewVersion belongs to a preview
// the user has left.
type previewRequest struct {
	path    string
	stamp   string
	version int
}

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
// The preview is also identified by previewVersion. Each change to what should
// be on screen moves the version on, so a load that was started for the old
// preview is dropped when it arrives instead of replacing the new one.
//
// Nothing is gathered while the layout has no column for the preview. The
// request is kept, so the preview is built when the terminal is wide enough
// again without the cursor having to move.
//
// The returned command must be dispatched by the caller.
func (m *Model) updatePreview() tea.Cmd {
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	path := ""
	stamp := ""
	if cursor < len(files) {
		path = files[cursor].Path
		stamp = previewStamp(path)
	}

	// Another file, another version of it, or a palette that has changed is
	// a new preview. Moving the version on makes any load still running for
	// the previous preview stale: its colours and contents were gathered for
	// a screen that no longer exists.
	if path != m.previewPath || stamp != m.previewStamp {
		m.previewPath = path
		m.previewStamp = stamp
		m.previewContent = ui.Content{}
		m.previewCachePath = ""
		m.previewCacheStamp = ""
		m.previewPending = previewRequest{}
		m.previewVersion++

		// Nothing is drawn for an empty cursor, or for a preview the layout
		// has no column for. Recording the path is enough: when the terminal
		// grows again, the current preview is seen to have no content yet and
		// is gathered then.
		if path == "" || !m.previewDrawn() {
			m.previewLoading = false
			return nil
		}

		m.previewLoading = true
		m.previewPending = previewRequest{path: path, stamp: stamp, version: m.previewVersion}
		return loadPreviewCmd(path, stamp, m.previewVersion, m.theme)
	}

	if path == "" {
		m.previewLoading = false
		return nil
	}

	// The content on screen is this file at this version already.
	if path == m.previewCachePath && stamp == m.previewCacheStamp {
		m.previewLoading = false
		return nil
	}

	// A load for exactly this preview is still running. Reading the file
	// again would duplicate a full read and a full highlight, and two
	// results for one file could land in either order.
	if m.previewPending == (previewRequest{path: path, stamp: stamp, version: m.previewVersion}) {
		return nil
	}

	// The preview has no column at the moment, so there is nothing to gather
	// for it. It keeps the request and returns, with its content, as soon as
	// the layout gives it room again.
	if !m.previewDrawn() {
		m.previewLoading = false
		return nil
	}

	// This preview is current but no load is running for it: the theme was
	// reloaded, or a result was dropped, or the preview has just come back
	// into view. Start the load now.
	m.previewLoading = true
	m.previewPending = previewRequest{path: path, stamp: stamp, version: m.previewVersion}
	return loadPreviewCmd(path, stamp, m.previewVersion, m.theme)
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

// loadPreviewCmd gathers a preview in the background. The version is carried
// with the result so Update can tell whether the preview it was gathered for
// is still the one on screen.
func loadPreviewCmd(path, stamp string, version int, theme config.Theme) tea.Cmd {
	return safeCmd("preview", func() tea.Msg {
		return previewLoadedMsg{path: path, stamp: stamp, version: version, content: ui.Load(path, theme)}
	})
}

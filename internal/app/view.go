package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bjornramberg/omakaiju/internal/ui"
	"github.com/bjornramberg/omakaiju/internal/version"

	tea "charm.land/bubbletea/v2"
)

// layout builds the geometry for the current model state. It is the single
// place the app turns its state into a Layout, so what View draws and what
// updatePreview decides to gather cannot disagree about whether a column is
// on screen.
func (m Model) layout() ui.Layout {
	l := ui.NewLayout(m.width, m.height, m.theme)
	l.PanesStacked = m.panesStacked
	l.PreviewHidden = m.previewHidden
	l.DebugActive = m.debugActive
	return l
}

// previewDrawn reports whether the preview is actually drawn this frame. The
// layout yields the preview's column when keeping it would squeeze a filename
// out of the file panes, so building a preview then would be work whose result
// is thrown away.
//
// Before the first size message the terminal is still unmeasured: no layout
// has been chosen yet, so the old behaviour stands and the preview is gathered.
func (m Model) previewDrawn() bool {
	if m.width <= 0 || m.height <= 0 {
		return !m.previewHidden
	}
	l := m.layout()
	return !l.TooSmall() && l.PreviewVisible()
}

// debugDrawn reports whether the debug pane is actually drawn this frame. Like
// the preview it can be open and still not fit, in which case sampling runtime
// telemetry stops the world for a panel nobody can see.
func (m Model) debugDrawn() bool {
	if m.width <= 0 || m.height <= 0 {
		return m.debugActive
	}
	l := m.layout()
	return !l.TooSmall() && l.DebugVisible()
}

// View draws the screen and survives a panic in the renderer.
//
// The same reasoning as Update applies: a fault in one frame must not end
// the session. The frame is replaced with a plain-text notice and the next
// frame tries again, so a transient failure — a file that vanished between
// the listing and the preview, say — costs the user one frame rather than
// their whole session.
func (m Model) View() (v tea.View) {
	defer func() {
		r := recover()
		if r != nil {
			report := newCrashReport("view", r)
			// View cannot hand a model back, so a state created here would be
			// discarded. The constructor always sets one, so the history is kept
			// in every real session.
			m.crashStateFor().record(report)
			v = m.crashView(report)
		}
		// Every frame, including the crash fallback, stays in the alternate
		// screen. Quitting then restores what the terminal showed before the
		// app started instead of leaving the last frame behind.
		v.AltScreen = true
	}()
	return m.view()
}

// previewSuppressedNote is shown when the preview is on but has yielded its
// column to keep the file panes readable. It is deliberately worded as a
// width problem, so it cannot be mistaken for the user having turned the
// preview off with Shift+I.
const previewSuppressedNote = "preview hidden: terminal too narrow"

func (m Model) view() tea.View {
	if m.viewHook != nil {
		m.viewHook()
	}
	layout := m.layout()

	// Before the first WindowSizeMsg the size is still zero, and a terminal
	// dragged below the minimum has nowhere to draw. Both would otherwise hand
	// Lipgloss zero or negative dimensions and produce stray artifacts.
	if layout.TooSmall() {
		return tea.NewView(layout.TooSmallPlaceholder())
	}

	topBar := ui.NewTopBar(m.width, m.theme)
	topBar.Filter = m.filterOf(m.activePane)
	topBar.Path = m.rawPath(m.activePane)

	markedSet := make(map[string]bool, len(m.marked))
	for path := range m.marked {
		markedSet[path] = true
	}

	leftPane := ui.NewPane(layout.PaneWidth(), layout.PaneHeight(true), m.activePane == 0, m.theme)
	leftPane.Path = m.leftPath
	leftPane.Filter = m.leftFilter
	leftPane.Files = m.visibleFiles(0)
	leftPane.Cursor = m.leftCursor
	leftPane.TotalFiles = len(m.leftFiles)
	leftPane.Marked = markedSet
	if m.activePane == 0 && len(m.marked) > 0 {
		leftPane.Status = fmt.Sprintf("%d marked", len(m.marked))
	}
	leftRendered := leftPane.Render()

	rightPane := ui.NewPane(layout.PaneWidth(), layout.PaneHeight(false), m.activePane == 1, m.theme)
	rightPane.Path = m.rightPath
	rightPane.Filter = m.rightFilter
	rightPane.Files = m.visibleFiles(1)
	rightPane.Cursor = m.rightCursor
	rightPane.TotalFiles = len(m.rightFiles)
	rightPane.Marked = markedSet
	if m.activePane == 1 && len(m.marked) > 0 {
		rightPane.Status = fmt.Sprintf("%d marked", len(m.marked))
	}
	rightRendered := rightPane.Render()

	var previewRendered string
	if layout.PreviewVisible() {
		previewWidth := layout.PreviewWidth()
		previewHeight := layout.PreviewHeight()

		// An unchanged preview is not redrawn. Measuring a styled line is
		// expensive, and the frame rate is wasted work when nothing about
		// the preview changed.
		if cached, ok := m.previewRender.get(m.previewPath, m.previewVersion, previewWidth, previewHeight); ok {
			previewRendered = cached
		} else {
			preview := ui.NewPreview(previewWidth, previewHeight, m.theme)
			preview = preview.SetPath(m.previewPath)

			switch {
			case m.previewPath == "":
				previewRendered = preview.RenderContent(ui.Content{Kind: ui.KindEmpty})
			case m.previewCachePath == m.previewPath:
				previewRendered = preview.RenderContent(m.previewContent)
			default:
				// The preview for this file is still being gathered. The panel
				// says so rather than showing the previous file's contents.
				previewRendered = preview.RenderContent(ui.Content{Kind: ui.KindLoading})
			}
			m.previewRender.put(m.previewPath, m.previewVersion, previewWidth, previewHeight, previewRendered)
		}
	}

	var debugRendered string
	if layout.DebugVisible() {
		debug := ui.NewDebug(layout.RightColumnWidth(), layout.DebugHeight(), m.theme)
		debug.Info = ui.DebugInfo{
			Stats:         m.debugStats,
			Version:       version.Display(),
			PID:           os.Getpid(),
			StartedAt:     m.startedAt,
			Startup:       m.startup.Elapsed(),
			ActivePath:    m.rawPath(m.activePane),
			Entries:       len(m.visibleFiles(m.activePane)),
			Cursor:        m.rawCursor(m.activePane),
			Marked:        len(m.marked),
			Sort:          m.sortOf(m.activePane).Label(),
			Filter:        m.filterOf(m.activePane),
			PreviewPath:   m.previewPath,
			PreviewHidden: m.previewHidden,
			ThemePath:     m.cfg.ThemePath,
			Width:         m.width,
			Height:        m.height,
		}
		debugRendered = debug.Render()
	}

	bottomBar := ui.NewBottomBar(m.width, m.theme)
	bottomBar.Splash = ui.Splash(m.theme, m.splashStep)
	switch m.mode {
	case inputFilter:
		bottomBar.Prompt = "filter"
		bottomBar.PromptInput = m.inputBuf
	case inputAdd:
		bottomBar.Prompt = "new file"
		bottomBar.PromptInput = m.inputBuf
	case inputAddDir:
		bottomBar.Prompt = "new directory"
		bottomBar.PromptInput = m.inputBuf
	case inputRename:
		bottomBar.Prompt = "rename"
		bottomBar.PromptInput = m.inputBuf
	case inputGoto:
		bottomBar.Prompt = "goto"
		bottomBar.PromptInput = m.inputBuf
	default:
		if m.copier != nil {
			progress := ui.NewProgress(m.copier, m.theme)
			bottomBar.Progress = &progress
		} else if m.confirmEmptyTrash {
			bottomBar.Input = "there are items in the trash — empty it? (y)es / (n)o (Esc quits without emptying)"
		} else if m.confirmDelete {
			bottomBar.Input = filepath.Base(m.deleteTarget) + "? (t)rash / (f)orce / (n)o"
		} else if m.crashErr != nil {
			// A recovered crash outranks a load error: it is the one thing
			// the user needs to know about, and it may still be relevant.
			bottomBar.Error = m.crashErr.Error()
		} else if m.opErr != nil {
			// An operation failure is what the user just caused, so it
			// outranks a listing error. loadErr stays on the model, so the
			// next reload can show it once the operation message is gone.
			bottomBar.Error = m.opErr.Error()
		} else if m.loadErr != nil {
			bottomBar.Error = m.loadErr.Error()
		} else if m.opResult != "" {
			bottomBar.OpResult = m.opResult
		} else if layout.PreviewSuppressed() {
			// The preview is on, but the layout gave its column to the file
			// panes because the terminal is too narrow for both. Say so, so
			// the missing panel does not look like Shift+I's "off".
			bottomBar.OpResult = previewSuppressedNote
		}
	}
	bottomRendered := bottomBar.Render()

	view := layout.Render(topBar.Render(), leftRendered, rightRendered, previewRendered, debugRendered, bottomRendered)

	// The finder sits on top of the finished layout rather than replacing it,
	// so the panes stay visible and the terminal is not left with a blank
	// grid behind the panel.
	if m.fuzzyActive {
		finder := ui.NewFuzzyFinder(m.width, m.height, m.theme)
		finder.Focused = true
		finder.Input = m.fuzzyInput
		finder.Results = m.fuzzyResults
		finder.Cursor = m.fuzzyCursor
		finder.Loading = m.fuzzyLoading
		finder.Root = m.fuzzyRoot
		finder.Spinner = m.fuzzySpinner
		finder.Truncated = m.fuzzyTruncated
		view = ui.OverlayCentered(view, finder.Render(), m.width)
	}

	if m.keyListActive {
		keyList := ui.NewKeyList(m.width, m.height, m.theme)
		keyList.Focused = true
		keyList.Input = m.keyListQuery
		keyList.Cursor = m.keyListCursor
		view = ui.OverlayCentered(view, keyList.Render(), m.width)
	}

	// The frame is composed. The next write to the terminal is the one the
	// user sees, so this is the moment the startup is measured from.
	m.startup.FrameReady()

	return tea.NewView(view)
}

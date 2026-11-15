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
		// Mouse reporting is a per-frame property, which is what lets a modal
		// hand the terminal's own text selection back for as long as it is
		// open. A frame without the feature, or under a modal, reports None so
		// the terminal never captures the pointer in the first place.
		v.MouseMode = m.mouseMode()
	}()
	return m.view()
}

// previewSuppressedNote is shown when the preview is on but has yielded its
// column to keep the file panes readable. It is deliberately worded as a
// width problem, so it cannot be mistaken for the user having turned the
// preview off with Shift+I.
const previewSuppressedNote = "preview hidden: terminal too narrow"

// paneViews builds the two file panes the frame draws. It is the single source
// of the per-pane state: whether a pane has a filter or a status row decides
// how many rows the listing gets, and that decides which entry sits under the
// pointer. Render and the mouse hit-test both take these values, so they cannot
// disagree about where a row is.
func (m Model) paneViews(layout ui.Layout) [2]ui.Pane {
	markedSet := make(map[string]bool, len(m.marked))
	for path := range m.marked {
		markedSet[path] = true
	}

	panes := [2]ui.Pane{
		ui.NewPane(layout.PaneWidth(), layout.PaneHeight(true), m.activePane == 0, m.theme),
		ui.NewPane(layout.PaneWidth(), layout.PaneHeight(false), m.activePane == 1, m.theme),
	}
	paths := [2]string{m.leftPath, m.rightPath}
	filters := [2]string{m.leftFilter, m.rightFilter}
	totals := [2]int{len(m.leftFiles), len(m.rightFiles)}

	for pane := range panes {
		panes[pane].Path = paths[pane]
		panes[pane].Filter = filters[pane]
		panes[pane].Files = m.visibleFiles(pane)
		panes[pane].Cursor = m.rawCursor(pane)
		// The pane draws the window the model has scrolled to. Handing it
		// over here keeps the renderer and the hit-test on the same rows, so
		// a click can never resolve against a window the user is not seeing.
		panes[pane].Scroll = m.rawScroll(pane)
		panes[pane].TotalFiles = totals[pane]
		panes[pane].Marked = markedSet
		// The status line names the marked count, and only the focused pane
		// carries it. It shortens that pane's listing, which is exactly why
		// the hit-test reads the same built Pane.
		if m.activePane == pane && len(m.marked) > 0 {
			panes[pane].Status = fmt.Sprintf("%d marked", len(m.marked))
		}
	}
	return panes
}

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

	panes := m.paneViews(layout)
	leftRendered := panes[0].Render()
	rightRendered := panes[1].Render()

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

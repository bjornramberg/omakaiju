package app

import (
	"fmt"
	"path/filepath"

	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

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

func (m Model) view() tea.View {
	if m.viewHook != nil {
		m.viewHook()
	}
	layout := ui.NewLayout(m.width, m.height, m.theme)
	layout.PanesStacked = m.panesStacked
	layout.PreviewHidden = m.previewHidden

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
	if !m.previewHidden {
		previewWidth := layout.PreviewWidth()
		previewHeight := layout.MainAreaHeight()

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

	bottomBar := ui.NewBottomBar(m.width, m.theme)
	bottomBar.Splash = ui.Splash(m.theme, m.splashStep)
	switch m.mode {
	case inputFilter:
		bottomBar.Prompt = "filter"
		bottomBar.PromptInput = m.inputBuf
	case inputAdd:
		bottomBar.Prompt = "new (dir/)"
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
		}
	}
	bottomRendered := bottomBar.Render()

	view := layout.Render(topBar.Render(), leftRendered, rightRendered, previewRendered, bottomRendered)

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

	return tea.NewView(view)
}

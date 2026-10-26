package app

import (
	"fmt"

	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

// Update dispatches a message and survives a panic in the handler.
//
// A panic here is a bug in the app, not the user's fault, and ending the
// session over one is the worst possible response: the terminal is restored
// but the user loses their place, their panes and any pending work. The
// failed update is discarded and the pre-update state is handed back, so the
// app redraws and carries on.
//
// Model is a value type, but some of its fields are references: the marked
// set and the file slices. The handlers that change those copy them first
// (handleMove and restoreMarks for the marks, applySort for the listing), and
// visibleFiles never hands out the model's own array. That is what makes the
// state this method holds genuinely untouched by a panic in update: returning
// it is exactly the state the user was in before the keypress that triggered
// the fault.
func (m Model) Update(msg tea.Msg) (model tea.Model, cmd tea.Cmd) {
	model = m
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		report := newCrashReport("update", r)
		m.ensureCrash()
		m.crash.record(report)
		m.crashErr = crashError(report, m.crash.count())
		model = m
		cmd = nil
		// A spread of unrelated failures means the state is no longer
		// trustworthy. Quitting cleanly beats limping along with a UI that
		// may be showing nonsense.
		if m.crash.exhausted() {
			cmd = tea.Quit
		}
	}()
	return m.update(msg)
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.updateHook != nil {
		m.updateHook()
	}
	if m.updateAfterHook != nil {
		defer m.updateAfterHook()
	}
	switch msg := msg.(type) {
	case panicMsg:
		// A command panicked. The command's work was abandoned; the session
		// continues with the error reported in the bottom bar.
		m.ensureCrash()
		m.crash.record(msg.report)
		m.crashErr = crashError(msg.report, m.crash.count())
		if m.crash.exhausted() {
			return m, tea.Quit
		}
		return m, nil
	case previewLoadedMsg:
		// The cursor may have moved on while the preview was being
		// gathered. A preview for a file the user is no longer on is
		// stale, and showing it would put the wrong file in the panel.
		if msg.path != m.previewPath {
			return m, nil
		}
		// The file changed again while it was being read. The content just
		// gathered is already out of date, so ask for the current version
		// instead of displaying it.
		if msg.stamp != m.previewStamp {
			return m, m.updatePreview()
		}
		m.previewContent = msg.content
		m.previewCachePath = msg.path
		m.previewCacheStamp = msg.stamp
		m.previewVersion++
		m.previewLoading = false
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case dirLoadedMsg:
		// A load for a path the pane has since left is stale. Committing it
		// would drag the pane back to that directory — and operations would
		// then run there — even though the user's last action was to leave
		// it. Bubble Tea runs every command in its own goroutine, so the
		// results of two navigation keypresses can arrive in either order.
		if msg.path != m.targetPath(msg.pane) {
			return m, nil
		}
		if msg.err != nil {
			// The listing never arrived, so the path is not committed. The
			// header keeps naming the directory whose rows are on screen,
			// and the failure is reported instead of leaving the two out of
			// step.
			m.setPending(msg.pane, "")
			m.loadErr = msg.err
			return m, nil
		}
		m.loadErr = nil
		m.setPending(msg.pane, "")
		if msg.pane == 0 {
			m.leftPath = msg.path
			m.leftFiles = msg.files
			m.leftCursor = 0
			m.leftFilter = ""
		} else {
			m.rightPath = msg.path
			m.rightFiles = msg.files
			m.rightCursor = 0
			m.rightFilter = ""
		}
		// Re-apply the pane's sort mode: ReadDir always arrives in name
		// order, so a size mode would otherwise be lost on every reload.
		m.applySort(msg.pane)
		// A load triggered by going up should land on the directory we came
		// from, and one triggered by a copy, move, or paste on the entry that
		// operation put there; every other load starts at the top as before.
		if name := m.restoreOf(msg.pane); name != "" {
			for i, f := range m.rawFiles(msg.pane) {
				if f.Name == name {
					m.setCursor(msg.pane, i)
					break
				}
			}
			m.clampCursor(msg.pane)
		}
		return m, m.updatePreview()

	case copyProgressMsg:
		// A message from a copy the user already cancelled belongs to a
		// copy that no longer exists. Adopting its copier would resurrect
		// the dead copy, block the keys again and report a copy that
		// never finished.
		if msg.gen != m.copyGen {
			return m, nil
		}

		// The copier is owned by the model; adopt it and drive the next step.
		m.copier = msg.copier

		if msg.err != nil {
			done := 0
			if msg.copier != nil {
				done = msg.copier.FilesDone()
			}
			name := msg.name
			if name == "" {
				name = "copy"
			}
			path := msg.path
			if path == "" {
				path = name
			}
			clipboard := m.copyClipboard
			marks := m.copyMarks
			skipped := m.copySkipped
			m.finishCopy()
			verb := "copied"
			failures := []opFailure{{name: name, path: path, err: msg.err}}
			if msg.move {
				verb = "moved"
				// The mover reports the entries it could not even start,
				// and the one that stopped it is reported beside them.
				failures = append(moverFailures(msg.copier), failures...)
				done = 0
				if msg.copier != nil {
					done = msg.copier.Moved()
				}
			}
			m.recordOpResult(fileOpMsg{
				verb:      verb,
				succeeded: done,
				failures:  failures,
				clipboard: clipboard,
				marks:     marks,
				skipped:   skipped,
			})
			return m, m.reloadPanesCmd()
		}

		if msg.done {
			files := m.copyFiles
			marks := m.copyMarks
			skipped := m.copySkipped
			m.finishCopy()
			if msg.move {
				moved := 0
				var failures []opFailure
				if msg.copier != nil {
					moved = msg.copier.Moved()
					failures = moverFailures(msg.copier)
				}
				m.recordOpResult(fileOpMsg{
					verb:      "moved",
					succeeded: moved,
					failures:  failures,
					marks:     marks,
					skipped:   skipped,
				})
				return m, m.reloadPanesCmd()
			}
			m.recordOpResult(fileOpMsg{verb: "copied", succeeded: files})
			return m, m.reloadPanesCmd()
		}

		return m, stepCopyCmd(msg.copier, msg.gen)

	case fileOpMsg:
		// The whole batch reports back as one message, so its counts and
		// failures cannot be overwritten by a sibling result arriving
		// later. Reloading the panes must not clear the outcome: opErr
		// lives apart from loadErr precisely so the listing refresh that
		// follows an operation cannot erase the operation's error.
		m.recordOpResult(msg)
		return m, m.reloadPanesCmd()

	case themeReloadedMsg:
		if msg.err != nil {
			if msg.closed {
				// The watcher was shut down for good; keep the old theme
				// rather than blanking the UI, and stop listening.
				return m, nil
			}
			// A transient watcher failure — inotify's queue overflow on a
			// busy state directory is the realistic one — must not end
			// hot-reload for the rest of the session. Report it and listen
			// again.
			m.loadErr = fmt.Errorf("theme watcher: %w", msg.err)
			return m, m.waitThemeCmd()
		}
		m.theme = msg.theme
		// Colours changed, so the highlighted preview must be rebuilt.
		// Clearing the cached path makes updatePreview load it again, this
		// time with the new palette.
		m.previewCachePath = ""
		m.previewCacheStamp = ""
		m.previewStamp = ""
		m.previewLoading = false
		// The rendered frame is cached under the preview's version, and an
		// empty preview is drawn too: without a new version the cache would
		// hand back the frame the old palette produced.
		m.previewVersion++
		// Re-arm: the previous wait command has returned, so listening must be
		// resumed or only the first reload would ever take effect.
		return m, tea.Batch(m.updatePreview(), m.waitThemeCmd())

	case openMsg:
		if msg.err != nil {
			m.opErr = msg.err
			m.opResult = ""
		} else {
			m.opErr = nil
			m.opResult = "opening " + msg.name + "..."
		}
		// No pane reload: opening a file must not disturb the listing or move
		// the cursor.
		return m, nil

	case fuzzyTickMsg:
		// A tick outliving the walk it was animating must not restart the
		// loop, so the session flags decide whether to continue.
		if !m.fuzzyActive || !m.fuzzyLoading || m.fuzzyInput == "" {
			m.fuzzySpinning = false
			return m, nil
		}
		m.fuzzySpinner++
		return m, fuzzyTickCmd()

	case splashTickMsg:
		m.splashStep++
		if ui.SplashActive(m.splashStep) {
			return m, splashTickCmd()
		}
		return m, nil

	case fuzzyResultsMsg:
		// A result for a query the user has since changed, or for a slower
		// run that finished after a faster one, is stale. Applying it would
		// show results that do not match what is typed.
		if msg.gen != m.fuzzyGen || msg.query != m.fuzzyInput {
			return m, nil
		}
		m.fuzzyResults = msg.results
		m.fuzzyCursor = 0
		return m, nil

	case fuzzyFilesLoadedMsg:
		// A walk started elsewhere, or from a session already abandoned, must
		// not replace the current results.
		if msg.root != m.fuzzyRoot {
			return m, nil
		}
		m.fuzzyAllFiles = msg.files
		m.fuzzyLoading = false
		m.fuzzyTruncated = msg.truncated
		// The tree has landed, so the query the user typed while it was
		// running can be applied now.
		cmd, _ := m.refilterFuzzy()
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

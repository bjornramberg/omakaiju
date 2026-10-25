package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/fuzzy"
	"github.com/bjornramberg/omakaiju/internal/nav"
	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
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

type dirLoadedMsg struct {
	pane  int
	path  string
	files []fs.Entry
	err   error
}

// opFailure is one entry a file operation could not finish, with the reason
// reduced to something a person can read.
type opFailure struct {
	name string
	// path is the entry's source path, used to put a failed move back into
	// the marked set so it can be retried.
	path string
	err  error
}

// fileOpMsg reports the outcome of a file operation, or of a whole batch.
//
// A batch runs to completion inside a single command and reports back as one
// message. That is what makes its outcome independent of completion order:
// there is no race between per-file messages, so one failure cannot be erased
// by a success that happens to arrive after it.
type fileOpMsg struct {
	// verb is the past tense shown for the successful entries: "moved",
	// "copied", "pasted", "deleted", "trashed", "renamed", "created".
	verb string
	// name is set for a single-entry operation, which reads better as
	// "<verb> <name>" than as a count.
	name      string
	succeeded int
	skipped   int
	failures  []opFailure
	// text, when set, replaces the generated result. It is for outcomes that
	// are more than a count, such as a trash that moved the payload but could
	// not write its metadata.
	text string
	// marks holds the marked entries a move batch attempted, keyed by path.
	// A failed entry is put back so the user can fix the cause and retry
	// instead of marking everything again.
	marks map[string]fs.Entry
	// clipboard is the paste selection the batch consumed. A failed paste
	// re-arms it for the same reason.
	clipboard *Clipboard
}

// result renders the line shown in the bottom bar. Failures are sorted by
// name, so the same batch always reads the same way, whatever order its work
// happened to finish in.
func (msg fileOpMsg) result() string {
	if msg.text != "" {
		return msg.text
	}
	if len(msg.failures) == 0 && msg.skipped == 0 && msg.succeeded == 1 && msg.name != "" {
		return msg.verb + " " + msg.name
	}

	var parts []string
	if msg.succeeded > 0 {
		parts = append(parts, fmt.Sprintf("%s %d file(s)", msg.verb, msg.succeeded))
	}
	if msg.skipped > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d", msg.skipped))
	}
	if len(msg.failures) > 0 {
		failures := append([]opFailure(nil), msg.failures...)
		sort.Slice(failures, func(i, j int) bool { return failures[i].name < failures[j].name })
		reasons := make([]string, 0, len(failures))
		for _, f := range failures {
			reasons = append(reasons, fmt.Sprintf("%s (%s)", f.name, readableOpError(f.err)))
		}
		parts = append(parts, fmt.Sprintf("failed %d: %s", len(failures), strings.Join(reasons, ", ")))
	}
	return strings.Join(parts, ", ")
}

// readableOpError strips the noisy prefix os.LinkError and os.PathError add
// ("rename /a /b: ", "open /a: ") and keeps the reason the user can act on,
// such as "permission denied". The file name is already shown beside it, so
// repeating the path adds nothing.
//
// A refused replacement is the exception: fs reports it as a
// *fs.RefusedReplacement, which names the destination that could not be
// replaced and why. Unwrapping that would reduce the whole outcome to the bare
// reason the backup failed, leaving the user unable to tell a replacement that
// was refused because the old file could not be kept from a copy that failed
// some other way. The type is recognised rather than its wording, so fs may
// reword the message without the context silently disappearing.
func readableOpError(err error) string {
	if err == nil {
		return ""
	}
	var refused *fs.RefusedReplacement
	if errors.As(err, &refused) {
		return refused.Error()
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) && linkErr.Err != nil {
		err = linkErr.Err
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) && pathErr.Err != nil {
		err = pathErr.Err
	}
	return err.Error()
}

// opItem is one entry in a batch: its display name, source, and destination.
type opItem struct {
	name string
	src  string
	dst  string
}

// singularResult builds the one-entry result of an operation that finished
// inline, such as creating or renaming an entry.
func singularResult(verb, name string, err error) fileOpMsg {
	if err != nil {
		return fileOpMsg{verb: verb, failures: []opFailure{{name: name, err: err}}}
	}
	return fileOpMsg{verb: verb, name: name, succeeded: 1}
}

// copyProgressMsg reports one chunk of an in-flight copy and asks for the next
// step, or signals completion.
type copyProgressMsg struct {
	copier *fs.Copier
	done   bool
	err    error
	// name is the file the step was working on when it failed, captured
	// before Step ran because a failed Step drops the copier's pointer to
	// the current entry.
	name string
	// path is where that file was read from, so a failed transfer can put
	// the entry back for another try.
	path string
	// gen identifies the copy this message belongs to.
	//
	// A copy that was cancelled still has a step in flight, and its message
	// arrives after the user has moved on. Without a generation to check it
	// against, the message would adopt its dead copier, block the keys
	// again and report a copy that never finished.
	gen int
	// move marks the transfer as a move rather than a copy. A move removes
	// the source once the copy has landed, so it is reported with its own
	// wording.
	move bool
}

// openMsg reports the result of opening a file with the desktop handler. It is
// deliberately not a fileOpMsg: that handler reloads both panes, which would
// reset the cursor every time a file was opened.
type openMsg struct {
	name string
	err  error
}

type themeReloadedMsg struct {
	theme config.Theme
	err   error
	// closed reports that the watcher was shut down and cannot be waited on
	// again. Any other error is transient, and the wait is re-armed so one
	// inotify hiccup does not end hot-reload for the rest of the session.
	closed bool
}

// fuzzyTickMsg drives the spinner while a walk is in progress. The animation
// state lives on the model because the finder widget is rebuilt on every frame
// and cannot hold it itself.
type fuzzyTickMsg struct{}

// splashTickMsg advances the version splash animation in the bottom bar.
type splashTickMsg struct{}

// previewLoadedMsg carries a preview that was gathered in the background.
type previewLoadedMsg struct {
	// path is the file the preview belongs to. A preview for a file the
	// cursor has already left is stale and is dropped.
	path string
	// stamp identifies the file's contents at the time it was read: its size
	// and modification time. A preview keyed on the path alone would keep
	// showing a file that has since been edited, overwritten or replaced.
	stamp   string
	content ui.Content
}

// fuzzyFilesLoadedMsg reports the files a walk found.
type fuzzyFilesLoadedMsg struct {
	files []string
	// truncated reports that the walk stopped at its limit and the tree
	// holds more than was collected.
	truncated bool
	// root identifies which walk produced files. A walk from a directory the
	// user has since left can finish after the current one, and applying it
	// would replace the live results with a stale tree.
	root string
}

// fuzzyResultsMsg carries results that were filtered in the background.
type fuzzyResultsMsg struct {
	// query is the query the results belong to. Results for a query the
	// user has since changed are dropped.
	query string
	// results are the matching paths, best first.
	results []string
	// gen identifies the filter run. A slow run that finishes after a
	// faster one must not overwrite the faster one's results.
	gen int
}

type Clipboard struct {
	Action string
	Files  []fs.Entry
	Source string
}

// inputMode is the active single-line text prompt. Filter, add, rename, and goto all
// share one capture path: type to edit, enter commits, esc cancels.
type inputMode int

const (
	inputNone inputMode = iota
	inputFilter
	inputAdd
	inputRename
	inputGoto
)

type Model struct {
	cfg    config.Config
	theme  config.Theme
	width  int
	height int

	activePane int
	// panesStacked toggles the file panes between side by side and stacked.
	// It lives for the session only and resets on launch.
	panesStacked bool
	// leftSort/rightSort hold the per-pane sort cycle:
	// name -> size↓ -> size↑ -> name. Zero value is SortByName (default).
	leftSort    fs.SortMode
	rightSort   fs.SortMode
	leftPath    string
	leftFiles   []fs.Entry
	leftCursor  int
	rightPath   string
	rightFiles  []fs.Entry
	rightCursor int
	// leftPending/rightPending hold the path of a listing the pane is still
	// waiting for. The pane's path and listing are committed together when
	// that listing arrives, so a failed load leaves both as they were.
	// Navigation uses the pending path meanwhile, which keeps "enter, then
	// leave" heading for the parent of the directory just entered.
	leftPending  string
	rightPending string
	// loadErr reports a directory-listing failure. It is deliberately
	// separate from opErr: a pane reload must never erase the outcome of a
	// file operation, and an operation must not erase a listing error.
	loadErr error
	// opErr reports the last file-operation failure. It survives the pane
	// reloads an operation triggers and is cleared only when the user acts.
	opErr error

	clipboard     Clipboard
	clipboardOn   bool
	confirmDelete bool
	deleteTarget  string
	opResult      string

	// marked accumulates entries selected with m for a bulk move to the other
	// pane. Keyed by absolute path so it survives directory changes.
	marked map[string]fs.Entry

	// copier drives an in-flight copy so long copies can report progress and be
	// cancelled. Small copies skip this and use the blocking path.
	copier    *fs.Copier
	copyFiles int
	// copyClipboard is the paste selection a large copy consumed. If the copy
	// fails, the selection is re-armed so the paste can be retried.
	copyClipboard *Clipboard
	// copyMarks holds the marked entries a move consumed. A failed entry is
	// put back so the user can fix the cause and try again.
	copyMarks map[string]fs.Entry
	// copySkipped counts the entries a move never attempted, such as ones
	// that had already vanished. They are reported with the result.
	copySkipped int
	// copyGen identifies the current copy. It changes whenever a copy
	// starts or ends, so a message from a copy that has been cancelled is
	// recognised as stale and dropped.
	copyGen int

	// leftRestore/rightRestore hold the name the cursor should land on when a
	// pane's listing loads: the directory we came from after going up a level,
	// and the entry a copy, move, or paste just put there. Without them both
	// operations end with the pane scrolled back to the top.
	leftRestore  string
	rightRestore string

	themeWatcher  *fs.Watcher
	previewPath   string
	previewHidden bool

	// previewContent is the gathered preview for previewPath. It is built
	// in the background so the render path never waits for the disk.
	previewContent ui.Content
	// previewCachePath is the file previewContent belongs to.
	previewCachePath string
	// previewCacheStamp identifies the version of that file previewContent
	// was read from. Together with the path it is the preview's identity.
	previewCacheStamp string
	// previewStamp identifies the version the in-flight preview request was
	// made for, so a second request for the same unchanged file is not
	// dispatched while the first is still running.
	previewStamp string
	// previewVersion counts the changes to the preview. The render cache
	// uses it to tell a changed preview from an unchanged one.
	previewVersion int
	// previewLoading marks the window between asking for a preview and
	// it arriving.
	previewLoading bool
	// previewRender remembers the last drawn preview so an unchanged one
	// is not redrawn on every frame.
	previewRender *previewRenderCache

	fuzzyActive   bool
	fuzzyInput    string
	fuzzyResults  []string
	fuzzyCursor   int
	fuzzyAllFiles []string
	// fuzzyLoading marks the window between pressing ctrl+f and the walk
	// finishing. A large root takes seconds, and without this the panel would
	// show the previous search's results in the meantime.
	fuzzyLoading bool
	// fuzzyRoot is the directory the live walk belongs to.
	fuzzyRoot string
	// fuzzyGen identifies the current filter run. It changes on every
	// query, so a slow run that finishes after a faster one is dropped
	// instead of overwriting the newer results.
	fuzzyGen int
	// fuzzyTruncated reports that the walk stopped at its limit, so the
	// results cover only part of the tree.
	fuzzyTruncated bool
	// fuzzySpinner is the current animation frame.
	fuzzySpinner int

	// splashStep counts the version-splash frames drawn since startup; the
	// bar renders the corresponding mix level until the splash fades out.
	splashStep int

	// keyListActive pins the key overlay up; every key press feeds its
	// search or moves its cursor (see handleKeyListKeys).
	keyListActive bool
	// keyListQuery is the live search over both the key and description
	// columns; keyListCursor indexes the filtered rows.
	keyListQuery  string
	keyListCursor int

	// jumpActive marks that Space was pressed and we're waiting for a letter
	// to jump to the first file starting with that letter.
	jumpActive bool

	// fuzzySpinning records whether the animation loop is running. It only
	// starts once the user types, so a walk that finishes immediately does not
	// flash a spinner for results nobody has had time to see.
	fuzzySpinning bool

	leftFilter  string
	rightFilter string

	mode         inputMode
	inputBuf     string
	renameTarget string

	// crash holds the session's panic history. It is a pointer because Model
	// is copied by value on every update, and a crash recorded during a
	// render must be visible to the next render.
	crash *crashState
	// crashErr is the one-line summary of the most recent crash, shown in
	// the bottom bar until the next keypress.
	crashErr error

	// updateHook and viewHook inject a fault into the update and render
	// paths. They exist so the panic recovery can be exercised without
	// waiting for a real bug to trip it; both are nil in production.
	//
	// updateAfterHook runs once the message handler has finished and before
	// its result is returned. It lets a test panic after a handler has made
	// a change, which is the case the crash recovery has to discard.
	updateHook      func()
	updateAfterHook func()
	viewHook        func()
}

func NewModel(cfg config.Config) Model {
	theme, err := config.LoadTheme(cfg.ThemePath)
	if err != nil {
		theme = config.DefaultTheme()
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "/"
	}

	m := Model{
		cfg:           cfg,
		theme:         theme,
		leftPath:      cwd,
		rightPath:     cwd,
		leftCursor:    0,
		rightCursor:   0,
		crash:         &crashState{},
		previewRender: &previewRenderCache{},
	}

	// The watcher belongs to the model so it can be closed on quit and so the
	// waiting command has something to block on. Creating it here rather than in
	// a goroutine also means the model actually holds it; assigning from inside a
	// command only mutates that command's copy.
	if watcher, err := fs.NewWatcher(); err == nil {
		dirs := cfg.ThemeWatchDirs
		if len(dirs) == 0 {
			dirs = []string{filepath.Dir(cfg.ThemePath)}
		}
		for _, dir := range dirs {
			_ = watcher.Watch(dir)
		}
		m.themeWatcher = watcher
	}

	return m
}

func loadDirCmd(pane int, path string) tea.Cmd {
	return safeCmd("load-dir", func() tea.Msg {
		files, err := fs.ReadDir(path)
		return dirLoadedMsg{pane: pane, path: path, files: files, err: err}
	})
}

// batchOpCmd applies one operation to every item in a single command and
// reports one aggregate result. Running the whole batch here rather than
// dispatching one command per entry is what keeps the outcome independent of
// completion order: there is exactly one message to apply.
//
// marks and clipboard are the selection the batch consumes. They ride along on
// the result so a failure can restore what the user would otherwise have to
// select again.
func batchOpCmd(verb string, items []opItem, skipped int, marks map[string]fs.Entry, clipboard *Clipboard, op func(src, dst string) error) tea.Cmd {
	return safeCmd(verb, func() tea.Msg {
		msg := fileOpMsg{verb: verb, skipped: skipped, marks: marks, clipboard: clipboard}
		for _, item := range items {
			if err := op(item.src, item.dst); err != nil {
				msg.failures = append(msg.failures, opFailure{name: item.name, path: item.src, err: err})
				continue
			}
			msg.succeeded++
		}
		return msg
	})
}

// singleOpCmd runs one path-based operation and reports it as a one-entry
// result. The name is reported even when the operation fails, so the user can
// tell which file was involved.
func singleOpCmd(verb, name, path string, op func(string) error) tea.Cmd {
	return safeCmd(verb, func() tea.Msg {
		return singularResult(verb, name, op(path))
	})
}

// copyProgressThreshold is the size below which a copy runs synchronously.
// Showing a bar for a handful of bytes only produces a one-frame flash, so
// small copies keep the simple path and just report a result.
const copyProgressThreshold = 1 << 20

// startCopyCmd builds the copier and performs the first step. clipboard is the
// paste selection the copy consumed, restored if the job cannot start.
func startCopyCmd(srcs []string, dstDir string, gen int, clipboard *Clipboard) tea.Cmd {
	return safeCmd("copy-start", func() tea.Msg {
		copier, err := fs.NewCopier(srcs, dstDir)
		if err != nil {
			// Nothing was copied. Name each source so the failure reads
			// like any other aggregate result.
			msg := fileOpMsg{verb: "copied", clipboard: clipboard}
			for _, src := range srcs {
				msg.failures = append(msg.failures, opFailure{name: filepath.Base(src), path: src, err: err})
			}
			return msg
		}
		return copyProgressMsg{copier: copier, gen: gen}
	})
}

// startMoveCmd builds the mover and performs the first step.
//
// A move within one filesystem is a rename, and it costs nothing. A move
// onto another filesystem — a share, say — cannot be a rename, so the entry
// is copied across, and that copy has to travel. It therefore runs through
// the same steppable machinery as a copy, and gets a progress bar and a
// cancel of its own.
//
// The marks ride along on the result so a move that fails can put the
// selection back the way it was.
func startMoveCmd(srcs []string, dstDir string, gen int, marks map[string]fs.Entry, skipped int) tea.Cmd {
	return safeCmd("move-start", func() tea.Msg {
		copier, err := fs.NewMover(srcs, dstDir)
		if err != nil {
			msg := fileOpMsg{verb: "moved", skipped: skipped, marks: marks}
			for _, src := range srcs {
				msg.failures = append(msg.failures, opFailure{name: filepath.Base(src), path: src, err: err})
			}
			return msg
		}
		// A move that had nothing to copy is already finished, so it reports
		// straight away rather than flashing a progress bar for a frame.
		//
		// A rename-only move is that case even though Pending() is not zero:
		// its renames are outstanding work, but no bytes have to travel, so
		// the move is driven to completion here instead of handing a job with
		// nothing to draw to the progress loop. Every step runs until the
		// mover says it is done, and a step that fails is reported the same
		// way the progress path reports one.
		if copier.CopyPending() == 0 && copier.Pending() > 0 {
			for {
				// Capture the entry before Step runs: a failed Step drops the
				// copier's pointer to the current job.
				name := copier.CurrentName()
				path := copier.CurrentSource()
				done, stepErr := copier.Step()
				if stepErr != nil {
					return copyProgressMsg{
						copier: copier,
						gen:    gen,
						move:   true,
						err:    stepErr,
						name:   name,
						path:   path,
					}
				}
				if done {
					return copyProgressMsg{copier: copier, gen: gen, move: true, done: true}
				}
			}
		}
		return copyProgressMsg{copier: copier, gen: gen, move: true, done: copier.Pending() == 0}
	})
}

// stepCopyCmd advances the copier by one chunk.
func stepCopyCmd(copier *fs.Copier, gen int) tea.Cmd {
	return safeCmd("copy-step", func() tea.Msg {
		// A nil copier would dereference into a panic. It cannot happen
		// today, but the guard costs nothing and turns a future mistake
		// into a reported error instead of a dead session.
		if copier == nil {
			return singularResult("copied", "copy", errors.New("no copier in flight"))
		}
		// Capture the file before Step runs: a failed Step discards the
		// copier's pointer to the current job, which is exactly the file
		// the user needs named in the error.
		name := copier.CurrentName()
		path := copier.CurrentSource()
		done, err := copier.Step()
		return copyProgressMsg{copier: copier, done: done, err: err, name: name, path: path, gen: gen, move: copier.IsMove()}
	})
}

func deleteCmd(path string) tea.Cmd {
	return singleOpCmd("deleted", filepath.Base(path), path, fs.Delete)
}

func openCmd(path, name string) tea.Cmd {
	return safeCmd("open", func() tea.Msg {
		return openMsg{name: name, err: fs.OpenFile(path)}
	})
}

// trashCmd moves a path into the trash. Trash reports the payload's new
// location alongside an error: once the rename has happened, a failure to
// write the .trashinfo record leaves the file in the trash with nothing
// pointing back to where it came from. That destination must be shown, or the
// user is told the delete failed while the file is no longer where it was.
func trashCmd(path string) tea.Cmd {
	return safeCmd("trash", func() tea.Msg {
		name := filepath.Base(path)
		newPath, err := fs.Trash(path)
		if err == nil {
			return fileOpMsg{verb: "trashed", name: name, succeeded: 1}
		}

		reason := readableOpError(err)
		if newPath == "" {
			return fileOpMsg{
				verb:     "trashed",
				failures: []opFailure{{name: name, path: path, err: err}},
				text:     fmt.Sprintf("trash failed: %s did not move (%s)", name, reason),
			}
		}
		return fileOpMsg{
			verb:     "trashed",
			failures: []opFailure{{name: name, path: path, err: err}},
			text:     fmt.Sprintf("trashed %s to %s (write failed: %s)", name, newPath, reason),
		}
	})
}

func loadFuzzyFilesCmd(root string) tea.Cmd {
	return safeCmd("fuzzy-walk", func() tea.Msg {
		result, _ := fs.WalkDir(root)
		return fuzzyFilesLoadedMsg{files: result.Files, truncated: result.Truncated, root: root}
	})
}

// fuzzyTickCmd advances the loading spinner. The loop stops on its own: a tick
// that arrives once the walk has landed, the finder has been closed, or the
// root has moved on is simply dropped rather than reissued.
func fuzzyTickCmd() tea.Cmd {
	return safeCmd("fuzzy-tick", tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return fuzzyTickMsg{}
	}))
}

// splashTickCmd advances the version splash one frame. Update re-issues it
// until the splash has fully faded out, then lets the loop end so no ticker
// outlives the animation.
func splashTickCmd() tea.Cmd {
	return safeCmd("splash-tick", tea.Tick(ui.SplashTick, func(time.Time) tea.Msg {
		return splashTickMsg{}
	}))
}

// waitThemeCmd blocks until the next palette-relevant fsnotify event and
// reports it. It is deliberately one-shot: Update re-issues it after handling
// each message, so the watcher survives every reload instead of stopping after
// the first one.
func (m Model) waitThemeCmd() tea.Cmd {
	watcher := m.themeWatcher
	if watcher == nil {
		return nil
	}
	path := m.cfg.ThemePath
	omarchyPath := m.cfg.OmarchyStateDir

	return safeCmd("theme-watch", func() tea.Msg {
		for {
			select {
			case event, ok := <-watcher.Events():
				if !ok {
					return themeReloadedMsg{err: fmt.Errorf("theme watcher closed"), closed: true}
				}
				if !relevantThemeEvent(event.Name, path, omarchyPath) {
					continue
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Chmod) {
					// Editors and the theme engine write in bursts, so let the
					// write settle before reading.
					time.Sleep(100 * time.Millisecond)
					theme, err := config.LoadTheme(path)
					if err != nil {
						// A theme switch briefly swaps the symlink; the next
						// event will carry the final file.
						continue
					}
					return themeReloadedMsg{theme: theme}
				}

			case err, ok := <-watcher.Errors():
				if !ok {
					// The watcher was closed, which also closes the events
					// channel. Returning here ends the wait cleanly instead
					// of spinning on a closed channel.
					return themeReloadedMsg{err: fmt.Errorf("theme watcher closed"), closed: true}
				}
				if err != nil {
					// The watcher is still alive, so this is a transient
					// failure. Update reports it and arms the wait again;
					// returning here without re-arming is what used to stop
					// theme switches from ever being noticed again.
					return themeReloadedMsg{err: err}
				}
			}
		}
	})
}

// relevantThemeEvent reports whether an fsnotify event concerns the palette.
// A theme switch re-points the omarchy symlink rather than rewriting the file,
// so events under the state directory count too.
func relevantThemeEvent(name, themePath, omarchyStateDir string) bool {
	if name == "" {
		return false
	}
	if name == themePath {
		return true
	}
	if strings.HasSuffix(name, "colors.toml") {
		return true
	}
	return omarchyStateDir != "" && strings.HasPrefix(name, omarchyStateDir)
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		loadDirCmd(0, m.leftPath),
		loadDirCmd(1, m.rightPath),
		m.waitThemeCmd(),
		splashTickCmd(),
	)
}

func (m Model) rawFiles(pane int) []fs.Entry {
	if pane == 0 {
		return m.leftFiles
	}
	return m.rightFiles
}

func (m Model) rawCursor(pane int) int {
	if pane == 0 {
		return m.leftCursor
	}
	return m.rightCursor
}

func (m Model) rawPath(pane int) string {
	if pane == 0 {
		return m.leftPath
	}
	return m.rightPath
}

func (m *Model) setPath(pane int, path string) {
	if pane == 0 {
		m.leftPath = path
	} else {
		m.rightPath = path
	}
}

// targetPath is where the pane is heading: the path of a load already in
// flight, or the committed path when nothing is pending. Navigation works
// from it so a second keypress applies to the directory the first one asked
// for, even before its listing has arrived.
func (m Model) targetPath(pane int) string {
	if pane == 0 {
		if m.leftPending != "" {
			return m.leftPending
		}
		return m.leftPath
	}
	if m.rightPending != "" {
		return m.rightPending
	}
	return m.rightPath
}

func (m *Model) setPending(pane int, path string) {
	if pane == 0 {
		m.leftPending = path
	} else {
		m.rightPending = path
	}
}

// beginLoad records the path a pane is heading to and dispatches the load.
// The path is not committed here: the result commits it, so a failed read
// leaves the header naming the directory whose rows are still on screen.
func (m *Model) beginLoad(pane int, path string) tea.Cmd {
	m.setPending(pane, path)
	return loadDirCmd(pane, path)
}

// reloadPaneCmd refreshes a pane without abandoning a navigation already in
// flight for it.
func (m *Model) reloadPaneCmd(pane int) tea.Cmd {
	return m.beginLoad(pane, m.targetPath(pane))
}

// reloadPanesCmd refreshes both panes. It is the reload every file operation
// ends with.
func (m *Model) reloadPanesCmd() tea.Cmd {
	return tea.Batch(m.reloadPaneCmd(0), m.reloadPaneCmd(1))
}

func (m Model) filterOf(pane int) string {
	if pane == 0 {
		return m.leftFilter
	}
	return m.rightFilter
}

func (m *Model) setFilter(pane int, q string) {
	if pane == 0 {
		m.leftFilter = q
	} else {
		m.rightFilter = q
	}
}

func (m *Model) setCursor(pane, cursor int) {
	if pane == 0 {
		m.leftCursor = cursor
	} else {
		m.rightCursor = cursor
	}
}

func (m Model) sortOf(pane int) fs.SortMode {
	if pane == 0 {
		return m.leftSort
	}
	return m.rightSort
}

func (m *Model) setSort(pane int, mode fs.SortMode) {
	if pane == 0 {
		m.leftSort = mode
	} else {
		m.rightSort = mode
	}
}

// applySort re-orders a pane's raw listing by its current sort mode.
//
// The sort happens on a copy. Model is copied by value on every update, but
// a slice is a reference: sorting the model's own backing array in place
// would leak out of a failed update and corrupt the state crash recovery
// hands back.
func (m *Model) applySort(pane int) {
	if pane == 0 {
		m.leftFiles = sortEntriesCopy(m.leftFiles, m.leftSort)
	} else {
		m.rightFiles = sortEntriesCopy(m.rightFiles, m.rightSort)
	}
}

// sortEntriesCopy returns entries sorted by mode, leaving the caller's slice
// and its backing array untouched.
func sortEntriesCopy(entries []fs.Entry, mode fs.SortMode) []fs.Entry {
	out := make([]fs.Entry, len(entries))
	copy(out, entries)
	fs.SortEntriesBy(out, mode)
	return out
}

// cycleSort advances the active pane through name -> size↓ -> size↑ -> name,
// keeps the cursor on the same entry when it survives the reorder, and
// reports the new mode.
func (m *Model) cycleSort(pane int) fs.SortMode {
	// Remember the selected entry so the cursor can follow it across the
	// reorder instead of jumping to an unrelated row.
	var selected string
	if files := m.visibleFiles(pane); m.rawCursor(pane) < len(files) {
		selected = files[m.rawCursor(pane)].Path
	}
	next := m.sortOf(pane).Next()
	m.setSort(pane, next)
	m.applySort(pane)
	if selected != "" {
		for i, f := range m.visibleFiles(pane) {
			if f.Path == selected {
				m.setCursor(pane, i)
				break
			}
		}
	}
	m.clampCursor(pane)
	return next
}

// visibleFiles returns the pane's listing after filtering.
//
// The result is never the model's own backing array: with no filter active
// the caller gets a copy, and filtering already allocates. A caller that
// reorders the slice it is handed therefore cannot change the model — and a
// panic after such a change cannot leave it behind in the restored state.
func (m Model) visibleFiles(pane int) []fs.Entry {
	files := m.rawFiles(pane)
	if filter := m.filterOf(pane); filter != "" {
		return nav.FilterEntries(files, filter)
	}
	out := make([]fs.Entry, len(files))
	copy(out, files)
	return out
}

func (m *Model) setRestore(pane int, name string) {
	if pane == 0 {
		m.leftRestore = name
	} else {
		m.rightRestore = name
	}
}

// restoreOf reports the name the pane's cursor should land on after its next
// load.
//
// The name outlives the loads that apply it rather than being consumed by the
// first one. A paste of several entries dispatches one operation per entry and
// each of them reloads the panes, so a restore that was taken on the first
// landing would leave the cursor back at the top once the rest arrive.
func (m Model) restoreOf(pane int) string {
	if pane == 0 {
		return m.leftRestore
	}
	return m.rightRestore
}

// clearRestores drops both panes' pending restores. A key press means the user
// has moved on, so a selection left over from an earlier operation must not
// survive to drag the cursor back the next time a listing loads.
func (m *Model) clearRestores() {
	m.leftRestore = ""
	m.rightRestore = ""
}

// restoreLanded points a pane at the entry an operation just put on disk, so a
// copy, move, or paste ends with the cursor on what it created.
//
// The entry is the first in name order rather than whichever operation message
// arrives last: the moves of a marked set are dispatched from a map, so their
// messages come back in no particular order, and every reload they trigger has
// to agree on the same row.
func (m *Model) restoreLanded(pane int, names []string) {
	first := ""
	for _, name := range names {
		if first == "" || name < first {
			first = name
		}
	}
	if first == "" {
		return
	}
	m.setRestore(pane, first)
}

func (m *Model) clampCursor(pane int) {
	cursor := m.rawCursor(pane)
	if n := len(m.visibleFiles(pane)); cursor > n-1 {
		cursor = n - 1
	}
	if cursor < 0 {
		cursor = 0
	}
	m.setCursor(pane, cursor)
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
	}
	return m, nil
}

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

// moverFailures converts the entries a mover could not move into the
// failures a result reports.
//
// A move that cannot even start an entry — because the destination is the
// file itself, say — is reported the same way as one that failed halfway,
// so the user sees every entry that did not arrive in one place.
func moverFailures(copier *fs.Copier) []opFailure {
	if copier == nil {
		return nil
	}
	failures := copier.Failures()
	out := make([]opFailure, 0, len(failures))
	for _, f := range failures {
		out = append(out, opFailure{name: f.Name, path: f.Path, err: f.Err})
	}
	return out
}

// recordOpResult stores the outcome of a file operation. A failure goes into
// opErr, which the bottom bar shows and only a keypress clears; a success goes
// into opResult. Both live apart from loadErr so neither the pane reload that
// follows an operation nor a later listing failure can erase the other.
func (m *Model) recordOpResult(msg fileOpMsg) {
	m.opResult = ""
	// Any result ends the copy lifecycle, including a copier that never
	// started, so the retry snapshot must not outlive it. The file count was
	// set when the paste was dispatched; a simple copy that took the
	// blocking path reports through here without finishCopy having run.
	m.copyClipboard = nil
	m.copyFiles = 0
	if len(msg.failures) > 0 {
		m.restoreSelection(msg)
		m.opErr = errors.New(msg.result())
		return
	}
	m.opErr = nil
	m.opResult = msg.result()
}

// restoreSelection puts back what a failed operation consumed: the marked
// entries it could not move, and the paste selection it used. The work that
// did land stays done; only what failed is offered for another try.
func (m *Model) restoreSelection(msg fileOpMsg) {
	if len(msg.marks) > 0 {
		paths := make([]string, 0, len(msg.failures))
		for _, f := range msg.failures {
			paths = append(paths, f.path)
		}
		m.restoreMarks(msg.marks, paths)
	}
	m.restoreClipboard(msg.clipboard)
}

// restoreMarks puts the marked entries named by paths back into the marked
// set. marks is the snapshot the transfer took, keyed by source path.
//
// The set is copied before it is extended. Model is copied by value on every
// update, but a map is a reference: writing into the model's map in place
// would survive a panic and be visible in the pre-update state Update
// restores.
func (m *Model) restoreMarks(marks map[string]fs.Entry, paths []string) {
	var next map[string]fs.Entry
	for _, path := range paths {
		entry, ok := marks[path]
		if !ok {
			continue
		}
		if next == nil {
			next = cloneMarked(m.marked)
		}
		next[entry.Path] = entry
	}
	if next != nil {
		m.marked = next
	}
}

// cloneMarked returns a marked set that can be changed without touching the
// caller's map.
func cloneMarked(marked map[string]fs.Entry) map[string]fs.Entry {
	out := make(map[string]fs.Entry, len(marked)+1)
	for path, entry := range marked {
		out[path] = entry
	}
	return out
}

// restoreClipboard re-arms a paste selection a transfer consumed. A selection
// the user staged after the transfer was dispatched is left alone.
func (m *Model) restoreClipboard(clipboard *Clipboard) {
	if clipboard != nil && !m.clipboardOn {
		m.clipboard = *clipboard
		m.clipboardOn = true
	}
}

func (m Model) loadActive() tea.Cmd {
	return m.reloadPaneCmd(m.activePane)
}

func (m Model) handleYank() (tea.Model, tea.Cmd) {
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor >= len(files) {
		return m, nil
	}
	m.clipboard = Clipboard{
		Action: "copy",
		Files:  []fs.Entry{files[cursor]},
		Source: m.rawPath(m.activePane),
	}
	m.clipboardOn = true
	m.opResult = "yanked " + files[cursor].Name
	return m, nil
}

// handleMove toggles the cursor entry in the marked set. Pressing m again on
// an already-marked entry unmarks it, so several files or folders can be staged
// before moving them into the pane under the cursor with Shift+V.
func (m Model) handleMove() (tea.Model, tea.Cmd) {
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor >= len(files) {
		return m, nil
	}

	entry := files[cursor]
	// Copy the set before changing it. Model is copied by value, but the map
	// is shared with the state Update would restore after a panic, so an
	// in-place mark or unmark would leak out of a failed update.
	m.marked = cloneMarked(m.marked)

	if _, exists := m.marked[entry.Path]; exists {
		delete(m.marked, entry.Path)
		m.opResult = fmt.Sprintf("unmarked %s (%d)", entry.Name, len(m.marked))
		return m, nil
	}

	m.marked[entry.Path] = entry
	m.opResult = fmt.Sprintf("marked %s (%d)", entry.Name, len(m.marked))
	return m, nil
}

// finishCopy releases the copier and restores normal operation.
//
// The generation is bumped so a message still in flight from the copy that
// just ended is recognised as stale and dropped rather than reviving the
// copier.
func (m *Model) finishCopy() {
	m.copier = nil
	m.copyFiles = 0
	m.copyClipboard = nil
	m.copyMarks = nil
	m.copySkipped = 0
	m.copyGen++
}

// cancelCopy aborts an in-flight copy or move.
//
// The cancel signals the abort at once and leaves the part of the cleanup that
// can wait to a goroutine. Copier.Close waits for a step that is still in
// flight, and a step can be stuck in a read or a sync on a stalled network
// filesystem; waiting for it here — this runs on the event loop — would freeze
// the whole UI until the filesystem returned.
//
// The cleanup still finishes. Abort is non-blocking: it sets the flag, and an
// in-flight step unwinds when it observes it. The goroutine's Close then waits
// for that step, so the scratch file is removed, the directories the copy made
// are taken away and the handles are closed, just later than the keypress.
//
// The goroutine captures the copier and nothing else. Model is copied by value
// on every update, so a write to a model copy from here would race the app and
// change nothing the user sees; the teardown touches only the copier.
//
// Because nothing landed, the selection the transfer consumed is put back.
// For a move, an entry is restored only if its source is still on disk: one
// that was already renamed away did move, and must not come back.
func (m Model) cancelCopy() (tea.Model, tea.Cmd) {
	if m.copier == nil {
		return m, nil
	}
	copier := m.copier
	verb := "copy"
	if copier.IsMove() {
		verb = "move"
	}

	marks := m.copyMarks
	clipboard := m.copyClipboard

	// Signal the abort before returning to the loop, so the step stops as
	// soon as it looks at the flag rather than copying another chunk first.
	_ = copier.Abort()
	go func() {
		// A goroutine the app owns sits outside Bubble Tea's panic net, so
		// a panic in it would kill the process with the terminal still in
		// raw mode. There is no UI to report it to from here; the recover
		// keeps a teardown bug from taking the session with it.
		defer func() { _ = recover() }()
		_ = copier.Close()
	}()
	m.finishCopy()

	if len(marks) > 0 {
		pending := make([]string, 0, len(marks))
		for path := range marks {
			if _, err := os.Stat(path); err == nil {
				pending = append(pending, path)
			}
		}
		m.restoreMarks(marks, pending)
	}
	m.restoreClipboard(clipboard)

	m.opResult = verb + " cancelled"
	return m, m.reloadPanesCmd()
}

// copySizeWalkLimit bounds how many entries the pre-copy size walk visits, so
// starting a paste cannot stall on an enormous tree. It mirrors the finder's
// limit for the same reason: the estimate only decides which copy path to use,
// and erring toward the simple path is cheaper than blocking the UI.
const copySizeWalkLimit = 200000

// pasteSizeWalk is the pre-paste size estimate. It is a variable so a test
// can observe that the walk runs inside the paste command rather than on the
// event loop.
var pasteSizeWalk = copyTotalBytes

// copyTotalBytes estimates how many bytes a paste will move, so the copy path
// can decide whether the work deserves a progress bar.
//
// A directory entry's Size is the inode's own size — a few kilobytes — not the
// payload inside it. Summing that made a multi-gigabyte folder look tiny and
// sent it down the blocking path with no progress at all. Directories are
// therefore walked; files still just contribute their recorded size.
//
// The walk is disk work and must not run in Update: it stats every entry up to
// copySizeWalkLimit, and on a large tree that would freeze the UI before the
// progress notice could even be drawn. pasteCopyCmd calls it from the command
// goroutine, where the other disk work happens.
func copyTotalBytes(files []fs.Entry) int64 {
	var total int64
	budget := copySizeWalkLimit
	for _, f := range files {
		if !f.IsDir {
			total += f.Size
			continue
		}
		total += dirContentSize(f.Path, &budget)
	}
	return total
}

// dirContentSize sums the files beneath root. budget is shared across every
// directory in the batch; once it is exhausted the walk stops. A truncated
// walk can only understate the size, which at worst keeps a copy on the simple
// path rather than stalling the key press.
func dirContentSize(root string, budget *int) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if *budget <= 0 {
			return filepath.SkipAll
		}
		*budget--
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// pasteCopyCmd estimates the paste and performs it, all in the background.
//
// The size walk decides whether the selection gets the steppable copier (and
// its progress bar) or the simple blocking path. Doing that walk in Update
// froze the whole UI on a large folder before the progress notice could be
// shown; here it happens where the copy itself happens.
func pasteCopyCmd(files []fs.Entry, dstDir string, gen int, clipboard *Clipboard) tea.Cmd {
	return safeCmd("paste-copy", func() tea.Msg {
		if pasteSizeWalk(files) >= copyProgressThreshold {
			srcs := make([]string, 0, len(files))
			for _, f := range files {
				srcs = append(srcs, f.Path)
			}
			return startCopyCmd(srcs, dstDir, gen, clipboard)()
		}

		msg := fileOpMsg{verb: "pasted", clipboard: clipboard}
		for _, f := range files {
			dst := filepath.Join(dstDir, f.Name)
			if filepath.Clean(dst) == filepath.Clean(f.Path) {
				continue
			}
			if err := fs.CopyRecursive(f.Path, dst); err != nil {
				msg.failures = append(msg.failures, opFailure{name: f.Name, path: f.Path, err: err})
				continue
			}
			msg.succeeded++
		}
		return msg
	})
}

// handlePaste drops everything into the active pane's current directory — the
// pane the user is looking at is where a paste belongs, wherever the copy was
// started. Tabbing to the other pane and pasting therefore targets that pane,
// and pasting after navigating inside the copying pane gains a fresh copy
// there. Marks outweigh the y-staged file, and a file already sitting in the
// target is skipped while the clipboard stays armed, so tabbing somewhere
// else can still complete the copy.
//
// Whatever lands is named as the target pane's next cursor position, so the
// reload that follows the operation puts the cursor on the new file rather than
// back at the top of the listing.
func (m Model) handlePaste() (tea.Model, tea.Cmd) {
	if m.copier != nil {
		what := "copy"
		if m.copier.IsMove() {
			what = "move"
		}
		m.opResult = what + " in progress"
		return m, nil
	}
	if len(m.marked) > 0 {
		return m.pasteMarks()
	}
	if !m.clipboardOn {
		return m, nil
	}

	targetPath := m.rawPath(m.activePane)

	// A copy dispatches first and measures in the command. The size walk
	// decides between the steppable copier — progress visible, cancellable —
	// and the simple path, but it is disk work, so it must not run here: on a
	// large tree it would freeze the UI before the notice below could be
	// drawn. The command replaces the notice with the result when it lands.
	if m.clipboard.Action == "copy" {
		for _, file := range m.clipboard.Files {
			dst := filepath.Join(targetPath, file.Name)
			if filepath.Clean(dst) == filepath.Clean(file.Path) {
				m.opResult = "already in this directory"
				return m, nil
			}
		}
		names := make([]string, 0, len(m.clipboard.Files))
		for _, f := range m.clipboard.Files {
			names = append(names, f.Name)
		}
		// Snapshot the selection before disarming it: a failure re-arms
		// this exact paste so the user can retry.
		clipboard := m.clipboard
		m.clipboardOn = false
		m.copyClipboard = &clipboard
		m.copyFiles = len(clipboard.Files)
		m.copyGen++
		m.restoreLanded(m.activePane, names)
		m.opResult = fmt.Sprintf("copying %d file(s)...", len(names))
		return m, pasteCopyCmd(clipboard.Files, targetPath, m.copyGen, &clipboard)
	}

	verb, gerund := "moved", "moving"
	op := fs.Move
	if m.clipboard.Action == "copy" {
		verb, gerund = "pasted", "copying"
		op = fs.CopyRecursive
	}

	var items []opItem
	var pasted []string
	for _, file := range m.clipboard.Files {
		dst := filepath.Join(targetPath, file.Name)
		if filepath.Clean(dst) == filepath.Clean(file.Path) {
			continue
		}
		items = append(items, opItem{name: file.Name, src: file.Path, dst: dst})
		pasted = append(pasted, file.Name)
	}
	if len(items) == 0 {
		m.opResult = "already in this directory"
		return m, nil
	}

	clipboard := m.clipboard
	m.clipboardOn = false
	m.restoreLanded(m.activePane, pasted)
	// The work has not run yet, so this is a progress notice rather than a
	// result: claiming a paste whose outcome is still unknown is the bug.
	m.opResult = fmt.Sprintf("%s %d file(s)...", gerund, len(items))
	return m, batchOpCmd(verb, items, 0, nil, &clipboard, op)
}

// pasteMarks moves the marked set into the active pane's current directory.
// Entries that vanished from disk, or that already sit in the destination,
// are skipped so a stale mark cannot fail the whole batch. The pane is left
// pointing at one of the entries that arrived, not at the top of the listing.
//
// A paste with nothing to move keeps the marks. They are still staged, so
// tabbing to the destination and pasting there has to work; clearing them
// made the two-step move the clipboard also supports impossible for marks.
func (m Model) pasteMarks() (tea.Model, tea.Cmd) {
	targetPath := m.rawPath(m.activePane)

	var items []opItem
	var landed []string
	var srcs []string
	marks := make(map[string]fs.Entry, len(m.marked))
	skipped := 0
	alreadyHere := 0
	missing := 0
	for _, entry := range m.marked {
		if _, err := os.Stat(entry.Path); err != nil {
			skipped++
			missing++
			continue
		}
		dst := filepath.Join(targetPath, entry.Name)
		if filepath.Clean(dst) == filepath.Clean(entry.Path) {
			skipped++
			alreadyHere++
			continue
		}
		items = append(items, opItem{name: entry.Name, src: entry.Path, dst: dst})
		srcs = append(srcs, entry.Path)
		landed = append(landed, entry.Name)
		marks[entry.Path] = entry
	}
	dstDir := targetPath

	if len(items) == 0 {
		// Nothing was consumed, so nothing is cleared. Saying the entries
		// are already here matches the clipboard path, which keeps its
		// selection in the same case.
		if alreadyHere > 0 && missing == 0 {
			m.opResult = "already in this directory"
		} else {
			m.opResult = "nothing to move"
		}
		return m, nil
	}

	m.marked = nil
	m.restoreLanded(m.activePane, landed)

	// The moves have not run yet, so report that they are starting rather
	// than claiming they are done. The final count comes back in the batch
	// result once every move has actually been attempted. The marks ride
	// along so a move that fails can be put back for another try.
	m.opResult = fmt.Sprintf("moving %d file(s)...", len(items))
	m.copyMarks = marks
	m.copySkipped = skipped
	m.copyGen++
	return m, startMoveCmd(srcs, dstDir, m.copyGen, marks, skipped)
}

func (m Model) handleDelete() (tea.Model, tea.Cmd) {
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor >= len(files) {
		return m, nil
	}
	m.confirmDelete = true
	m.deleteTarget = files[cursor].Path
	return m, nil
}

func (m Model) handleConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	// Trash is the safe choice, so it is the one that also answers to the
	// keys people reach for out of habit.
	case "t", "T", "y", "Y", "enter":
		m.confirmDelete = false
		target := m.deleteTarget
		m.deleteTarget = ""
		m.opResult = "trashing " + filepath.Base(target) + "..."
		return m, trashCmd(target)
	case "f", "F":
		m.confirmDelete = false
		target := m.deleteTarget
		m.deleteTarget = ""
		m.opResult = "deleting " + filepath.Base(target) + "..."
		return m, deleteCmd(target)
	case "n", "N", "esc":
		m.confirmDelete = false
		m.deleteTarget = ""
		m.opResult = "cancelled"
		return m, nil
	}
	return m, nil
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

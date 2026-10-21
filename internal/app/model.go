package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

type fileOpMsg struct {
	err error
}

// copyProgressMsg reports one chunk of an in-flight copy and asks for the next
// step, or signals completion.
type copyProgressMsg struct {
	copier *fs.Copier
	done   bool
	err    error
	// gen identifies the copy this message belongs to.
	//
	// A copy that was cancelled still has a step in flight, and its message
	// arrives after the user has moved on. Without a generation to check it
	// against, the message would adopt its dead copier, block the keys
	// again and report a copy that never finished.
	gen int
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
	path    string
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
	loadErr     error

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
	// copyGen identifies the current copy. It changes whenever a copy
	// starts or ends, so a message from a copy that has been cancelled is
	// recognised as stale and dropped.
	copyGen int

	// leftRestore/rightRestore hold the directory name the cursor should return
	// to after going up a level, so leaving and re-entering a folder keeps the
	// position instead of jumping to the top.
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
	updateHook func()
	viewHook   func()
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

func copyCmd(src, dst string) tea.Cmd {
	return safeCmd("copy", func() tea.Msg {
		err := fs.CopyRecursive(src, dst)
		return fileOpMsg{err: err}
	})
}

// copyProgressThreshold is the size below which a copy runs synchronously.
// Showing a bar for a handful of bytes only produces a one-frame flash, so
// small copies keep the simple path and just report a result.
const copyProgressThreshold = 1 << 20

// startCopyCmd builds the copier and performs the first step.
func startCopyCmd(srcs []string, dstDir string, gen int) tea.Cmd {
	return safeCmd("copy-start", func() tea.Msg {
		copier, err := fs.NewCopier(srcs, dstDir)
		if err != nil {
			return fileOpMsg{err: err}
		}
		return copyProgressMsg{copier: copier, gen: gen}
	})
}

// stepCopyCmd advances the copier by one chunk.
func stepCopyCmd(copier *fs.Copier, gen int) tea.Cmd {
	return safeCmd("copy-step", func() tea.Msg {
		// A nil copier would dereference into a panic. It cannot happen
		// today, but the guard costs nothing and turns a future mistake
		// into a reported error instead of a dead session.
		if copier == nil {
			return fileOpMsg{err: errors.New("copy: no copier in flight")}
		}
		done, err := copier.Step()
		return copyProgressMsg{copier: copier, done: done, err: err, gen: gen}
	})
}

func moveCmd(src, dst string) tea.Cmd {
	return safeCmd("move", func() tea.Msg {
		err := fs.Move(src, dst)
		return fileOpMsg{err: err}
	})
}

func deleteCmd(path string) tea.Cmd {
	return safeCmd("delete", func() tea.Msg {
		err := fs.Delete(path)
		return fileOpMsg{err: err}
	})
}

func openCmd(path, name string) tea.Cmd {
	return safeCmd("open", func() tea.Msg {
		return openMsg{name: name, err: fs.OpenFile(path)}
	})
}

func trashCmd(path string) tea.Cmd {
	return safeCmd("trash", func() tea.Msg {
		_, err := fs.Trash(path)
		return fileOpMsg{err: err}
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
					return themeReloadedMsg{err: fmt.Errorf("theme watcher closed")}
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
					return themeReloadedMsg{err: fmt.Errorf("theme watcher closed")}
				}
				if err != nil {
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
func (m *Model) applySort(pane int) {
	if pane == 0 {
		fs.SortEntriesBy(m.leftFiles, m.leftSort)
	} else {
		fs.SortEntriesBy(m.rightFiles, m.rightSort)
	}
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

func (m Model) visibleFiles(pane int) []fs.Entry {
	return nav.FilterEntries(m.rawFiles(pane), m.filterOf(pane))
}

func (m *Model) setRestore(pane int, name string) {
	if pane == 0 {
		m.leftRestore = name
	} else {
		m.rightRestore = name
	}
}

func (m *Model) takeRestore(pane int) string {
	if pane == 0 {
		name := m.leftRestore
		m.leftRestore = ""
		return name
	}
	name := m.rightRestore
	m.rightRestore = ""
	return name
}

func (m *Model) clearRestore(pane int) {
	if pane == 0 {
		m.leftRestore = ""
	} else {
		m.rightRestore = ""
	}
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
// The returned command must be dispatched by the caller.
func (m *Model) updatePreview() tea.Cmd {
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor < len(files) {
		m.previewPath = files[cursor].Path
	} else {
		m.previewPath = ""
	}

	if m.previewPath == m.previewCachePath {
		return nil
	}

	// The cached preview belongs to another file. Drop it so the panel
	// shows something honest rather than the previous file's contents.
	m.previewContent = ui.Content{}
	m.previewCachePath = ""
	m.previewVersion++
	m.previewLoading = m.previewPath != ""

	if m.previewPath == "" {
		return nil
	}
	return loadPreviewCmd(m.previewPath, m.theme)
}

// loadPreviewCmd gathers a preview in the background.
func loadPreviewCmd(path string, theme config.Theme) tea.Cmd {
	return safeCmd("preview", func() tea.Msg {
		return previewLoadedMsg{path: path, content: ui.Load(path, theme)}
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
// Model is a value type, so the copy this method holds is untouched by the
// panic in update; returning it is exactly the state the user was in before
// the keypress that triggered the fault.
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
		m.previewContent = msg.content
		m.previewCachePath = msg.path
		m.previewVersion++
		m.previewLoading = false
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case dirLoadedMsg:
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		m.loadErr = nil
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
		// from; every other load starts at the top as before.
		if name := m.takeRestore(msg.pane); name != "" {
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
			m.finishCopy()
			m.loadErr = msg.err
			return m, tea.Batch(
				loadDirCmd(0, m.leftPath),
				loadDirCmd(1, m.rightPath),
			)
		}

		if msg.done {
			files := m.copyFiles
			m.finishCopy()
			m.opResult = fmt.Sprintf("copied %d file(s)", files)
			return m, tea.Batch(
				loadDirCmd(0, m.leftPath),
				loadDirCmd(1, m.rightPath),
			)
		}

		return m, stepCopyCmd(msg.copier, msg.gen)

	case fileOpMsg:
		if msg.err != nil {
			m.opResult = ""
			m.loadErr = msg.err
		} else {
			m.loadErr = nil
		}
		return m, tea.Batch(
			loadDirCmd(0, m.leftPath),
			loadDirCmd(1, m.rightPath),
		)

	case themeReloadedMsg:
		if msg.err != nil {
			// A closed watcher only happens on shutdown; keep the old theme
			// rather than blanking the UI.
			return m, nil
		}
		m.theme = msg.theme
		// Colours changed, so the highlighted preview must be rebuilt.
		// Clearing the cached path makes updatePreview load it again, this
		// time with the new palette.
		m.previewCachePath = ""
		// Re-arm: the previous wait command has returned, so listening must be
		// resumed or only the first reload would ever take effect.
		return m, tea.Batch(m.updatePreview(), m.waitThemeCmd())

	case openMsg:
		if msg.err != nil {
			m.loadErr = msg.err
			m.opResult = ""
		} else {
			m.loadErr = nil
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
		m.crashErr = nil

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
			if msg.String() == "esc" {
				return m.cancelCopy()
			}
			return m, nil
		}

		keyStr := msg.String()

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
			// The layout object lives in View, so the gate check builds a
			// throwaway one from the current size.
			if !ui.NewLayout(m.width, m.height, m.theme).CanStack() {
				m.opResult = "terminal too short to stack panes"
				return m, nil
			}
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
				newPath := files[cursor].Path
				m.setPath(m.activePane, newPath)
				return m, loadDirCmd(m.activePane, newPath)
			}
			if cursor < len(files) {
				return m, openCmd(files[cursor].Path, files[cursor].Name)
			}
			return m, nil
		case nav.ActionRefresh:
			return m, loadDirCmd(m.activePane, m.rawPath(m.activePane))
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

// handleKeyListKeys drives the key overlay, fuzzy-style: printable text
// feeds the query (matched against both the key and the description),
// arrows move the cursor with no side effects, backspace edits, and
// esc/shift+k closes. Enter does nothing — the panel is a reference, not a
// launcher.
func (m Model) handleKeyListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
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
		if r, ok := printableRune(msg.String()); ok {
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
	switch msg.String() {
	case "esc":
		m.clearFuzzy()
		return m, nil
	case "enter":
		if m.fuzzyCursor < len(m.fuzzyResults) {
			selected := m.fuzzyResults[m.fuzzyCursor]
			dir := filepath.Dir(selected)
			if m.activePane == 0 {
				m.leftPath = dir
			} else {
				m.rightPath = dir
			}
			m.clearFuzzy()
			return m, loadDirCmd(m.activePane, dir)
		}
		return m, nil
	case "j", "down":
		if m.fuzzyCursor < len(m.fuzzyResults)-1 {
			m.fuzzyCursor++
		}
		return m, nil
	case "k", "up":
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

	keyStr := msg.String()
	if r, ok := printableRune(keyStr); ok {
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
		currentPath := m.rawPath(m.activePane)
		parent := filepath.Dir(currentPath)
		if parent != currentPath {
			// Record the directory being left so the parent listing can
			// put the cursor back on it. The name comes from the path,
			// not the current listing, which may have changed since entry.
			m.setRestore(m.activePane, filepath.Base(currentPath))
			m.setPath(m.activePane, parent)
			return m, loadDirCmd(m.activePane, parent)
		}
		return m, nil
	case nav.ActionRight:
		// Entering a directory is a navigation step, but launching a file is
		// not: that belongs to enter alone, so an arrow press can never
		// unexpectedly hand a file to the desktop handler.
		files := m.visibleFiles(m.activePane)
		cursor := m.rawCursor(m.activePane)
		if cursor < len(files) && files[cursor].IsDir {
			newPath := files[cursor].Path
			m.setPath(m.activePane, newPath)
			return m, loadDirCmd(m.activePane, newPath)
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleInputKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "down", "left", "right":
		// Movement stays available while the prompt is open; filtering is
		// unaffected, so the user can walk the narrowed list and carry on
		// typing.
		if m.mode == inputFilter || m.mode == inputGoto {
			return m.handleNavAction(nav.Lookup(msg.String()))
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

	if r, ok := printableRune(msg.String()); ok {
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
			err := fs.CreateDir(path)
			m.opResult = resultMessage("created dir", name, err)
		} else {
			err := fs.CreateFile(path)
			m.opResult = resultMessage("created", name, err)
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
		m.opResult = resultMessage("renamed", name, err)
		if err != nil {
			m.loadErr = err
		}
		return m, m.loadActive()
	}

	m.mode = inputNone
	m.inputBuf = ""
	return m, nil
}

// commitGoto resolves the typed path and navigates the active pane to it.
// On invalid path, keeps the prompt open and sets loadErr for display.
func (m Model) commitGoto() (tea.Model, tea.Cmd) {
	path := m.inputBuf
	if path == "" {
		m.mode = inputNone
		m.inputBuf = ""
		return m, nil
	}

	expanded := expandPath(path, m.rawPath(m.activePane))

	if info, err := os.Stat(expanded); err == nil && info.IsDir() {
		m.mode = inputNone
		m.inputBuf = ""
		m.setPath(m.activePane, expanded)
		return m, loadDirCmd(m.activePane, expanded)
	}

	// Invalid - keep prompt open, show error
	m.loadErr = fmt.Errorf("not a directory: %s", path)
	return m, nil
}

func resultMessage(action, name string, err error) string {
	if err != nil {
		return err.Error()
	}
	return action + " " + name
}

func (m Model) loadActive() tea.Cmd {
	return loadDirCmd(m.activePane, m.rawPath(m.activePane))
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
// before moving them to the other pane with p.
func (m Model) handleMove() (tea.Model, tea.Cmd) {
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor >= len(files) {
		return m, nil
	}

	entry := files[cursor]
	if m.marked == nil {
		m.marked = make(map[string]fs.Entry)
	}

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
	m.copyGen++
}

// cancelCopy aborts an in-flight copy, removing the partially written file.
func (m Model) cancelCopy() (tea.Model, tea.Cmd) {
	if m.copier == nil {
		return m, nil
	}
	if err := m.copier.Abort(); err != nil {
		m.loadErr = err
	}
	m.finishCopy()
	m.opResult = "copy cancelled"
	return m, tea.Batch(
		loadDirCmd(0, m.leftPath),
		loadDirCmd(1, m.rightPath),
	)
}

// copyTotalBytes sums the size of every staged entry so the copy path can decide
// whether the work is large enough to warrant a progress bar.
func copyTotalBytes(files []fs.Entry) int64 {
	var total int64
	for _, f := range files {
		total += f.Size
	}
	return total
}

// handlePaste moves everything marked with m to the other pane, falling back to
// the single file staged by y when nothing is marked.
func (m Model) handlePaste() (tea.Model, tea.Cmd) {
	if m.copier != nil {
		m.opResult = "copy in progress"
		return m, nil
	}
	if len(m.marked) > 0 {
		return m.pasteMarks()
	}
	if !m.clipboardOn {
		return m, nil
	}

	targetPath := m.rawPath(1 - m.activePane)

	// Large copies run through the steppable copier so progress is visible and
	// the operation can be cancelled.
	if m.clipboard.Action == "copy" {
		if total := copyTotalBytes(m.clipboard.Files); total >= copyProgressThreshold {
			srcs := make([]string, 0, len(m.clipboard.Files))
			for _, f := range m.clipboard.Files {
				srcs = append(srcs, f.Path)
			}
			m.clipboardOn = false
			m.copyFiles = len(m.clipboard.Files)
			m.copyGen++
			return m, startCopyCmd(srcs, targetPath, m.copyGen)
		}
	}

	var cmds []tea.Cmd
	for _, file := range m.clipboard.Files {
		dst := filepath.Join(targetPath, file.Name)
		if m.clipboard.Action == "copy" {
			cmds = append(cmds, copyCmd(file.Path, dst))
		} else {
			cmds = append(cmds, moveCmd(file.Path, dst))
		}
	}

	m.clipboardOn = false
	m.opResult = fmt.Sprintf("pasted %d file(s)", len(m.clipboard.Files))
	return m, tea.Batch(cmds...)
}

// pasteMarks moves the marked set into the inactive pane. Entries that vanished
// from disk, or that already sit in the destination, are skipped so a stale
// mark cannot fail the whole batch.
func (m Model) pasteMarks() (tea.Model, tea.Cmd) {
	targetPath := m.rawPath(1 - m.activePane)

	var cmds []tea.Cmd
	moved, skipped := 0, 0
	for _, entry := range m.marked {
		if _, err := os.Stat(entry.Path); err != nil {
			skipped++
			continue
		}
		dst := filepath.Join(targetPath, entry.Name)
		if filepath.Clean(dst) == filepath.Clean(entry.Path) {
			skipped++
			continue
		}
		cmds = append(cmds, moveCmd(entry.Path, dst))
		moved++
	}

	m.marked = nil

	switch {
	case moved == 0:
		m.opResult = "nothing to move"
	case skipped > 0:
		m.opResult = fmt.Sprintf("moved %d, skipped %d", moved, skipped)
	default:
		m.opResult = fmt.Sprintf("moved %d file(s)", moved)
	}
	return m, tea.Batch(cmds...)
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
		m.opResult = "trashed " + filepath.Base(target)
		return m, trashCmd(target)
	case "f", "F":
		m.confirmDelete = false
		target := m.deleteTarget
		m.deleteTarget = ""
		m.opResult = "deleted " + filepath.Base(target)
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
		if r == nil {
			return
		}
		report := newCrashReport("view", r)
		// View cannot hand a model back, so a state created here would be
		// discarded. The constructor always sets one, so the history is kept
		// in every real session.
		m.crashStateFor().record(report)
		v = m.crashView(report)
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

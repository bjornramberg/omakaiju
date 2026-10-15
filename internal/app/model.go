package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/fuzzy"
	"github.com/bjornramberg/omakaiju/internal/nav"
	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

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

type fuzzyFilesLoadedMsg struct {
	files []string
	// root identifies which walk produced files. A walk from a directory the
	// user has since left can finish after the current one, and applying it
	// would replace the live results with a stale tree.
	root string
}

type Clipboard struct {
	Action string
	Files  []fs.Entry
	Source string
}

// inputMode is the active single-line text prompt. Filter, add, and rename all
// share one capture path: type to edit, enter commits, esc cancels.
type inputMode int

const (
	inputNone inputMode = iota
	inputFilter
	inputAdd
	inputRename
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
	// previewHidden hides the file preview pane and gives its width back to
	// the two navigation panes. It lives for the session only and resets on
	// launch, mirroring panesStacked.
	previewHidden bool
	// leftSort/rightSort hold the per-pane sort cycle:
	// name -> size↓ -> size↑ -> name. Zero value is SortByName (default).
	leftSort  fs.SortMode
	rightSort fs.SortMode
	leftPath     string
	leftFiles    []fs.Entry
	leftCursor   int
	rightPath    string
	rightFiles   []fs.Entry
	rightCursor  int
	loadErr      error

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

	// leftRestore/rightRestore hold the directory name the cursor should return
	// to after going up a level, so leaving and re-entering a folder keeps the
	// position instead of jumping to the top.
	leftRestore  string
	rightRestore string

	themeWatcher   *fs.Watcher
	previewPath    string
	previewFocused bool

	previewCache            []string
	previewCachePath        string
	previewHL               []string
	previewArchive          []fs.ArchiveEntry
	previewArchiveTruncated bool

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
	// fuzzySpinner is the current animation frame.
	fuzzySpinner int
	// fuzzySpinning records whether the animation loop is running. It only
	// starts once the user types, so a walk that finishes immediately does not
	// flash a spinner for results nobody has had time to see.
	fuzzySpinning bool

	leftFilter  string
	rightFilter string

	mode         inputMode
	inputBuf     string
	renameTarget string
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
		cfg:         cfg,
		theme:       theme,
		leftPath:    cwd,
		rightPath:   cwd,
		leftCursor:  0,
		rightCursor: 0,
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
	return func() tea.Msg {
		files, err := fs.ReadDir(path)
		return dirLoadedMsg{pane: pane, path: path, files: files, err: err}
	}
}

func copyCmd(src, dst string) tea.Cmd {
	return func() tea.Msg {
		err := fs.CopyRecursive(src, dst)
		return fileOpMsg{err: err}
	}
}

// copyProgressThreshold is the size below which a copy runs synchronously.
// Showing a bar for a handful of bytes only produces a one-frame flash, so
// small copies keep the simple path and just report a result.
const copyProgressThreshold = 1 << 20

// startCopyCmd builds the copier and performs the first step.
func startCopyCmd(srcs []string, dstDir string) tea.Cmd {
	return func() tea.Msg {
		copier, err := fs.NewCopier(srcs, dstDir)
		if err != nil {
			return fileOpMsg{err: err}
		}
		return copyProgressMsg{copier: copier}
	}
}

// stepCopyCmd advances the copier by one chunk.
func stepCopyCmd(copier *fs.Copier) tea.Cmd {
	return func() tea.Msg {
		done, err := copier.Step()
		return copyProgressMsg{copier: copier, done: done, err: err}
	}
}

func moveCmd(src, dst string) tea.Cmd {
	return func() tea.Msg {
		err := fs.Move(src, dst)
		return fileOpMsg{err: err}
	}
}

func deleteCmd(path string) tea.Cmd {
	return func() tea.Msg {
		err := fs.Delete(path)
		return fileOpMsg{err: err}
	}
}

func openCmd(path, name string) tea.Cmd {
	return func() tea.Msg {
		return openMsg{name: name, err: fs.OpenFile(path)}
	}
}

func trashCmd(path string) tea.Cmd {
	return func() tea.Msg {
		_, err := fs.Trash(path)
		return fileOpMsg{err: err}
	}
}

func loadFuzzyFilesCmd(root string) tea.Cmd {
	return func() tea.Msg {
		files, _ := fs.WalkDir(root)
		return fuzzyFilesLoadedMsg{files: files, root: root}
	}
}

// fuzzyTickCmd advances the loading spinner. The loop stops on its own: a tick
// that arrives once the walk has landed, the finder has been closed, or the
// root has moved on is simply dropped rather than reissued.
func fuzzyTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return fuzzyTickMsg{}
	})
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

	return func() tea.Msg {
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

			case err := <-watcher.Errors():
				if err != nil {
					return themeReloadedMsg{err: err}
				}
			}
		}
	}
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

func (m *Model) updatePreview() {
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor < len(files) {
		m.previewPath = files[cursor].Path
	} else {
		m.previewPath = ""
	}

	if m.previewPath != m.previewCachePath {
		m.previewCache = nil
		m.previewHL = nil
		m.previewArchive = nil
		m.previewArchiveTruncated = false
		m.previewCachePath = ""

		if m.previewPath != "" {
			fileType := fs.DetectFileType(m.previewPath)
			switch fileType {
			case fs.FileTypeText:
				lines, _ := fs.ReadFileHead(m.previewPath, 1000)
				// Clean at ingest so neither the highlighter nor the
				// preview cache ever holds raw bytes from the file.
				lines = ui.SanitizeLines(lines)
				m.previewCache = lines
				m.previewHL = ui.HighlightLines(m.previewPath, lines, m.theme)
				m.previewCachePath = m.previewPath
			case fs.FileTypeArchive:
				entries, truncated, err := fs.ListArchive(m.previewPath)
				if err == nil {
					// Cache raw entries; formatting is width-dependent and
					// must happen per render so resizes stay correct.
					m.previewArchive = entries
					m.previewArchiveTruncated = truncated
					m.previewCachePath = m.previewPath
				}
			}
		}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
		m.updatePreview()
		return m, nil

	case copyProgressMsg:
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

		return m, stepCopyCmd(msg.copier)

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
		m.previewHL = nil
		m.previewCachePath = ""
		m.updatePreview()
		// Re-arm: the previous wait command has returned, so listening must be
		// resumed or only the first reload would ever take effect.
		return m, m.waitThemeCmd()

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

	case fuzzyFilesLoadedMsg:
		// A walk started elsewhere, or from a session already abandoned, must
		// not replace the current results.
		if msg.root != m.fuzzyRoot {
			return m, nil
		}
		m.fuzzyAllFiles = msg.files
		m.fuzzyResults = fuzzy.Filter(m.fuzzyInput, msg.files)
		m.fuzzyLoading = false
		return m, nil

	case tea.KeyMsg:
		// A status message reports what just happened, so it stops being
		// current once the user acts. Clearing ahead of dispatching the key
		// means a message produced by this very keypress stays visible until
		// the next one.
		m.opResult = ""
		m.loadErr = nil

		if m.fuzzyActive {
			return m.handleFuzzyKeys(msg)
		}

		if m.mode != inputNone {
			return m.handleInputKeys(msg)
		}

		action := nav.Lookup(msg.String())
		// v must work everywhere typing does not depend on it: while the
		// preview is focused, during a copy, or under the delete
		// confirmation. Otherwise hiding is unreachable once i has focused
		// the preview and appears dead. It stays below the fuzzy/input
		// gates so typing v into those prompts still inserts text instead
		// of toggling.
		if action == nav.ActionTogglePreview {
			m.previewHidden = !m.previewHidden
			if m.previewHidden {
				// A hidden pane cannot stay focused; focusing again via i
				// unhides it (see below), so this stays reversible.
				m.previewFocused = false
			}
			return m, nil
		}

		if m.confirmDelete {
			return m.handleConfirmDelete(msg)
		}

		// An in-flight copy claims esc, but only once the modal prompts above
		// have had their chance (v is handled above so it stays reachable).
		if m.copier != nil {
			if msg.String() == "esc" {
				return m.cancelCopy()
			}
			return m, nil
		}

		if m.previewFocused {
			switch msg.String() {
			case "i", "I", "esc":
				m.previewFocused = false
				return m, nil
			}
			// Focus only highlights the preview border; every other key
			// falls through to normal handling so focus never locks
			// navigation, pane switching, or quit.
		}

		switch action {
		case nav.ActionQuit:
			if m.themeWatcher != nil {
				m.themeWatcher.Close()
			}
			return m, tea.Quit
		case nav.ActionTab:
			m.activePane = 1 - m.activePane
			m.updatePreview()
			return m, nil
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
			m.updatePreview()
			return m, nil
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
			return m, loadFuzzyFilesCmd(root)
		case nav.ActionFilter:
			m.mode = inputFilter
			m.inputBuf = ""
			m.setFilter(m.activePane, "")
			m.setCursor(m.activePane, 0)
			m.updatePreview()
			return m, nil
		case nav.ActionClear:
			m.setFilter(m.activePane, "")
			m.setCursor(m.activePane, 0)
			m.updatePreview()
			return m, nil
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
		case nav.ActionYank:
			return m.handleYank()
		case nav.ActionMove:
			return m.handleMove()
		case nav.ActionPaste:
			return m.handlePaste()
		case nav.ActionDelete:
			return m.handleDelete()
		}

		switch msg.String() {
		case "i", "I":
			if m.previewHidden {
				// Focusing a hidden pane unhides it so i always restores
				// what v hid instead of appearing to do nothing.
				m.previewHidden = false
			}
			m.previewFocused = true
			return m, nil
		}
	}
	return m, nil
}

// refilterFuzzy re-applies the query. While the walk is still running there is
// nothing to filter, so the query is simply held until it lands.
// refilterFuzzy re-applies the query and reports whether it started the
// loading animation, which the caller then has to schedule.
func (m *Model) refilterFuzzy() (started bool) {
	m.fuzzyCursor = 0
	if m.fuzzyLoading {
		// Nothing to filter yet. The animation only starts once there is a
		// query the user is actually waiting on.
		if m.fuzzyInput != "" && !m.fuzzySpinning {
			m.fuzzySpinning = true
			m.fuzzySpinner = 0
			return true
		}
		return false
	}
	m.fuzzyResults = fuzzy.Filter(m.fuzzyInput, m.fuzzyAllFiles)
	return false
}

// clearFuzzy drops the walk tree and filtered results and hands the memory
// back to the OS in the background. The finder is allowed to be expensive
// while it runs; nil-ing on close lets the GC reclaim the heap, and
// FreeOSMemory returns RSS promptly instead of leaving the footprint high.
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
	go debug.FreeOSMemory()
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
		if len(m.fuzzyInput) > 0 {
			m.fuzzyInput = m.fuzzyInput[:len(m.fuzzyInput)-1]
			if m.refilterFuzzy() {
				return m, fuzzyTickCmd()
			}
		}
		return m, nil
	}

	keyStr := msg.String()
	if len(keyStr) == 1 && keyStr[0] >= 32 && keyStr[0] < 127 {
		m.fuzzyInput += keyStr
		if m.refilterFuzzy() {
			return m, fuzzyTickCmd()
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
		m.updatePreview()
		return m, nil
	case nav.ActionDown:
		cursor := m.rawCursor(m.activePane)
		if cursor < len(m.visibleFiles(m.activePane))-1 {
			m.setCursor(m.activePane, cursor+1)
		}
		m.updatePreview()
		return m, nil
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
		if m.mode == inputFilter {
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
		m.updatePreview()
		return m, nil
	case "enter":
		return m.commitInput()
	case "backspace":
		if m.inputBuf != "" {
			m.inputBuf = m.inputBuf[:len(m.inputBuf)-1]
			if m.mode == inputFilter {
				m.setFilter(m.activePane, m.inputBuf)
			}
			m.setCursor(m.activePane, 0)
			m.clampCursor(m.activePane)
			m.updatePreview()
		}
		return m, nil
	}

	if key := msg.String(); len(key) == 1 && key[0] >= 32 && key[0] < 127 {
		m.inputBuf += key
		if m.mode == inputFilter {
			m.setFilter(m.activePane, m.inputBuf)
			m.setCursor(m.activePane, 0)
		}
		m.updatePreview()
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
func (m *Model) finishCopy() {
	m.copier = nil
	m.copyFiles = 0
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
			return m, startCopyCmd(srcs, targetPath)
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

func (m Model) View() tea.View {
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
		preview := ui.NewPreview(layout.PreviewWidth(), layout.MainAreaHeight(), m.theme)
		preview = preview.SetPath(m.previewPath)
		preview.Focused = m.previewFocused

		switch {
		case m.previewPath == "":
			previewRendered = preview.RenderMetadata("")
		case m.previewCache != nil:
			previewRendered = preview.RenderText(m.previewCache, m.previewHL)
		case m.previewArchive != nil:
			previewRendered = preview.RenderArchive(
				ui.ArchiveLines(m.previewArchive, m.previewArchiveTruncated, m.theme, preview.Width-4),
			)
		default:
			previewRendered = preview.RenderFile(m.previewPath, fs.DetectFileType(m.previewPath))
		}
	}

	bottomBar := ui.NewBottomBar(m.width, m.theme)
	bottomBar.PanesStacked = m.panesStacked
	bottomBar.PreviewHidden = m.previewHidden
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
	default:
		if m.copier != nil {
			progress := ui.NewProgress(m.copier, m.theme)
			bottomBar.Progress = &progress
		} else if m.confirmDelete {
			bottomBar.Input = filepath.Base(m.deleteTarget) + "? (t)rash / (f)orce / (n)o"
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
		view = ui.OverlayCentered(view, finder.Render(), m.width)
	}

	return tea.NewView(view)
}

package app

import (
	"os"
	"path/filepath"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
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

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		loadDirCmd(0, m.leftPath),
		loadDirCmd(1, m.rightPath),
		m.waitThemeCmd(),
		splashTickCmd(),
	)
}

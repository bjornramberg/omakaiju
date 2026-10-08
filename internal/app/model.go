package app

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"omakaiju/internal/config"
	"omakaiju/internal/fs"
	"omakaiju/internal/fuzzy"
	"omakaiju/internal/nav"
	"omakaiju/internal/ui"

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

type themeReloadedMsg struct {
	theme config.Theme
	err   error
}

type fuzzyFilesLoadedMsg struct {
	files []string
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

	activePane  int
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

	themeWatcher   *fs.Watcher
	previewPath    string
	previewFocused bool

	previewCache     []string
	previewCachePath string

	fuzzyActive   bool
	fuzzyInput    string
	fuzzyResults  []string
	fuzzyCursor   int
	fuzzyAllFiles []string

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

	return Model{
		cfg:         cfg,
		theme:       theme,
		leftPath:    cwd,
		rightPath:   cwd,
		leftCursor:  0,
		rightCursor: 0,
	}
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

func loadFuzzyFilesCmd(root string) tea.Cmd {
	return func() tea.Msg {
		files, _ := fs.WalkDir(root)
		return fuzzyFilesLoadedMsg{files: files}
	}
}

func (m Model) startThemeWatcher() tea.Cmd {
	return func() tea.Msg {
		watcher, err := fs.NewWatcher()
		if err != nil {
			return themeReloadedMsg{err: err}
		}

		themeDir := filepath.Dir(m.cfg.ThemePath)
		if err := watcher.Watch(themeDir); err != nil {
			return themeReloadedMsg{err: err}
		}

		m.themeWatcher = watcher

		for {
			select {
			case event := <-watcher.Events():
				if event.Name == m.cfg.ThemePath && (event.Has(fsnotify.Write) || event.Has(fsnotify.Create)) {
					time.Sleep(100 * time.Millisecond)
					theme, err := config.LoadTheme(m.cfg.ThemePath)
					return themeReloadedMsg{theme: theme, err: err}
				}
			case err := <-watcher.Errors():
				return themeReloadedMsg{err: err}
			}
		}
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		loadDirCmd(0, m.leftPath),
		loadDirCmd(1, m.rightPath),
		m.startThemeWatcher(),
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

func (m Model) visibleFiles(pane int) []fs.Entry {
	return nav.FilterEntries(m.rawFiles(pane), m.filterOf(pane))
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
		m.previewCachePath = ""

		if m.previewPath != "" {
			fileType := fs.DetectFileType(m.previewPath)
			if fileType == fs.FileTypeText {
				lines, _ := fs.ReadFileHead(m.previewPath, 1000)
				m.previewCache = lines
				m.previewCachePath = m.previewPath
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
		m.updatePreview()
		return m, nil

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
			return m, nil
		}
		m.theme = msg.theme
		return m, nil

	case fuzzyFilesLoadedMsg:
		m.fuzzyAllFiles = msg.files
		m.fuzzyResults = fuzzy.Filter(m.fuzzyInput, msg.files)
		return m, nil

	case tea.KeyMsg:
		if m.confirmDelete {
			return m.handleConfirmDelete(msg)
		}

		if m.fuzzyActive {
			return m.handleFuzzyKeys(msg)
		}

		if m.mode != inputNone {
			return m.handleInputKeys(msg)
		}

		if m.previewFocused {
			switch msg.String() {
			case "i", "esc":
				m.previewFocused = false
				return m, nil
			}
			return m, nil
		}

		action := nav.Lookup(msg.String())
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
		case nav.ActionDown:
			cursor := m.rawCursor(m.activePane)
			if cursor < len(m.visibleFiles(m.activePane))-1 {
				m.setCursor(m.activePane, cursor+1)
			}
			m.updatePreview()
			return m, nil
		case nav.ActionUp:
			if cursor := m.rawCursor(m.activePane); cursor > 0 {
				m.setCursor(m.activePane, cursor-1)
			}
			m.updatePreview()
			return m, nil
		case nav.ActionRight, nav.ActionEnter:
			files := m.visibleFiles(m.activePane)
			cursor := m.rawCursor(m.activePane)
			if cursor < len(files) && files[cursor].IsDir {
				newPath := files[cursor].Path
				m.setPath(m.activePane, newPath)
				return m, loadDirCmd(m.activePane, newPath)
			}
			return m, nil
		case nav.ActionLeft:
			currentPath := m.rawPath(m.activePane)
			parent := filepath.Dir(currentPath)
			if parent != currentPath {
				m.setPath(m.activePane, parent)
				return m, loadDirCmd(m.activePane, parent)
			}
			return m, nil
		case nav.ActionRefresh:
			return m, loadDirCmd(m.activePane, m.rawPath(m.activePane))
		case nav.ActionFuzzyFind:
			m.fuzzyActive = true
			m.fuzzyInput = ""
			m.fuzzyResults = nil
			m.fuzzyCursor = 0
			return m, loadFuzzyFilesCmd(m.rawPath(m.activePane))
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

		if msg.String() == "i" {
			m.previewFocused = true
			return m, nil
		}
	}
	return m, nil
}

func (m Model) handleFuzzyKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.fuzzyActive = false
		m.fuzzyInput = ""
		m.fuzzyResults = nil
		m.fuzzyCursor = 0
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
			m.fuzzyActive = false
			m.fuzzyInput = ""
			m.fuzzyResults = nil
			m.fuzzyCursor = 0
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
			m.fuzzyResults = fuzzy.Filter(m.fuzzyInput, m.fuzzyAllFiles)
			m.fuzzyCursor = 0
		}
		return m, nil
	}

	keyStr := msg.String()
	if len(keyStr) == 1 && keyStr[0] >= 32 && keyStr[0] < 127 {
		m.fuzzyInput += keyStr
		m.fuzzyResults = fuzzy.Filter(m.fuzzyInput, m.fuzzyAllFiles)
		m.fuzzyCursor = 0
	}
	return m, nil
}

func (m Model) handleInputKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
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

func (m Model) handleMove() (tea.Model, tea.Cmd) {
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor >= len(files) {
		return m, nil
	}
	m.clipboard = Clipboard{
		Action: "move",
		Files:  []fs.Entry{files[cursor]},
		Source: m.rawPath(m.activePane),
	}
	m.clipboardOn = true
	m.opResult = "moved " + files[cursor].Name
	return m, nil
}

func (m Model) handlePaste() (tea.Model, tea.Cmd) {
	if !m.clipboardOn {
		return m, nil
	}

	targetPath := m.rawPath(1 - m.activePane)

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
	m.opResult = "pasted " + string(rune(len(m.clipboard.Files))) + " file(s)"
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
	case "y", "Y":
		m.confirmDelete = false
		target := m.deleteTarget
		m.deleteTarget = ""
		m.opResult = "deleted"
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
	if m.fuzzyActive {
		finder := ui.NewFuzzyFinder(m.width, m.height, m.theme)
		finder.Input = m.fuzzyInput
		finder.Results = m.fuzzyResults
		finder.Cursor = m.fuzzyCursor
		finder.AllFiles = m.fuzzyAllFiles
		return tea.NewView(finder.Render())
	}

	layout := ui.NewLayout(m.width, m.height, m.theme)

	topBar := ui.NewTopBar(m.width, m.theme)
	topBar.Filter = m.filterOf(m.activePane)

	leftPane := ui.NewPane(layout.PaneWidth(), layout.MainAreaHeight(), m.activePane == 0, m.theme)
	leftPane.Path = m.leftPath
	leftPane.Filter = m.leftFilter
	leftPane.Files = m.visibleFiles(0)
	leftPane.Cursor = m.leftCursor
	leftPane.TotalFiles = len(m.leftFiles)
	leftRendered := leftPane.Render()

	rightPane := ui.NewPane(layout.PaneWidth(), layout.MainAreaHeight(), m.activePane == 1, m.theme)
	rightPane.Path = m.rightPath
	rightPane.Filter = m.rightFilter
	rightPane.Files = m.visibleFiles(1)
	rightPane.Cursor = m.rightCursor
	rightPane.TotalFiles = len(m.rightFiles)
	rightRendered := rightPane.Render()

	var previewRendered string
	if m.previewPath != "" && m.previewCache != nil {
		preview := ui.NewPreview(layout.PreviewWidth(), layout.MainAreaHeight(), m.theme)
		preview = preview.SetPath(m.previewPath)
		previewRendered = preview.RenderText(m.previewCache)
	} else if m.previewPath != "" {
		fileType := fs.DetectFileType(m.previewPath)
		preview := ui.NewPreview(layout.PreviewWidth(), layout.MainAreaHeight(), m.theme)
		previewRendered = preview.RenderFile(m.previewPath, fileType)
	} else {
		preview := ui.NewPreview(layout.PreviewWidth(), layout.MainAreaHeight(), m.theme)
		previewRendered = preview.RenderMetadata("")
	}

	if m.previewFocused {
		previewRendered = m.theme.PreviewPanelFocused().
			Width(layout.PreviewWidth()).
			Height(layout.MainAreaHeight()).
			Render(previewRendered)
	}

	bottomBar := ui.NewBottomBar(m.width, m.theme)
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
		if m.confirmDelete {
			bottomBar.Input = "delete " + filepath.Base(m.deleteTarget) + "? (y/n)"
		} else if m.loadErr != nil {
			bottomBar.Error = m.loadErr.Error()
		} else if m.opResult != "" {
			bottomBar.OpResult = m.opResult
		}
	}
	bottomRendered := bottomBar.Render()

	view := layout.Render(topBar.Render(), leftRendered, rightRendered, previewRendered, bottomRendered)

	return tea.NewView(view)
}

package app

import (
	"os"
	"path/filepath"
	"time"

	"omakaiju/internal/config"
	"omakaiju/internal/fs"
	"omakaiju/internal/fuzzy"
	"omakaiju/internal/nav"
	"omakaiju/internal/ui"

	"github.com/fsnotify/fsnotify"
	tea "charm.land/bubbletea/v2"
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

	themeWatcher  *fs.Watcher
	previewPath   string
	previewFocused bool

	previewCache     []string
	previewCachePath string

	fuzzyActive   bool
	fuzzyInput    string
	fuzzyResults  []string
	fuzzyCursor   int
	fuzzyAllFiles []string
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
		cfg:        cfg,
		theme:      theme,
		leftPath:   cwd,
		rightPath:  cwd,
		leftCursor: 0,
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

func (m *Model) updatePreview() {
	var files []fs.Entry
	var cursor int
	if m.activePane == 0 {
		files = m.leftFiles
		cursor = m.leftCursor
	} else {
		files = m.rightFiles
		cursor = m.rightCursor
	}
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
		} else {
			m.rightPath = msg.path
			m.rightFiles = msg.files
			m.rightCursor = 0
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
			if m.activePane == 0 {
				if m.leftCursor < len(m.leftFiles)-1 {
					m.leftCursor++
				}
			} else {
				if m.rightCursor < len(m.rightFiles)-1 {
					m.rightCursor++
				}
			}
			m.updatePreview()
			return m, nil
		case nav.ActionUp:
			if m.activePane == 0 {
				if m.leftCursor > 0 {
					m.leftCursor--
				}
			} else {
				if m.rightCursor > 0 {
					m.rightCursor--
				}
			}
			m.updatePreview()
			return m, nil
		case nav.ActionRight, nav.ActionEnter:
			var files []fs.Entry
			var cursor int
			if m.activePane == 0 {
				files = m.leftFiles
				cursor = m.leftCursor
			} else {
				files = m.rightFiles
				cursor = m.rightCursor
			}
			if cursor < len(files) && files[cursor].IsDir {
				newPath := files[cursor].Path
				if m.activePane == 0 {
					m.leftPath = newPath
				} else {
					m.rightPath = newPath
				}
				return m, loadDirCmd(m.activePane, newPath)
			}
			return m, nil
		case nav.ActionLeft:
			var currentPath string
			if m.activePane == 0 {
				currentPath = m.leftPath
			} else {
				currentPath = m.rightPath
			}
			parent := filepath.Dir(currentPath)
			if parent != currentPath {
				if m.activePane == 0 {
					m.leftPath = parent
				} else {
					m.rightPath = parent
				}
				return m, loadDirCmd(m.activePane, parent)
			}
			return m, nil
		case nav.ActionRefresh:
			if m.activePane == 0 {
				return m, loadDirCmd(0, m.leftPath)
			}
			return m, loadDirCmd(1, m.rightPath)
		case nav.ActionFuzzyFind:
			m.fuzzyActive = true
			m.fuzzyInput = ""
			m.fuzzyResults = nil
			m.fuzzyCursor = 0
			var currentPath string
			if m.activePane == 0 {
				currentPath = m.leftPath
			} else {
				currentPath = m.rightPath
			}
			return m, loadFuzzyFilesCmd(currentPath)
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

func (m Model) handleYank() (tea.Model, tea.Cmd) {
	var files []fs.Entry
	var cursor int
	var currentPath string
	if m.activePane == 0 {
		files = m.leftFiles
		cursor = m.leftCursor
		currentPath = m.leftPath
	} else {
		files = m.rightFiles
		cursor = m.rightCursor
		currentPath = m.rightPath
	}
	if cursor >= len(files) {
		return m, nil
	}
	m.clipboard = Clipboard{
		Action: "copy",
		Files:  []fs.Entry{files[cursor]},
		Source: currentPath,
	}
	m.clipboardOn = true
	m.opResult = "yanked " + files[cursor].Name
	return m, nil
}

func (m Model) handleMove() (tea.Model, tea.Cmd) {
	var files []fs.Entry
	var cursor int
	var currentPath string
	if m.activePane == 0 {
		files = m.leftFiles
		cursor = m.leftCursor
		currentPath = m.leftPath
	} else {
		files = m.rightFiles
		cursor = m.rightCursor
		currentPath = m.rightPath
	}
	if cursor >= len(files) {
		return m, nil
	}
	m.clipboard = Clipboard{
		Action: "move",
		Files:  []fs.Entry{files[cursor]},
		Source: currentPath,
	}
	m.clipboardOn = true
	m.opResult = "moved " + files[cursor].Name
	return m, nil
}

func (m Model) handlePaste() (tea.Model, tea.Cmd) {
	if !m.clipboardOn {
		return m, nil
	}

	var targetPath string
	if m.activePane == 0 {
		targetPath = m.rightPath
	} else {
		targetPath = m.leftPath
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
	m.opResult = "pasted " + string(rune(len(m.clipboard.Files))) + " file(s)"
	return m, tea.Batch(cmds...)
}

func (m Model) handleDelete() (tea.Model, tea.Cmd) {
	var files []fs.Entry
	var cursor int
	if m.activePane == 0 {
		files = m.leftFiles
		cursor = m.leftCursor
	} else {
		files = m.rightFiles
		cursor = m.rightCursor
	}
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

	topBar := ui.NewTopBar(m.width, m.theme).Render()

	leftPane := ui.NewPane(layout.PaneWidth(), layout.MainAreaHeight(), m.activePane == 0, m.theme)
	leftPane.Path = m.leftPath
	leftPane.Files = m.leftFiles
	leftPane.Cursor = m.leftCursor
	leftRendered := leftPane.Render()

	rightPane := ui.NewPane(layout.PaneWidth(), layout.MainAreaHeight(), m.activePane == 1, m.theme)
	rightPane.Path = m.rightPath
	rightPane.Files = m.rightFiles
	rightPane.Cursor = m.rightCursor
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
	if m.confirmDelete {
		bottomBar.Input = "delete " + filepath.Base(m.deleteTarget) + "? (y/n)"
	} else {
		if m.loadErr != nil {
			bottomBar.Error = m.loadErr.Error()
		} else if m.opResult != "" {
			bottomBar.OpResult = m.opResult
		}
	}
	bottomRendered := bottomBar.Render()

	view := layout.Render(topBar, leftRendered, rightRendered, previewRendered, bottomRendered)

	return tea.NewView(view)
}

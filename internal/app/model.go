package app

import (
	"os"
	"path/filepath"

	"omakaiju/internal/config"
	"omakaiju/internal/fs"
	"omakaiju/internal/nav"
	"omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

type dirLoadedMsg struct {
	pane  int
	path  string
	files []fs.Entry
	err   error
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

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		loadDirCmd(0, m.leftPath),
		loadDirCmd(1, m.rightPath),
	)
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
		return m, nil

	case tea.KeyMsg:
		action := nav.Lookup(msg.String())
		switch action {
		case nav.ActionQuit:
			return m, tea.Quit
		case nav.ActionTab:
			m.activePane = 1 - m.activePane
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
		}
	}
	return m, nil
}

func (m Model) View() tea.View {
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

	bottomBar := ui.NewBottomBar(m.width, m.theme)
	if m.loadErr != nil {
		bottomBar.Error = m.loadErr.Error()
	}
	bottomRendered := bottomBar.Render()

	view := layout.Render(topBar, leftRendered, rightRendered, bottomRendered)

	return tea.NewView(view)
}

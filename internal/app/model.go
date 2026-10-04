package app

import (
	"omakaiju/internal/config"
	"omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

type Model struct {
	cfg    config.Config
	theme  config.Theme
	width  int
	height int
}

func NewModel(cfg config.Config) Model {
	theme, err := config.LoadTheme(cfg.ThemePath)
	if err != nil {
		theme = config.DefaultTheme()
	}

	return Model{
		cfg:   cfg,
		theme: theme,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) View() tea.View {
	layout := ui.NewLayout(m.width, m.height, m.theme)

	topBar := ui.NewTopBar(m.width, m.theme).Render()

	leftPane := ui.NewPane(layout.PaneWidth(), layout.MainAreaHeight(), true, m.theme)
	leftPane.Path = "/home/user"
	leftPane.Files = nil
	leftPane.Cursor = 0
	leftRendered := leftPane.Render()

	rightPane := ui.NewPane(layout.PaneWidth(), layout.MainAreaHeight(), false, m.theme)
	rightPane.Path = "/home/user/projects"
	rightPane.Files = nil
	rightPane.Cursor = 0
	rightRendered := rightPane.Render()

	bottomBar := ui.NewBottomBar(m.width, m.theme).Render()

	view := layout.Render(topBar, leftRendered, rightRendered, bottomBar)

	return tea.NewView(view)
}

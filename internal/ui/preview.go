package ui

import (
	"fmt"

	"omakaiju/internal/config"
)

type Preview struct {
	Width  int
	Height int
	Theme  config.Theme
}

func NewPreview(width, height int, theme config.Theme) Preview {
	return Preview{Width: width, Height: height, Theme: theme}
}

func (p Preview) Render(content string) string {
	return p.Theme.PreviewPanel().
		Width(p.Width).
		Height(p.Height).
		Render(content)
}

func (p Preview) RenderMetadata(name string, size int64, modTime int64) string {
	header := p.Theme.AccentText().Bold(true).Render(name)
	info := p.Theme.StatusText().Render(fmt.Sprintf("Size: %d bytes", size))
	return p.Theme.PreviewPanel().
		Width(p.Width).
		Height(p.Height).
		Render(header + "\n\n" + info)
}

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"omakaiju/internal/config"
	"omakaiju/internal/fs"
)

type Preview struct {
	Width  int
	Height int
	Theme  config.Theme
	path   string
}

func NewPreview(width, height int, theme config.Theme) Preview {
	return Preview{Width: width, Height: height, Theme: theme}
}

func (p Preview) SetPath(path string) Preview {
	p.path = path
	return p
}

func (p Preview) RenderFile(path string, fileType fs.FileType) string {
	switch fileType {
	case fs.FileTypeText:
		return p.RenderTextFromFile(path)
	case fs.FileTypeBinary:
		return p.RenderBinary(path)
	case fs.FileTypeImage:
		return p.RenderImage(path)
	case fs.FileTypeArchive:
		return p.RenderArchive(path)
	case fs.FileTypeDirectory:
		return p.RenderDirectory(path)
	default:
		return p.RenderMetadata(path)
	}
}

func (p Preview) RenderText(lines []string) string {
	chrome := 8
	maxVisible := p.Height - chrome
	if maxVisible < 1 {
		maxVisible = 1
	}

	totalLines := len(lines)
	visibleLines := lines
	truncated := false
	if len(lines) > maxVisible {
		visibleLines = lines[:maxVisible]
		truncated = true
	}

	header := p.Theme.AccentText().Bold(true).Render(filepath.Base(p.path))
	content := p.Theme.Text().Render(strings.Join(visibleLines, "\n"))

	footer := ""
	if truncated {
		footer = p.Theme.StatusText().Render(fmt.Sprintf("... (%d more lines)", totalLines-maxVisible))
	} else {
		footer = p.Theme.StatusText().Render(fmt.Sprintf("%d lines", totalLines))
	}

	return p.Theme.PreviewPanel().
		Width(p.Width).
		MaxHeight(p.Height).
		Render(header + "\n\n" + content + "\n\n" + footer)
}

func (p Preview) RenderTextFromFile(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return p.RenderError(err)
	}

	maxLines := p.Height - 8
	if info.Size() > 1024*1024 {
		maxLines = 100
	}

	lines, err := fs.ReadFileHead(path, maxLines)
	if err != nil {
		return p.RenderError(err)
	}

	p.path = path
	return p.RenderText(lines)
}

func (p Preview) RenderBinary(path string) string {
	header := p.Theme.AccentText().Bold(true).Render(filepath.Base(path))
	hexDump := p.generateHexDump(path, 256)
	footer := p.Theme.StatusText().Render("binary file")

	return p.Theme.PreviewPanel().
		Width(p.Width).
		MaxHeight(p.Height).
		Render(header + "\n\n" + hexDump + "\n\n" + footer)
}

func (p Preview) RenderImage(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return p.RenderError(err)
	}

	header := p.Theme.AccentText().Bold(true).Render(filepath.Base(path))
	size := p.Theme.StatusText().Render(fmt.Sprintf("Size: %d bytes", info.Size()))
	footer := p.Theme.StatusText().Render("image preview not available")

	return p.Theme.PreviewPanel().
		Width(p.Width).
		MaxHeight(p.Height).
		Render(header + "\n\n" + size + "\n\n" + footer)
}

func (p Preview) RenderArchive(path string) string {
	header := p.Theme.AccentText().Bold(true).Render(filepath.Base(path))
	footer := p.Theme.StatusText().Render("archive contents not available")

	return p.Theme.PreviewPanel().
		Width(p.Width).
		MaxHeight(p.Height).
		Render(header + "\n\n" + footer)
}

func (p Preview) RenderDirectory(path string) string {
	entries, err := fs.ReadDir(path)
	if err != nil {
		return p.RenderError(err)
	}

	header := p.Theme.AccentText().Bold(true).Render(filepath.Base(path) + "/")
	count := p.Theme.StatusText().Render(fmt.Sprintf("%d items", len(entries)))

	return p.Theme.PreviewPanel().
		Width(p.Width).
		MaxHeight(p.Height).
		Render(header + "\n\n" + count)
}

func (p Preview) RenderMetadata(path string) string {
	if path == "" {
		return p.Theme.PreviewPanel().
			Width(p.Width).
			MaxHeight(p.Height).
			Render(p.Theme.StatusText().Render("no file selected"))
	}

	info, err := os.Stat(path)
	if err != nil {
		return p.RenderError(err)
	}

	header := p.Theme.AccentText().Bold(true).Render(filepath.Base(path))
	size := p.Theme.StatusText().Render(fmt.Sprintf("Size: %d bytes", info.Size()))
	perms := p.Theme.StatusText().Render(fmt.Sprintf("Permissions: %s", info.Mode()))
	modTime := p.Theme.StatusText().Render(fmt.Sprintf("Modified: %s", info.ModTime().Format("2006-01-02 15:04:05")))

	return p.Theme.PreviewPanel().
		Width(p.Width).
		MaxHeight(p.Height).
		Render(header + "\n\n" + size + "\n" + perms + "\n" + modTime)
}

func (p Preview) RenderError(err error) string {
	return p.Theme.ErrorText().
		Width(p.Width).
		MaxHeight(p.Height).
		Render("Error: " + err.Error())
}

func (p Preview) generateHexDump(path string, maxBytes int) string {
	f, err := os.Open(path)
	if err != nil {
		return p.Theme.ErrorText().Render("Error reading file")
	}
	defer f.Close()

	buf := make([]byte, maxBytes)
	n, _ := f.Read(buf)

	var sb strings.Builder
	for i := 0; i < n; i += 16 {
		end := i + 16
		if end > n {
			end = n
		}

		hexPart := ""
		asciiPart := ""
		for j := i; j < end; j++ {
			hexPart += fmt.Sprintf("%02x ", buf[j])
			if buf[j] >= 32 && buf[j] < 127 {
				asciiPart += string(buf[j])
			} else {
				asciiPart += "."
			}
		}

		sb.WriteString(fmt.Sprintf("%08x  %-48s  %s\n", i, hexPart, asciiPart))
	}

	return p.Theme.Text().Render(sb.String())
}

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

// ContentWidth is the usable width inside the panel border and padding. Body
// lines must be clipped to this or they spill past the border into the
// neighbouring pane.
func (p Preview) ContentWidth() int {
	w := p.Width - 2 /*border*/ - 4 /*padding*/
	if w < 1 {
		return 1
	}
	return w
}

// clipLines bounds every line to maxCells, preserving line count so the height
// arithmetic and footer stay correct. Clipping rather than wrapping is what
// keeps one source line equal to one rendered row.
func clipLines(lines []string, maxCells int) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = TruncateEnd(l, maxCells)
	}
	return out
}

// panel renders the preview frame with a MaxWidth backstop, so even a
// miscalculated budget cannot bleed into the adjacent pane.
func (p Preview) panel(content string) string {
	return p.Theme.PreviewPanel().
		Width(p.Width).
		MaxWidth(p.Width).
		MaxHeight(p.Height).
		Render(content)
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
		entries, truncated, err := fs.ListArchive(path)
		if err != nil {
			return p.ArchiveNotice(path, archiveMessage(err))
		}
		return p.RenderArchive(ArchiveLines(entries, truncated, p.Theme, p.ContentWidth()))
	case fs.FileTypeDirectory:
		return p.RenderDirectory(path)
	default:
		return p.RenderMetadata(path)
	}
}

// RenderText renders pre-highlighted content. lines carries the raw source for
// counting and highlighted carries the colourised text; when highlighted is
// empty the raw lines are shown unhighlighted.
func (p Preview) RenderText(lines, highlighted []string) string {
	chrome := 8
	maxVisible := p.Height - chrome
	if maxVisible < 1 {
		maxVisible = 1
	}

	body := highlighted
	if len(body) == 0 {
		body = lines
	}

	totalLines := len(body)
	if len(body) > maxVisible {
		body = body[:maxVisible]
	}
	// Clip before joining: the panel has no MaxWidth of its own on the text
	// body, so an over-long line would wrap and bleed into the next pane.
	body = clipLines(body, p.ContentWidth())

	header := p.Theme.AccentText().Bold(true).Render(filepath.Base(p.path))
	// Rendered without a wrapping foreground style so the inner ANSI colours
	// from Chroma are not overridden.
	content := strings.Join(body, "\n")

	footer := ""
	if totalLines > maxVisible {
		footer = p.Theme.StatusText().Render(fmt.Sprintf("... (%d more lines)", totalLines-maxVisible))
	} else {
		footer = p.Theme.StatusText().Render(fmt.Sprintf("%d lines", totalLines))
	}

	return p.panel(header + "\n\n" + content + "\n\n" + footer)
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
	highlighted := splitLines(Highlight(path, strings.Join(lines, "\n"), OmarchyStyle(p.Theme)))
	return p.RenderText(lines, highlighted)
}

// splitLines splits highlighted output into lines for height-bounded rendering.
// The formatter emits a style reset at each newline, so slicing by line keeps
// escape sequences intact.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func (p Preview) RenderBinary(path string) string {
	header := p.Theme.AccentText().Bold(true).Render(filepath.Base(path))
	hexDump := p.generateHexDump(path, 256)
	footer := p.Theme.StatusText().Render("binary file")

	return p.panel(header + "\n\n" + hexDump + "\n\n" + footer)
}

func (p Preview) RenderImage(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return p.RenderError(err)
	}

	header := p.Theme.AccentText().Bold(true).Render(filepath.Base(path))
	size := p.Theme.StatusText().Render(fmt.Sprintf("Size: %d bytes", info.Size()))
	footer := p.Theme.StatusText().Render("image preview not available")

	return p.panel(header + "\n\n" + size + "\n\n" + footer)
}

// RenderArchive lists the contents of an archive. Entries are pre-formatted by
// the caller and cached, so this only truncates and frames them.
func (p Preview) RenderArchive(lines []string) string {
	chrome := 8
	maxVisible := p.Height - chrome
	if maxVisible < 1 {
		maxVisible = 1
	}

	totalLines := len(lines)
	body := lines
	if len(body) > maxVisible {
		body = body[:maxVisible]
	}

	header := p.Theme.AccentText().Bold(true).Render(filepath.Base(p.path))
	content := strings.Join(body, "\n")

	var footer string
	switch {
	case totalLines > maxVisible:
		footer = p.Theme.StatusText().Render(fmt.Sprintf("... (%d more entries)", totalLines-maxVisible))
	case totalLines == 1:
		footer = p.Theme.StatusText().Render("1 entry")
	default:
		footer = p.Theme.StatusText().Render(fmt.Sprintf("%d entries", totalLines))
	}

	return p.panel(header + "\n\n" + content + "\n\n" + footer)
}

// ArchiveNotice frames a non-listable archive message in the preview panel.
func (p Preview) ArchiveNotice(path, message string) string {
	header := p.Theme.AccentText().Bold(true).Render(filepath.Base(path))
	return p.panel(header + "\n\n" + p.Theme.StatusText().Render(message))
}

func (p Preview) RenderDirectory(path string) string {
	entries, err := fs.ReadDir(path)
	if err != nil {
		return p.RenderError(err)
	}

	header := p.Theme.AccentText().Bold(true).Render(filepath.Base(path) + "/")
	count := p.Theme.StatusText().Render(fmt.Sprintf("%d items", len(entries)))

	return p.panel(header + "\n\n" + count)
}

func (p Preview) RenderMetadata(path string) string {
	if path == "" {
		return p.panel(p.Theme.StatusText().Render("no file selected"))
	}

	info, err := os.Stat(path)
	if err != nil {
		return p.RenderError(err)
	}

	header := p.Theme.AccentText().Bold(true).Render(filepath.Base(path))
	size := p.Theme.StatusText().Render(fmt.Sprintf("Size: %d bytes", info.Size()))
	perms := p.Theme.StatusText().Render(fmt.Sprintf("Permissions: %s", info.Mode()))
	modTime := p.Theme.StatusText().Render(fmt.Sprintf("Modified: %s", info.ModTime().Format("2006-01-02 15:04:05")))

	return p.panel(header + "\n\n" + size + "\n" + perms + "\n" + modTime)
}

func (p Preview) RenderError(err error) string {
	return p.panel("Error: " + err.Error())
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

	// A hex row is a fixed 76 cells, which is wider than a narrow preview
	// column, so clip it before it reaches the frame.
	return p.Theme.Text().Render(strings.Join(clipLines(
		strings.Split(sb.String(), "\n"), p.ContentWidth()), "\n"))
}

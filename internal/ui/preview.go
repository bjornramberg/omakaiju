package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
)

type Preview struct {
	Width  int
	Height int
	Theme  config.Theme
	path   string
	// Focused selects the accent frame. It swaps the panel style rather than
	// wrapping the rendered panel in a second border, which would add rows and
	// columns and overflow the layout.
	Focused bool
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

// header renders the preview title, clipped to the panel so a long filename
// cannot overflow the border on a narrow column.
func (p Preview) header(name string) string {
	return p.Theme.AccentText().Bold(true).Render(TruncateName(SanitizeDisplay(name), p.ContentWidth()))
}

// panel renders the preview frame with a MaxWidth backstop, so even a
// miscalculated budget cannot bleed into the adjacent pane.
//
// Height pads a short preview out to its allocation, the way the file
// panes do, so a panel with little to say — "loading…", or a file's
// metadata — still has the full frame rather than a stub with a border
// around three rows. MaxHeight clips one that somehow still runs long.
func (p Preview) panel(content string) string {
	if p.Focused {
		return p.Theme.PreviewPanelFocused().
			Width(p.Width).
			MaxWidth(p.Width).
			Height(p.Height).
			MaxHeight(p.Height).
			Render(content)
	}
	return p.Theme.PreviewPanel().
		Width(p.Width).
		MaxWidth(p.Width).
		Height(p.Height).
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

// RenderContent draws a preview from content that was gathered away from the
// render path.
//
// Nothing here touches the disk. The panel is a renderer now: it frames
// what it is given, and the reading and the colouring happen in the
// background where a slow disk cannot stall the cursor.
func (p Preview) RenderContent(c Content) string {
	switch c.Kind {
	case KindEmpty:
		return p.panel(p.Theme.StatusText().Render("no file selected"))
	case KindLoading:
		return p.panel(p.Theme.StatusText().Render("loading…"))
	case KindError:
		return p.RenderError(c.Err)
	case KindText:
		return p.renderText(c.Text, c.Highlighted, c.TextTruncated)
	case KindBinary:
		return p.renderBinaryContent(c)
	case KindImage:
		return p.renderImageContent(c)
	case KindArchive:
		return p.RenderArchive(ArchiveLines(c.Archive, c.ArchiveTruncated, p.Theme, p.ContentWidth()))
	case KindDirectory:
		return p.renderDirectoryContent(c)
	default:
		return p.renderMetadataContent(c)
	}
}

// renderBinaryContent frames a hex dump that was produced elsewhere.
//
// A hex row is a fixed 76 cells, which is wider than a narrow preview
// column, so each row is clipped before it reaches the frame. Clipping
// happens here rather than where the dump was built because it depends on
// the panel's width, and the panel can be resized without the preview
// being reloaded.
func (p Preview) renderBinaryContent(c Content) string {
	header := p.header(filepath.Base(p.path))
	hex := p.Theme.Text().Render(strings.Join(
		clipLines(strings.Split(c.Hex, "\n"), p.ContentWidth()), "\n"))
	footer := p.Theme.StatusText().Render("binary file")
	return p.panel(header + "\n\n" + hex + "\n\n" + footer)
}

// renderImageContent frames an image's metadata. Images are not rendered as
// pixels; the panel says what the file is.
func (p Preview) renderImageContent(c Content) string {
	header := p.header(filepath.Base(p.path))
	size := p.Theme.StatusText().Render(fmt.Sprintf("Size: %d bytes", c.Meta.Size))
	footer := p.Theme.StatusText().Render("image preview not available")
	return p.panel(header + "\n\n" + size + "\n\n" + footer)
}

// renderDirectoryContent frames a directory's item count.
func (p Preview) renderDirectoryContent(c Content) string {
	header := p.header(filepath.Base(p.path) + "/")
	count := p.Theme.StatusText().Render(fmt.Sprintf("%d items", c.Count))
	return p.panel(header + "\n\n" + count)
}

// renderMetadataContent frames a file by size, permissions and time.
func (p Preview) renderMetadataContent(c Content) string {
	header := p.header(filepath.Base(p.path))
	size := p.Theme.StatusText().Render(fmt.Sprintf("Size: %d bytes", c.Meta.Size))
	perms := p.Theme.StatusText().Render(fmt.Sprintf("Permissions: %s", c.Meta.Perms))
	modTime := p.Theme.StatusText().Render(fmt.Sprintf("Modified: %s", c.Meta.ModTime))
	return p.panel(header + "\n\n" + size + "\n" + perms + "\n" + modTime)
}

// RenderText renders pre-highlighted content. lines carries the raw source for
// counting and highlighted carries the colourised text; when highlighted is
// empty the raw lines are shown unhighlighted.
func (p Preview) RenderText(lines, highlighted []string) string {
	return p.renderText(lines, highlighted, false)
}

// renderText is RenderText with the option to say the preview stopped at its
// load cap. The cap is applied away from the render path; the panel only
// reports it.
func (p Preview) renderText(lines, highlighted []string, truncated bool) string {
	chrome := 8
	maxVisible := p.Height - chrome
	if maxVisible < 1 {
		maxVisible = 1
	}

	body := highlighted
	if len(body) == 0 {
		// Unhighlighted content is raw file bytes and must be cleaned.
		// Highlighted output is emitted by our own formatter and already
		// carries trusted ANSI colour codes, so running it through the
		// sanitizer here would strip the highlighting itself.
		body = SanitizeLines(lines)
	}

	totalLines := len(body)
	if len(body) > maxVisible {
		body = body[:maxVisible]
	}
	// Clip before joining: the panel has no MaxWidth of its own on the text
	// body, so an over-long line would wrap and bleed into the next pane.
	body = clipLines(body, p.ContentWidth())

	header := p.header(filepath.Base(p.path))
	// Rendered without a wrapping foreground style so the inner ANSI colours
	// from Chroma are not overridden.
	content := strings.Join(body, "\n")

	var footer string
	switch {
	case truncated && totalLines > maxVisible:
		footer = fmt.Sprintf("... (%d more lines, file cut short)", totalLines-maxVisible)
	case truncated:
		// The loader stopped at its byte or line cap, so even the lines it
		// did read are not the whole file.
		footer = "file cut short (preview limit)"
	case totalLines > maxVisible:
		footer = fmt.Sprintf("... (%d more lines)", totalLines-maxVisible)
	default:
		footer = fmt.Sprintf("%d lines", totalLines)
	}

	return p.panel(header + "\n\n" + content + "\n\n" + p.Theme.StatusText().Render(footer))
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
	// Clean before highlighting, so the formatter is never handed bytes that
	// could reach the terminal.
	lines = SanitizeLines(lines)

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
	header := p.header(filepath.Base(path))
	hexDump, err := p.generateHexDump(path, 256)
	if err != nil {
		return p.RenderError(err)
	}
	footer := p.Theme.StatusText().Render("binary file")

	return p.panel(header + "\n\n" + hexDump + "\n\n" + footer)
}

func (p Preview) RenderImage(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return p.RenderError(err)
	}

	header := p.header(filepath.Base(path))
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

	header := p.header(filepath.Base(p.path))
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
	header := p.header(filepath.Base(path))
	// The message can embed an operating-system error, and those name the
	// path, which is filesystem data.
	return p.panel(header + "\n\n" + p.Theme.StatusText().Render(SanitizeDisplay(message)))
}

func (p Preview) RenderDirectory(path string) string {
	entries, err := fs.ReadDir(path)
	if err != nil {
		return p.RenderError(err)
	}

	header := p.header(filepath.Base(path) + "/")
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

	header := p.header(filepath.Base(path))
	size := p.Theme.StatusText().Render(fmt.Sprintf("Size: %d bytes", info.Size()))
	perms := p.Theme.StatusText().Render(fmt.Sprintf("Permissions: %s", info.Mode()))
	modTime := p.Theme.StatusText().Render(fmt.Sprintf("Modified: %s", info.ModTime().Format("2006-01-02 15:04:05")))

	return p.panel(header + "\n\n" + size + "\n" + perms + "\n" + modTime)
}

func (p Preview) RenderError(err error) string {
	if err == nil {
		return p.panel("Error: unknown error")
	}
	// Operating-system errors embed the path they failed on, and a path is
	// filesystem data: it may contain control bytes that would act on the
	// terminal rather than being drawn.
	return p.panel("Error: " + SanitizeDisplay(err.Error()))
}

func (p Preview) generateHexDump(path string, maxBytes int) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	// Only a regular file may be streamed. A FIFO would block the render
	// path forever and a device has no end to read.
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("read: %s is not a regular file", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return "", err
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
		strings.Split(sb.String(), "\n"), p.ContentWidth()), "\n")), nil
}

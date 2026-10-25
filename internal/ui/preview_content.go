package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
)

// Kind says which panel a preview draws.
type Kind int

const (
	// KindEmpty is the panel shown when nothing is selected.
	KindEmpty Kind = iota
	// KindLoading is the panel shown while a preview is being gathered.
	KindLoading
	// KindText is a source file, shown with syntax colouring.
	KindText
	// KindBinary is a binary file, shown as a hex dump.
	KindBinary
	// KindImage is an image, shown as metadata only.
	KindImage
	// KindArchive is an archive, shown as an entry listing.
	KindArchive
	// KindDirectory is a directory, shown as an item count.
	KindDirectory
	// KindMetadata is any other file, shown as size, permissions and time.
	KindMetadata
	// KindError is a file that could not be read.
	KindError
)

// Metadata describes a file that has no richer preview.
type Metadata struct {
	Size    int64
	Perms   string
	ModTime string
}

// Content is everything the preview panel draws for one file.
//
// It is gathered away from the render path. Reading a file and colouring it
// is slow enough to be felt: a thousand lines of source takes tens of
// milliseconds, and the user pressed an arrow key to move the cursor. With
// the work off the render path, the cursor moves at once and the panel
// catches up when the file is ready.
type Content struct {
	// Kind selects the panel.
	Kind Kind
	// Text is the file's source, already sanitised.
	Text []string
	// TextTruncated reports that the text preview stopped at its line or
	// byte cap, so the body is not the whole file. The panel says so in its
	// footer rather than pretending the file ended there.
	TextTruncated bool
	// Highlighted is the colourised source. It is nil when highlighting
	// failed or made no difference, and the raw text is shown then.
	Highlighted []string
	// Archive is the listing of an archive.
	Archive []fs.ArchiveEntry
	// ArchiveTruncated marks an archive whose listing was cut short.
	ArchiveTruncated bool
	// Hex is the hex dump of a binary file.
	Hex string
	// Count is the number of items in a directory.
	Count int
	// Meta describes a file with no richer preview.
	Meta Metadata
	// Err is the failure, if any.
	Err error
}

// previewTextLines is how many lines of a text file the preview reads. A
// thousand lines is far more than the panel can show, and reading the whole
// file would be wasted work.
const previewTextLines = 1000

// previewTextBytes caps how much source the text preview reads and keeps.
//
// Lines alone are not a bound: one line can be tens of kilobytes, and the
// highlighter emits several bytes of styling per token, so a line-capped
// preview could still cost hundreds of megabytes. 256 KiB is far more than the
// panel can show while keeping a large file's preview cheap to build.
const previewTextBytes = 256 * 1024

// previewHexBytes is how much of a binary file the hex dump covers.
const previewHexBytes = 256

// Load gathers the preview for path.
//
// Every disk read the preview needs happens here, in the background, so the
// render path can draw what it is given without touching the filesystem.
func Load(path string, theme config.Theme) Content {
	if path == "" {
		return Content{Kind: KindEmpty}
	}

	info, err := os.Stat(path)
	if err != nil {
		return Content{Kind: KindError, Err: err}
	}

	if info.IsDir() {
		entries, err := fs.ReadDir(path)
		if err != nil {
			return Content{Kind: KindError, Err: err}
		}
		return Content{Kind: KindDirectory, Count: len(entries)}
	}

	switch fs.DetectFileType(path) {
	case fs.FileTypeText:
		lines, truncated, err := readTextHead(path, previewTextLines, previewTextBytes)
		if err != nil {
			// An unreadable file must say so; a silent empty preview would
			// look like a file with no content.
			return Content{Kind: KindError, Err: err}
		}
		// Clean at ingest so neither the highlighter nor the preview
		// cache ever holds raw bytes from the file.
		lines = SanitizeContentLines(lines)
		return Content{
			Kind:          KindText,
			Text:          lines,
			Highlighted:   HighlightLines(path, lines, theme),
			TextTruncated: truncated,
		}
	case fs.FileTypeBinary:
		hex, err := hexDump(path, previewHexBytes)
		if err != nil {
			return Content{Kind: KindError, Err: err}
		}
		return Content{Kind: KindBinary, Hex: hex}
	case fs.FileTypeImage:
		return Content{Kind: KindImage, Meta: metadataOf(info)}
	case fs.FileTypeArchive:
		entries, truncated, err := fs.ListArchive(path)
		if err != nil {
			return Content{Kind: KindError, Err: err}
		}
		return Content{
			Kind:             KindArchive,
			Archive:          entries,
			ArchiveTruncated: truncated,
		}
	default:
		return Content{Kind: KindMetadata, Meta: metadataOf(info)}
	}
}

// metadataOf describes a file by size, permissions and modification time.
func metadataOf(info os.FileInfo) Metadata {
	return Metadata{
		Size:    info.Size(),
		Perms:   info.Mode().String(),
		ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
	}
}

// readTextHead reads at most maxLines lines and at most maxBytes of retained
// source. It reports whether the file held more than was kept.
//
// The read itself is bounded by io.LimitReader, so a huge file costs a fixed
// amount of time and memory rather than however large it happens to be. The
// regular-file check mirrors internal/fs's readers: opening a FIFO waits for a
// writer that may never come, and a device has no end.
func readTextHead(path string, maxLines, maxBytes int) ([]string, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("read: %s is not a regular file", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	reader := bufio.NewReaderSize(io.LimitReader(f, int64(maxBytes)), 32*1024)
	lines := make([]string, 0, min(maxLines, 64))
	used := 0
	for len(lines) < maxLines && used < maxBytes {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			// Match bufio.Scanner, which the old reader used: line endings
			// are not part of the line.
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			if len(lines) > 0 {
				// Count the newline that will join this line to the
				// previous one, so the joined source also respects the cap.
				used++
			}
			if remaining := maxBytes - used; len(line) > remaining {
				line = truncateUTF8(line, remaining)
			}
			used += len(line)
			lines = append(lines, line)
			if used >= maxBytes {
				break
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return lines, false, readErr
		}
	}

	truncated := info.Size() > int64(maxBytes)
	if !truncated && len(lines) >= maxLines {
		// The line cap may have stopped before the end of the file. One
		// extra byte settles it without reading the rest.
		if _, err := reader.ReadByte(); err == nil {
			truncated = true
		}
	}
	return lines, truncated, nil
}

// truncateUTF8 cuts s to at most n bytes without leaving a partial rune at the
// end. The result is never longer than n.
func truncateUTF8(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// hexDump renders the first maxBytes of a binary file as a hex listing.
//
// It is a free function rather than a method on Preview because the preview
// panel is a renderer now: the dump is produced in the background and the
// panel only frames it. The rows are clipped to the panel's width when it is
// drawn, not here, because the panel can be resized without the preview
// being reloaded.
//
// Only a regular file is opened. A FIFO would block the preview worker forever
// and a device has no end to read.
func hexDump(path string, maxBytes int) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
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

		fmt.Fprintf(&sb, "%08x  %-48s  %s\n", i, hexPart, asciiPart)
	}
	return sb.String(), nil
}

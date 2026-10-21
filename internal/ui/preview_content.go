package ui

import (
	"fmt"
	"os"
	"strings"

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

// previewTextLines is how much of a text file the preview reads. A thousand
// lines is far more than the panel can show, and reading the whole file
// would be wasted work.
const previewTextLines = 1000

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
		lines, _ := fs.ReadFileHead(path, previewTextLines)
		// Clean at ingest so neither the highlighter nor the preview
		// cache ever holds raw bytes from the file.
		lines = SanitizeLines(lines)
		return Content{
			Kind:        KindText,
			Text:        lines,
			Highlighted: HighlightLines(path, lines, theme),
		}
	case fs.FileTypeBinary:
		return Content{Kind: KindBinary, Hex: hexDump(path, previewHexBytes)}
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

// hexDump renders the first maxBytes of a binary file as a hex listing.
//
// It is a free function rather than a method on Preview because the preview
// panel is a renderer now: the dump is produced in the background and the
// panel only frames it. The rows are clipped to the panel's width when it is
// drawn, not here, because the panel can be resized without the preview
// being reloaded.
func hexDump(path string, maxBytes int) string {
	f, err := os.Open(path)
	if err != nil {
		return "Error reading file"
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
	return sb.String()
}

package fs

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxArchiveScan caps how many headers we read from streamed formats. Zip is
// unaffected since it reads a central directory, but tar must be walked in
// order and a large archive should not stall the UI.
const maxArchiveScan = 500

// ErrUnsupportedArchive is returned for formats that need third-party readers.
var ErrUnsupportedArchive = errors.New("unsupported archive format")

type ArchiveEntry struct {
	Name  string
	Size  int64
	IsDir bool
}

// Truncated reports whether listing stopped early at the scan cap.
func ListArchive(path string) (entries []ArchiveEntry, truncated bool, err error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".zip":
		entries, err = listZip(path)
	case ".tar":
		entries, truncated, err = listTar(path, nil)
	case ".gz", ".tgz":
		// A bare .gz is usually a single compressed file, not a tar.
		if isGzippedTar(path) {
			f, openErr := os.Open(path)
			if openErr != nil {
				return nil, false, openErr
			}
			defer f.Close()
			gz, gzErr := gzip.NewReader(f)
			if gzErr != nil {
				return nil, false, gzErr
			}
			defer gz.Close()
			entries, truncated, err = listTar(path, gz)
		} else {
			return nil, false, ErrUnsupportedArchive
		}
	default:
		return nil, false, ErrUnsupportedArchive
	}

	if err != nil {
		return nil, false, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, truncated, nil
}

func listZip(path string) ([]ArchiveEntry, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	entries := make([]ArchiveEntry, 0, len(r.File))
	for _, f := range r.File {
		entries = append(entries, ArchiveEntry{
			Name:  f.Name,
			Size:  int64(f.UncompressedSize64),
			IsDir: f.FileInfo().IsDir(),
		})
	}
	return entries, nil
}

func listTar(path string, r io.Reader) ([]ArchiveEntry, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	src := io.Reader(f)
	if r != nil {
		src = r
	}

	tr := tar.NewReader(src)
	var entries []ArchiveEntry
	truncated := false

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A truncated tar still yields the headers read so far.
			if len(entries) > 0 {
				return entries, true, nil
			}
			return nil, false, err
		}

		entries = append(entries, ArchiveEntry{
			Name:  hdr.Name,
			Size:  hdr.Size,
			IsDir: hdr.Typeflag == tar.TypeDir,
		})

		if len(entries) >= maxArchiveScan {
			truncated = true
			break
		}
	}

	return entries, truncated, nil
}

// isGzippedTar reports whether the gzip stream contains a tar header.
func isGzippedTar(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return false
	}
	defer gz.Close()

	magic := make([]byte, 262)
	n, _ := io.ReadFull(gz, magic)
	return n >= 262 && string(magic[257:262]) == "ustar"
}

// FormatSize renders a byte count in human units.
func FormatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTP"[exp])
}

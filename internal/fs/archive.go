package fs

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxArchiveScan caps how many members a listing returns. Both formats are
// capped: a tar must be walked in order and a zip's central directory can be
// arbitrarily large, but in either case the preview only needs enough rows to
// fill the screen.
const maxArchiveScan = 500

// maxArchiveDecompress caps how many decompressed bytes the tar path may read.
// tar.Next has to skip over each member's payload to reach the next header,
// and a gzip stream cannot seek, so a small archive declaring a huge member
// would otherwise decompress the whole payload before the listing finishes.
// 64 MiB is far more than the 500 headers a preview needs, while a hostile
// archive costs well under a second of decompression instead of minutes.
//
// It is a variable so that a test can lower it and exercise the same path
// with a small archive: the behaviour under test is the budget, not its size.
var maxArchiveDecompress int64 = 64 << 20

// ErrUnsupportedArchive is returned for formats that need third-party readers.
var ErrUnsupportedArchive = errors.New("unsupported archive format")

// errArchiveBudget is returned by decodeBudget once the decompression budget
// is spent. It is deliberately not io.EOF: tar treats a plain EOF while
// skipping a payload as a clean end of the archive, which would turn a budget
// cut into a complete-looking listing.
var errArchiveBudget = errors.New("archive decompression budget exhausted")

type ArchiveEntry struct {
	Name  string
	Size  int64
	IsDir bool
}

// Truncated reports whether listing stopped early at an entry or byte cap.
func ListArchive(path string) (entries []ArchiveEntry, truncated bool, err error) {
	// A FIFO or a device cannot be listed as an archive: opening one can
	// block or never end. The check happens before the format is dispatched
	// so no reader below is handed a path it cannot stream.
	if err := requireRegularFile(path, "archive"); err != nil {
		return nil, false, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".zip":
		entries, truncated, err = listZip(path)
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

func listZip(path string) ([]ArchiveEntry, bool, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, false, err
	}
	defer r.Close()

	files := r.File
	truncated := len(files) > maxArchiveScan
	if truncated {
		files = files[:maxArchiveScan]
	}

	entries := make([]ArchiveEntry, 0, len(files))
	for _, f := range files {
		entries = append(entries, ArchiveEntry{
			Name:  f.Name,
			Size:  int64(f.UncompressedSize64),
			IsDir: f.FileInfo().IsDir(),
		})
	}
	return entries, truncated, nil
}

// decodeBudget caps how many bytes a reader may consume before it reports
// errArchiveBudget. It is used on the gzip stream, whose decompressed size is
// not bounded by the file on disk.
type decodeBudget struct {
	r         io.Reader
	remaining int64
}

func (b *decodeBudget) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, errArchiveBudget
	}
	if int64(len(p)) > b.remaining {
		p = p[:int(b.remaining)]
	}
	n, err := b.r.Read(p)
	b.remaining -= int64(n)
	return n, err
}

func (b *decodeBudget) exhausted() bool { return b.remaining <= 0 }

func listTar(path string, r io.Reader) ([]ArchiveEntry, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	src := io.Reader(f)
	var budget *decodeBudget
	if r != nil {
		// Streamed (compressed) tars get a byte budget. A plain tar is read
		// straight from the file, which tar can seek over, so it needs none.
		budget = &decodeBudget{r: r, remaining: maxArchiveDecompress}
		src = budget
	}

	tr := tar.NewReader(src)
	var entries []ArchiveEntry

	for {
		hdr, err := tr.Next()
		if err != nil {
			if errors.Is(err, errArchiveBudget) {
				// The walk stopped because the decompression budget ran out.
				return entries, true, nil
			}
			if errors.Is(err, io.EOF) {
				break
			}
			if budget != nil && budget.exhausted() {
				// A cut mid-block can surface as an unexpected EOF. With no
				// budget left to explain it, that is truncation, not damage.
				return entries, true, nil
			}
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

		if budget != nil && hdr.Size > budget.remaining {
			// Reaching the next header means decompressing this payload, and
			// gzip cannot seek over it. Stop before reading any of it; the
			// member just read is still listed.
			return entries, true, nil
		}

		if len(entries) >= maxArchiveScan {
			return entries, true, nil
		}
	}

	return entries, false, nil
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

// FormatSize renders a byte count in compact human units: whole numbers,
// single-letter suffixes, no space. Byte counts carry no suffix at all.
// Rounding that reaches the next unit promotes it, so 1048575 reads as 1MB
// rather than 1024KB.
func FormatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	v := int(math.Round(float64(n) / float64(div)))
	if v >= unit && exp < 4 {
		v /= unit
		exp++
	}
	return fmt.Sprintf("%d%cB", v, "KMGTP"[exp])
}

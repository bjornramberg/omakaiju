package fs

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type Entry struct {
	Name    string
	Path    string
	IsDir   bool
	IsLink  bool
	Size    int64
	ModTime int64
	// Mode carries the permission bits for display. ReadDir already holds the
	// FileInfo, so keeping it costs nothing extra. For a symlink it stays the
	// link's own mode, so the listing can show lrwxrwxrwx the way ls does.
	Mode os.FileMode
}

func ReadDir(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}

	result := make([]Entry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}

		full := filepath.Join(path, e.Name())
		size := info.Size()
		modTime := info.ModTime()
		isDir := e.IsDir()

		// DirEntry.IsDir reports the raw directory entry, so a symlink to a
		// directory comes back false. Since every other part of the app treats
		// the target as the real entry, the link is resolved here so Enter
		// navigates instead of handing a directory to the desktop opener.
		// Mode stays the link's own so the permissions column keeps its l,
		// while size and mtime come from the target because the link's own
		// size is just the length of the path it stores.
		isLink := info.Mode()&os.ModeSymlink != 0
		if isLink {
			if target, err := os.Stat(full); err == nil {
				isDir = target.IsDir()
				size = target.Size()
				modTime = target.ModTime()
			}
			// A dangling link resolves to nothing and stays a file: Enter then
			// hands it to the desktop opener, which reports the broken target
			// instead of the pane silently refusing to open.
		}

		result = append(result, Entry{
			Name:    e.Name(),
			Path:    full,
			IsDir:   isDir,
			IsLink:  isLink,
			Size:    size,
			ModTime: modTime.Unix(),
			Mode:    info.Mode(),
		})
	}

	SortEntries(result)
	return result, nil
}

// SortEntries orders a listing the way a file manager should: directories
// first, then case-insensitive by name. ReadDir returns raw filesystem order,
// which is unstable across machines, so callers get a deterministic list.
func SortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if la != lb {
			return la < lb
		}
		return a.Name < b.Name
	})
}

func Copy(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}

func CopyRecursive(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return Copy(src, dst)
	}
	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if err := CopyRecursive(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func Move(src, dst string) error {
	return os.Rename(src, dst)
}

func Delete(path string) error {
	return os.RemoveAll(path)
}

func CreateFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return f.Close()
}

func CreateDir(path string) error {
	return os.MkdirAll(path, 0755)
}

type FileType int

const (
	FileTypeText FileType = iota
	FileTypeBinary
	FileTypeImage
	FileTypeArchive
	FileTypeDirectory
)

func DetectFileType(path string) FileType {
	info, err := os.Stat(path)
	if err != nil {
		return FileTypeBinary
	}
	if info.IsDir() {
		return FileTypeDirectory
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md", ".go", ".py", ".js", ".ts", ".rs", ".c", ".cpp", ".h", ".json", ".yaml", ".yml", ".toml", ".xml", ".html", ".css", ".sh", ".bash", ".zsh":
		return FileTypeText
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".svg", ".webp":
		return FileTypeImage
	case ".zip", ".tar", ".tgz", ".gz", ".bz2", ".xz", ".7z", ".rar":
		return FileTypeArchive
	}

	f, err := os.Open(path)
	if err != nil {
		return FileTypeBinary
	}
	defer f.Close()

	// Sample generously: a torrent begins with readable bencoded keys and only
	// hits binary once the piece hashes start, so a short window can read as
	// clean text when the file is not text at all.
	const sniffLen = 4096
	buf := make([]byte, sniffLen)
	n, _ := io.ReadFull(f, buf)
	if n == 0 {
		return FileTypeText
	}
	sample := buf[:n]

	// A null byte is conclusive on its own and is the cheapest test.
	control := 0
	for _, b := range sample {
		if b == 0 {
			return FileTypeBinary
		}
		if isControlByte(b) {
			control++
		}
	}

	// SHA1 blocks are effectively random, so invalid UTF-8 is the signal that
	// catches them. Valid multi-byte text such as CJK source passes unchanged.
	if !utf8.Valid(sample) {
		return FileTypeBinary
	}

	// Catches binaries whose bytes happen to be valid UTF-8, such as a Latin-1
	// encoded document. Line breaks are exempt so ordinary text is unaffected.
	if control*5 > len(sample) {
		return FileTypeBinary
	}

	return FileTypeText
}

// isControlByte reports whether b is a control character that carries no
// meaning in a text file. Whitespace controls are exempt.
func isControlByte(b byte) bool {
	switch b {
	case '\t', '\n', '\r':
		return false
	}
	return b < 0x20 || b == 0x7f
}

func ReadFileHead(path string, maxLines int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for i := 0; i < maxLines && scanner.Scan(); i++ {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func CountLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}

func WalkDir(root string) ([]string, error) {
	files := make([]string, 0, 4096)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// Never skip the root itself, even if its name matches the
			// ignore list (e.g. searching inside a directory literally
			// called "target").
			if path != root && ignoredDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= MaxWalkFiles {
			return errWalkCap
		}
		files = append(files, path)
		return nil
	})
	if err == errWalkCap {
		err = nil
	}
	sort.Strings(files)
	return files, err
}

// MaxWalkFiles bounds how many paths WalkDir materializes. A full home-tree
// walk with caches and vendored deps can otherwise pin hundreds of MB that
// the finder only ever shows the top few hundred of.
const MaxWalkFiles = 50000

var errWalkCap = errors.New("walk file cap reached")

// ignoredDir reports whether a directory name is a known space hog that
// should never enter fuzzy results. Matching is by base name only so the
// walk prunes the whole subtree via SkipDir.
func ignoredDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".bzr",
		"node_modules", "vendor", "target",
		"__pycache__", ".cache", ".npm", ".yarn", ".pnpm-store",
		"dist", "build", ".next", ".nuxt", ".parcel-cache",
		"coverage", ".tox", ".venv", "venv",
		".mypy_cache", ".pytest_cache", ".gradle":
		return true
	}
	return false
}

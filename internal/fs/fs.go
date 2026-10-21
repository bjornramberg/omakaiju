package fs

import (
	"bufio"
	"fmt"
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

// SortMode selects the ordering applied to a pane listing. The cycle is
// Name -> SizeDesc -> SizeAsc -> Name, advanced by a single key so the user
// can flip through the three states without a prompt.
type SortMode int

const (
	// SortByName is the default: directories first, then case-insensitive
	// by name, exactly as before.
	SortByName SortMode = iota
	// SortBySizeDesc puts the largest entries on top.
	SortBySizeDesc
	// SortBySizeAsc puts the smallest entries on top.
	SortBySizeAsc
)

// Next advances the cycle Name -> SizeDesc -> SizeAsc -> Name.
func (s SortMode) Next() SortMode {
	switch s {
	case SortByName:
		return SortBySizeDesc
	case SortBySizeDesc:
		return SortBySizeAsc
	default:
		return SortByName
	}
}

// Label is the short human-readable name shown in the status line.
func (s SortMode) Label() string {
	switch s {
	case SortBySizeDesc:
		return "size↓"
	case SortBySizeAsc:
		return "size↑"
	default:
		return "name"
	}
}

// SortEntries orders a listing the way a file manager should: directories
// first, then case-insensitive by name. ReadDir returns raw filesystem order,
// which is unstable across machines, so callers get a deterministic list.
func SortEntries(entries []Entry) {
	SortEntriesBy(entries, SortByName)
}

// SortEntriesBy orders a listing by mode. Directories always sort first so the
// size modes only reorder within the dir and file groups; ties fall back to
// case-insensitive name so the order stays deterministic.
func SortEntriesBy(entries []Entry, mode SortMode) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		switch mode {
		case SortBySizeDesc:
			if a.Size != b.Size {
				return a.Size > b.Size
			}
		case SortBySizeAsc:
			if a.Size != b.Size {
				return a.Size < b.Size
			}
		}
		la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if la != lb {
			return la < lb
		}
		return a.Name < b.Name
	})
}

// Copy copies a single file, keeping its permission bits.
//
// A symlink is recreated as a symlink rather than followed: copying a link
// to a directory would otherwise fail with "is a directory", and copying a
// link to a file would silently duplicate the target's contents instead of
// the link the user pointed at.
func Copy(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return copySymlink(src, dst)
	}
	return copyFile(src, dst, info.Mode())
}

// copyMaxDepth bounds the recursion. A tree nested deeper than this is
// pathological rather than useful, and the bound keeps a runaway from
// consuming the stack.
const copyMaxDepth = 256

// CopyRecursive copies src to dst, recursing into directories.
//
// Symlinks are copied as symlinks and never followed. Following them is
// what turns a copy into a runaway: a link to an ancestor makes the copy
// recurse into its own output, and a link to the root of the filesystem
// makes it duplicate the whole disk. Copying the link itself is also what
// "copy this" means to the user.
//
// A directory cannot be copied into its own subtree, because the copy would
// keep discovering the directories it is creating and nest without end.
func CopyRecursive(src, dst string) error {
	return copyRecursive(src, dst, nil, 0)
}

// copyRecursive does the work for CopyRecursive.
//
// visited holds the directories already copied on the current path, so a
// tree that reaches itself is reported rather than followed. It is the
// current path only: the same directory legitimately reachable by two
// different routes is copied twice, as the tree describes it.
func copyRecursive(src, dst string, visited []os.FileInfo, depth int) error {
	if depth > copyMaxDepth {
		return fmt.Errorf("copy: %s is nested more than %d levels deep", src, copyMaxDepth)
	}

	info, err := os.Lstat(src)
	if err != nil {
		return err
	}

	// A symlink is copied as a link. Following it is the whole bug.
	if info.Mode()&os.ModeSymlink != 0 {
		return copySymlink(src, dst)
	}

	if !info.IsDir() {
		return copyFile(src, dst, info.Mode())
	}

	// Copying a directory into its own subtree would make the copy
	// discover the directories it is creating and nest forever.
	if inside, err := isInside(src, dst); err == nil && inside {
		return fmt.Errorf("copy: cannot copy %s into itself", src)
	}

	// A directory that is already on the path is a cycle.
	for _, seen := range visited {
		if os.SameFile(seen, info) {
			return fmt.Errorf("copy: %s is a directory cycle", src)
		}
	}
	visited = append(visited, info)

	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		err := copyRecursive(
			filepath.Join(src, entry.Name()),
			filepath.Join(dst, entry.Name()),
			visited,
			depth+1,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies a regular file's contents, keeping its permission bits.
//
// The setuid, setgid and sticky bits are deliberately dropped: a copied
// setuid binary in a world-writable directory is a privilege escalation
// waiting to happen, and no file manager should hand one out by accident.
func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	// OpenFile masks the mode with the umask, so the exact bits are set
	// here. A copied script that loses its executable bit is a surprise
	// the user would not expect from a file manager.
	return os.Chmod(dst, mode.Perm())
}

// copySymlink recreates a symlink at dst with the same target.
func copySymlink(src, dst string) error {
	target, err := os.Readlink(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// os.Symlink fails if the path exists, and the destination may be
	// left over from an earlier copy.
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, dst)
}

// isInside reports whether child is parent or lies beneath it.
func isInside(parent, child string) (bool, error) {
	pAbs, err := filepath.Abs(parent)
	if err != nil {
		return false, err
	}
	cAbs, err := filepath.Abs(child)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(pAbs, cAbs)
	if err != nil {
		return false, err
	}
	if rel == "." {
		return true, nil
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
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

// walkLimit bounds how many files a single walk collects.
//
// The finder's walk has no ignore list, so it sees everything under the
// root: vendored dependencies, caches, object stores. Each path is kept in
// memory, and the matcher allocates several times that again, so an
// unbounded walk of a very large tree can exhaust the machine's memory.
// That is a fatal error rather than a panic, and nothing recovers from it:
// the process dies with the terminal still in raw mode.
//
// The limit is high enough that no ordinary tree reaches it, and the finder
// says so when it does.
//
// It is a variable so a test can lower it; nothing else changes it.
var walkLimit = 200000

// WalkResult is what a walk found.
type WalkResult struct {
	// Files are the paths found, sorted.
	Files []string
	// Truncated reports that the walk stopped at the limit and the tree
	// holds more than was collected.
	Truncated bool
}

// WalkDir collects the files under root, sorted by path.
//
// The walk stops at walkLimit and reports that it did, so the caller can
// tell the user the results are partial rather than pretending the tree
// ended there.
func WalkDir(root string) (WalkResult, error) {
	var files []string
	truncated := false

	// Deliberately recursive with no ignore list: the finder must see the
	// whole tree under root, including dot-dirs, vendored deps and caches.
	// It is allowed to be expensive while the finder runs.
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if len(files) >= walkLimit {
			truncated = true
			return filepath.SkipAll
		}
		files = append(files, path)
		return nil
	})
	sort.Strings(files)
	return WalkResult{Files: files, Truncated: truncated}, err
}

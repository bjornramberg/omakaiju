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
	// Only a regular file can be streamed. Opening a FIFO waits for a
	// writer that may never come, and a device has no end to copy, so a
	// special file is refused by name rather than opened.
	if !info.Mode().IsRegular() {
		return &refusedEntry{verb: "copy", path: src}
	}
	return copyFileAtomic(src, dst, info.Mode())
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
		// A FIFO, socket or device is not a stream a copy can carry.
		// Refuse it by name; the rest of the tree is still copied.
		if !info.Mode().IsRegular() {
			return &refusedEntry{verb: "copy", path: src}
		}
		return copyFileAtomic(src, dst, info.Mode())
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
	// A destination that is not there yet is built in a scratch directory
	// beside it and renamed into place. A copy that fails part way through
	// then leaves the destination as it was, instead of a half-filled
	// directory the user has to clear out by hand before trying again.
	if _, err := os.Lstat(dst); os.IsNotExist(err) {
		return copyDirAtomic(src, dst, visited, depth, info.Mode().Perm())
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

// copyDirAtomic builds a directory tree in a scratch directory beside the
// destination and renames it into place.
//
// The rename is atomic, so the destination either holds the whole tree or
// is not there at all. A copy that fails leaves the scratch directory
// behind, and that is removed.
func copyDirAtomic(src, dst string, visited []os.FileInfo, depth int, mode os.FileMode) error {
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.MkdirTemp(dir, tempPrefix+"*")
	if err != nil {
		return err
	}

	if err := copyRecursive(src, tmp, visited, depth); err != nil {
		os.RemoveAll(tmp)
		return err
	}

	// The scratch directory was created private. The copy carries the
	// source's permissions.
	if err := os.Chmod(tmp, mode); err != nil {
		os.RemoveAll(tmp)
		return err
	}

	if err := os.Rename(tmp, dst); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	syncDir(dir)
	return nil
}

// copySymlink recreates a symlink at dst with the same target.
//
// The link is staged beside the destination and then put in place with the
// same protection a file copy uses: an existing destination is held aside by
// a hard link, so a replacement that does not land can be undone and the old
// entry put back. Removing the destination first and then failing to put the
// link back would leave the user with nothing at all.
//
// The link is always copied as a link, never as the file it points at.
func copySymlink(src, dst string) error {
	target, err := os.Readlink(src)
	if err != nil {
		return err
	}
	if filepath.Clean(src) == filepath.Clean(dst) {
		return fmt.Errorf("copy: %s onto itself", src)
	}
	// The destination may be the very file the link points at. Removing it
	// to put the link in its place would destroy the file the link names,
	// and the link would then point at itself.
	if isLinkToItsOwnDestination(src, dst, target) {
		return fmt.Errorf("copy: %s points at its own destination", src)
	}
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := scratchName(dir)
	if err != nil {
		return err
	}
	if err := os.Symlink(target, tmp); err != nil {
		os.Remove(tmp)
		return err
	}

	// From here on the staged link belongs to this function, so every
	// failure takes it away again.
	fail := func(err error) error {
		os.Remove(tmp)
		return err
	}

	// The staged entry has to be the link that was asked for.
	staged, err := os.Readlink(tmp)
	if err != nil {
		return fail(err)
	}
	if staged != target {
		return fail(fmt.Errorf("copy: %s was staged with the wrong target", dst))
	}

	backup, err := replaceDestination(tmp, dst)
	if err != nil {
		os.Remove(tmp)
		return err
	}
	syncDir(dir)

	// The name is on the link, but it is only really there if it can be
	// seen at its new name, as a link, naming the target that was asked
	// for. A replacement that turns out not to have landed is undone.
	landed, err := os.Lstat(dst)
	if err != nil {
		return restoreFailure(fmt.Errorf("copy: %s did not land: %w", dst, err), backup, dst)
	}
	if landed.Mode()&os.ModeSymlink == 0 {
		return restoreFailure(fmt.Errorf("copy: %s did not land as a link", dst), backup, dst)
	}
	landedTarget, err := os.Readlink(dst)
	if err != nil {
		return restoreFailure(fmt.Errorf("copy: %s did not land: %w", dst, err), backup, dst)
	}
	if landedTarget != target {
		return restoreFailure(fmt.Errorf("copy: %s landed with the wrong target", dst), backup, dst)
	}
	// The replacement is good, so the old entry can go.
	discardBackup(backup)
	return nil
}

// scratchName reserves an unused name in dir.
//
// The name is free, but nothing is written to it: it is a place to build a
// file before it takes its final name.
func scratchName(dir string) (string, error) {
	res, err := os.CreateTemp(dir, tempPrefix+"*")
	if err != nil {
		return "", err
	}
	name := res.Name()
	res.Close()
	os.Remove(name)
	return name, nil
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

// Move relocates src to dst.
//
// A rename is the move: it is atomic, it costs nothing, and the file is
// either in one place or the other. It only works within one filesystem,
// though, so a move onto a share — or between two shares — comes back as a
// cross-device link error. That case is handled by copying the file across
// and removing the original, but only once the copy has been written,
// flushed and checked. The source is never removed on the strength of a copy
// that did not land.
func Move(src, dst string) error {
	// A source and a destination that are two names for one file would
	// otherwise be a silent no-op that looks like a successful move.
	if same, err := SameFile(src, dst); err == nil && same {
		return fmt.Errorf("move: %s and %s are the same file", src, dst)
	}

	// A rename replaces an existing name atomically, which would destroy
	// whatever was there without a word. The destination is checked with
	// Lstat, so a symlink counts as something that is already there and is
	// not followed. This is the same check the app's rename prompt relies
	// on.
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("move: destination %s already exists", dst)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("move: cannot check destination %s: %w", dst, err)
	}

	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !isCrossDevice(err) {
		return err
	}
	return moveAcrossDevices(src, dst)
}

func Delete(path string) error {
	return os.RemoveAll(path)
}

// CreateFile creates an empty file at path.
//
// The creation is exclusive: a name that is already taken is refused rather
// than emptied. Creating a file over one that exists would destroy it, and
// the user asked for a new file, not an empty one.
func CreateFile(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
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
	// Only a regular file may be sampled. Opening a FIFO waits for a writer
	// that may never come, and a device has no end. Neither is a preview.
	if !info.Mode().IsRegular() {
		return FileTypeBinary
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

// requireRegularFile refuses a path that is not a regular file before a
// reader opens it.
//
// Opening a FIFO waits for a writer that may never come, and a device has no
// end to read, so neither can be treated as a stream. Stat follows a symlink,
// which is what a reader wants: the link is a name for the file being read.
// Directories have their own readers and are refused here too.
func requireRegularFile(path, verb string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s: %s is not a regular file", verb, path)
	}
	return nil
}

func ReadFileHead(path string, maxLines int) ([]string, error) {
	if err := requireRegularFile(path, "read"); err != nil {
		return nil, err
	}
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
	if err := requireRegularFile(path, "read"); err != nil {
		return 0, err
	}
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

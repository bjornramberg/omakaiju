package fs

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ErrCrossDeviceTrash is returned when an entry cannot be moved into the trash
// because even the trash for the filesystem the entry is on refused the
// rename. Trash picks the trash on the entry's own filesystem first, so this
// now means no trash could be used at all. Copying gigabytes behind a one-key
// prompt would be worse than refusing, so the caller should offer permanent
// deletion instead.
var ErrCrossDeviceTrash = errors.New("cannot trash across filesystems")

// ErrUnsafeTrash is returned when a trash directory exists but cannot be
// trusted: it is a symlink, it is not a directory, or it is not owned by the
// user who would write into it. The entry is left where it was rather than
// deleted, because a trash that cannot be written safely is not a trash.
var ErrUnsafeTrash = errors.New("trash directory is not safe to use")

// TrashRoot returns the XDG trash directory, honouring XDG_DATA_HOME and falling
// back to ~/.local/share as the spec requires.
func TrashRoot() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "Trash"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "Trash"), nil
}

// TrashEntry is one restorable entry in a trash: the payload, the info file
// that records where it came from, and when it was deleted.
type TrashEntry struct {
	// Name is the payload's name inside the trash. It is usually the
	// original name, with .2, .3 and so on added when that name was taken.
	Name string
	// Path is the original path, as an absolute path.
	Path string
	// DeletedAt is the time recorded in the info file. It is the zero time
	// for an info file that does not record one.
	DeletedAt time.Time
	// PayloadPath and InfoPath are the two halves of the entry.
	PayloadPath string
	InfoPath    string
}

// trashLocation is one trash directory and, for a top-directory trash, the
// mount point it belongs to. A topdir trash stores its original paths relative
// to the topdir; the home trash stores them absolute.
type trashLocation struct {
	root   string
	topdir string
}

// Trash moves path into the trash and returns its new location. Following the
// freedesktop.org spec, the payload goes to <trash>/files and a matching
// <trash>/info/<name>.trashinfo records the original path and deletion time.
//
// An entry on the same filesystem as the home trash goes there. Anything else
// goes to the top-directory trash for the filesystem it is on,
// <topdir>/.Trash-<uid>, so deleting from an external drive, a tmpfs or a
// share still keeps the file recoverable instead of refusing.
//
// A failure after the payload has moved — the record could not be written — is
// returned with the payload's new location, because the file is in the trash
// and the caller has to tell the user where it went.
func Trash(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(abs); err != nil {
		return "", err
	}

	loc, err := trashFor(abs)
	if err != nil {
		return "", err
	}

	dst, err := trashInto(loc, abs)
	if err == nil || !errors.Is(err, ErrCrossDeviceTrash) {
		return dst, err
	}

	// The device check is only a hint: it is unknown on platforms without
	// st_dev, and a home trash symlinked onto another filesystem moves the
	// real destination. The trash for the filesystem the entry is on is
	// still worth one attempt before giving up.
	top, terr := topDirFor(filepath.Dir(abs))
	if terr != nil {
		return "", err
	}
	alt := trashLocation{root: topdirTrashPath(top), topdir: top}
	if alt.root == loc.root {
		return "", err
	}
	if dst, aerr := trashInto(alt, abs); aerr == nil {
		return dst, nil
	} else if !errors.Is(aerr, ErrCrossDeviceTrash) {
		// The topdir trash was reached but refused for its own reason (an
		// unsafe directory, an unwritable mount): that is the useful error.
		return "", aerr
	}
	return "", err
}

// TrashDirFor reports which trash directory Trash would use for path. It
// creates nothing and moves nothing; it is what a caller uses to show, list or
// empty the trash an entry belongs to.
func TrashDirFor(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	loc, err := trashFor(abs)
	if err != nil {
		return "", err
	}
	return loc.root, nil
}

// trashFor chooses the trash for abs: the home trash when abs is on the same
// filesystem, the top-directory trash for the mount point otherwise.
func trashFor(abs string) (trashLocation, error) {
	home, err := TrashRoot()
	if err != nil {
		return trashLocation{}, err
	}
	fileDev, fileOK := deviceOfNearest(abs)
	homeDev, homeOK := deviceOfNearest(home)
	// When either device is unknown, the home trash is tried first and the
	// caller's cross-device retry takes over if the rename refuses.
	if !fileOK || !homeOK || fileDev == homeDev {
		return trashLocation{root: home}, nil
	}

	top, err := topDirFor(filepath.Dir(abs))
	if err != nil {
		return trashLocation{}, err
	}
	return trashLocation{root: topdirTrashPath(top), topdir: top}, nil
}

// topdirTrashPath is the spec's per-filesystem trash for this user.
func topdirTrashPath(topdir string) string {
	return filepath.Join(topdir, fmt.Sprintf(".Trash-%d", os.Getuid()))
}

// topDirFor returns the mount point of the filesystem path is on: the highest
// directory that still reports the same device as path. Walking the device
// number up the tree is what the spec calls finding the top directory, and it
// needs no mount table to be parsed. A bind mount or a loop device is
// therefore a top directory in its own right, which is what the rename needs.
func topDirFor(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	dev, ok := deviceID(info)
	if !ok {
		return "", fmt.Errorf("cannot determine the filesystem of %s", path)
	}

	dir := path
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir, nil
		}
		parentInfo, err := os.Stat(parent)
		if err != nil {
			return "", err
		}
		parentDev, ok := deviceID(parentInfo)
		if !ok {
			return "", fmt.Errorf("cannot determine the filesystem of %s", parent)
		}
		if parentDev != dev {
			return dir, nil
		}
		dir = parent
	}
}

// deviceOfNearest returns the device path lives on, or the device of its
// nearest existing ancestor when path itself does not exist yet. Lstat is used
// rather than Stat so a symlink reports the filesystem holding the link, which
// is where a rename of the link happens.
func deviceOfNearest(path string) (uint64, bool) {
	for {
		info, err := os.Lstat(path)
		if err == nil {
			return deviceID(info)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return 0, false
		}
		path = parent
	}
}

// trashInto creates the trash, reserves a name in it, and moves abs there. The
// info file exists before the payload moves, so a name that is taken is always
// detected rather than replaced.
//
// The record is written into the reserved file after the rename. A rename that
// has already happened must be reported with the payload's new location even
// when the record cannot be written: the file is in the trash and the user has
// to be told where. The reservation does not depend on the write, so a failed
// write cannot reopen the name to a second trasher while the payload moves.
func trashInto(loc trashLocation, abs string) (string, error) {
	if err := loc.prepare(); err != nil {
		return "", err
	}

	when := time.Now()
	stored := loc.storedPath(abs)
	name := filepath.Base(abs)

	for attempt := 0; attempt < 10000; attempt++ {
		candidate := trashName(name, attempt)
		infoPath := loc.infoPath(candidate)

		// Creating the info file takes the name. O_EXCL means exactly one
		// writer can hold it, whether the other writer is another window of
		// this app, a desktop file manager, or gio.
		reserved, err := os.OpenFile(infoPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}

		// A name taken by another trasher is a regular file, and the next
		// name is tried. A directory or a symlink there can never hold a
		// record: that is a damaged slot, and the payload is still stored
		// under the name (the payload check below protects it) so the caller
		// is told where the file went instead of it silently gaining a
		// suffix.
		damaged := err != nil
		if damaged {
			info, lerr := os.Lstat(infoPath)
			if errors.Is(lerr, os.ErrNotExist) {
				// The other writer gave the name back between the create
				// and this check; the next attempt may take it.
				continue
			}
			if lerr != nil {
				return "", lerr
			}
			if info.Mode().IsRegular() {
				continue
			}
		}

		payload := loc.payloadPath(candidate)
		if _, err := os.Lstat(payload); err == nil {
			// A payload with no info file, left by an interrupted trash or
			// by a foreign tool, owns this name. Leave it alone and let the
			// reservation go, then try the next name.
			if reserved != nil {
				reserved.Close()
				os.Remove(infoPath)
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			if reserved != nil {
				reserved.Close()
				os.Remove(infoPath)
			}
			return "", err
		}

		// A rename is atomic and never follows symlinks, so directory trees
		// move whole and links are stored rather than followed.
		if err := os.Rename(abs, payload); err != nil {
			if reserved != nil {
				reserved.Close()
				os.Remove(infoPath)
			}
			if errors.Is(err, os.ErrExist) {
				continue
			}
			if isCrossDevice(err) {
				return "", fmt.Errorf("%w: %s", ErrCrossDeviceTrash, abs)
			}
			return "", err
		}

		if damaged {
			return payload, fmt.Errorf("cannot write %s: it is not a regular file", infoPath)
		}
		if err := writeTrashInfo(reserved, stored, when); err != nil {
			os.Remove(infoPath)
			return payload, err
		}
		return payload, nil
	}
	return "", fmt.Errorf("could not reserve a name in %s for %s", loc.root, abs)
}

// trashName is the name to try at attempt i: the entry's own name first, then
// name.2.ext, name.3.ext and so on. The extension is kept so the payload stays
// recognisable in the trash.
func trashName(name string, attempt int) string {
	if attempt == 0 {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s.%d%s", stem, attempt+1, ext)
}

// prepare creates the trash's directories. The home trash keeps the layout it
// has always had; a top-directory trash is checked first, because a directory
// on a shared mount that is a symlink or belongs to somebody else must not
// receive the file.
func (t trashLocation) prepare() error {
	if t.topdir == "" {
		if err := os.MkdirAll(filepath.Join(t.root, "files"), 0o700); err != nil {
			return err
		}
		return os.MkdirAll(filepath.Join(t.root, "info"), 0o700)
	}

	if err := ensureTopdirTrash(t.root); err != nil {
		return err
	}
	for _, sub := range []string{"files", "info"} {
		if err := ensureTrashSubdir(filepath.Join(t.root, sub)); err != nil {
			return err
		}
	}
	return nil
}

func (t trashLocation) payloadPath(name string) string {
	return filepath.Join(t.root, "files", name)
}

func (t trashLocation) infoPath(name string) string {
	return filepath.Join(t.root, "info", name+".trashinfo")
}

// storedPath is what the info file records. The home trash stores an absolute
// path; a topdir trash stores a path relative to its topdir, as the spec
// requires. A path that cannot be made relative falls back to absolute, which
// Restore accepts.
func (t trashLocation) storedPath(abs string) string {
	if t.topdir == "" {
		return abs
	}
	if rel, ok := relativeWithin(t.topdir, abs); ok {
		return rel
	}
	return abs
}

// writeTrashInfo writes the record into the info file reserved for it and
// closes the file.
func writeTrashInfo(f *os.File, original string, when time.Time) error {
	if _, err := f.WriteString(trashInfoText(original, when)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// trashInfoText renders the [Trash Info] record. The path is percent-encoded
// and '/' stays literal.
func trashInfoText(original string, when time.Time) string {
	var b strings.Builder
	b.WriteString("[Trash Info]\n")
	b.WriteString("Path=" + escapeTrashPath(original) + "\n")
	b.WriteString("DeletionDate=" + when.Format("2006-01-02T15:04:05") + "\n")
	return b.String()
}

// ensureTopdirTrash makes sure root exists and is a directory this user may
// write into, creating it 0700 when it is missing.
func ensureTopdirTrash(root string) error {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		// Mkdir rather than MkdirAll: the last component is the trash, and
		// its parent is a mount point that must already be there. A racing
		// creator is not an error; the result is checked like any other
		// existing directory.
		if err := os.Mkdir(root, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err = os.Lstat(root)
	}
	if err != nil {
		return err
	}
	return checkTrashDir(root, info)
}

// checkTrashDir refuses a trash directory that is not a directory owned by
// this user, and never accepts a symlink: a link on a shared mount can point
// anywhere, and the file would be moved through it.
func checkTrashDir(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is a symlink", ErrUnsafeTrash, path)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrUnsafeTrash, path)
	}
	owner, ok := fileOwner(info)
	if !ok {
		return fmt.Errorf("%w: cannot determine the owner of %s", ErrUnsafeTrash, path)
	}
	if owner != os.Getuid() {
		return fmt.Errorf("%w: %s is owned by uid %d, not %d", ErrUnsafeTrash, path, owner, os.Getuid())
	}
	return nil
}

// ensureTrashSubdir creates files/ or info/ inside a checked trash root, and
// refuses to use one that exists as a symlink or as something other than a
// directory.
func ensureTrashSubdir(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is a symlink", ErrUnsafeTrash, path)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrUnsafeTrash, path)
	}
	return nil
}

func fileOwner(info os.FileInfo) (int, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}

// Restore moves a trashed entry back to the path recorded in its info file and
// removes the info file. It returns the restored path.
//
// The payload is found next to the info file, and the original path is read
// from it: absolute for the home trash, relative to the topdir for a topdir
// trash. A missing parent directory is created. Nothing that already exists is
// replaced: a path that has been taken again since the entry was trashed is an
// error, and the entry stays in the trash.
func Restore(trashinfoPath string) (string, error) {
	infoDir := filepath.Dir(trashinfoPath)
	if filepath.Base(infoDir) != "info" {
		return "", fmt.Errorf("restore: %s is not in a trash info directory", trashinfoPath)
	}
	base := filepath.Base(trashinfoPath)
	if !strings.HasSuffix(base, ".trashinfo") || base == ".trashinfo" {
		return "", fmt.Errorf("restore: %s is not a trash info file", trashinfoPath)
	}
	root := filepath.Dir(infoDir)
	name := strings.TrimSuffix(base, ".trashinfo")

	original, _, err := readTrashInfo(trashinfoPath)
	if err != nil {
		return "", err
	}
	target, err := originalPath(root, original)
	if err != nil {
		return "", err
	}

	payload := filepath.Join(root, "files", name)
	if _, err := os.Lstat(payload); err != nil {
		return "", fmt.Errorf("restore %s: payload missing: %w", name, err)
	}
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("restore %s: %s already exists", name, target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(payload, target); err != nil {
		if isCrossDevice(err) {
			return "", fmt.Errorf("%w: %s", ErrCrossDeviceTrash, target)
		}
		return "", err
	}
	// The payload is back where it belongs; only now is the record no longer
	// needed. If removing it fails the entry was restored all the same, so
	// the path is returned alongside the error.
	if err := os.Remove(trashinfoPath); err != nil {
		return target, err
	}
	return target, nil
}

// EmptyTrash removes everything from the home trash. The trash directory
// itself is left in place, so the next trash does not have to recreate it.
func EmptyTrash() error {
	root, err := TrashRoot()
	if err != nil {
		return err
	}
	return EmptyTrashAt(root)
}

// EmptyTrashAt removes every payload and every info file from the trash rooted
// at root: the home trash, or the .Trash-<uid> of a topdir. The directory
// itself is left in place.
//
// The files/ and info/ directories are checked rather than guessed at: a
// symlink in their place would make this walk out of the trash, so it is
// refused. Entries that are themselves symlinks are removed as links; the walk
// never follows one.
func EmptyTrashAt(root string) error {
	for _, sub := range []string{"files", "info"} {
		dir := filepath.Join(root, sub)
		info, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s is a symlink", ErrUnsafeTrash, dir)
		}
		if !info.IsDir() {
			return fmt.Errorf("%w: %s is not a directory", ErrUnsafeTrash, dir)
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// ListTrash lists what is in the home trash, newest first.
func ListTrash() ([]TrashEntry, error) {
	root, err := TrashRoot()
	if err != nil {
		return nil, err
	}
	return ListTrashAt(root)
}

// ListTrashAt lists the restorable entries in the trash rooted at root, newest
// first. An entry is restorable when its info file parses and its payload is
// still there; orphaned payloads and damaged or foreign info files are skipped
// rather than reported as something Restore would fail on. A trash that does
// not exist lists as empty.
func ListTrashAt(root string) ([]TrashEntry, error) {
	infoDir := filepath.Join(root, "info")
	entries, err := os.ReadDir(infoDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	list := make([]TrashEntry, 0, len(entries))
	for _, entry := range entries {
		// Trash writes regular info files, so anything else is either not an
		// entry or not ours to read through.
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".trashinfo") {
			continue
		}
		infoPath := filepath.Join(infoDir, entry.Name())
		original, deleted, err := readTrashInfo(infoPath)
		if err != nil {
			continue
		}
		path, err := originalPath(root, original)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".trashinfo")
		payload := filepath.Join(root, "files", name)
		if _, err := os.Lstat(payload); err != nil {
			continue
		}
		list = append(list, TrashEntry{
			Name:        name,
			Path:        path,
			DeletedAt:   deleted,
			PayloadPath: payload,
			InfoPath:    infoPath,
		})
	}

	// Newest first, and stable so entries deleted in the same second keep the
	// directory order instead of shuffling between calls.
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].DeletedAt.IsZero() != list[j].DeletedAt.IsZero() {
			return !list[i].DeletedAt.IsZero()
		}
		return list[i].DeletedAt.After(list[j].DeletedAt)
	})
	return list, nil
}

// readTrashInfo reads the Path and DeletionDate out of an info file. The path
// is returned still encoded; the caller decides what it is relative to.
func readTrashInfo(infoPath string) (string, time.Time, error) {
	data, err := os.ReadFile(infoPath)
	if err != nil {
		return "", time.Time{}, err
	}

	var (
		raw       string
		foundPath bool
		deleted   time.Time
	)
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
		if !ok {
			continue
		}
		switch key {
		case "Path":
			raw = value
			foundPath = true
		case "DeletionDate":
			// The format carries no zone: it is the local time of the
			// deletion, which is how Trash writes it, so it is read back in
			// the local zone rather than as UTC.
			if when, perr := time.ParseInLocation("2006-01-02T15:04:05", value, time.Local); perr == nil {
				deleted = when
			}
		}
	}
	if !foundPath {
		return "", time.Time{}, fmt.Errorf("%s: no Path in trash info", infoPath)
	}

	path, err := unescapeTrashPath(raw)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("%s: %w", infoPath, err)
	}
	return path, deleted, nil
}

// originalPath turns the Path from an info file into the absolute path the
// entry came from. The trash root's name says which kind of trash it is: a
// .Trash-<uid> belongs to a topdir and stores paths relative to the directory
// above it; the home trash stores absolute paths.
//
// gio falls back to an absolute path when a path cannot be made relative even
// in a topdir trash, so an absolute path there is accepted as long as it stays
// inside the topdir the trash belongs to.
func originalPath(root, stored string) (string, error) {
	if stored == "" {
		return "", fmt.Errorf("trash info has an empty Path")
	}
	topdir, isTopdir := topdirOf(root)
	if !isTopdir {
		if !filepath.IsAbs(stored) {
			return "", fmt.Errorf("trash info path %q is not absolute", stored)
		}
		return filepath.Clean(stored), nil
	}

	if filepath.IsAbs(stored) {
		if !within(topdir, stored) {
			return "", fmt.Errorf("trash info path %q is outside %s", stored, topdir)
		}
		return filepath.Clean(stored), nil
	}

	target := filepath.Join(topdir, stored)
	if !within(topdir, target) {
		return "", fmt.Errorf("trash info path %q escapes %s", stored, topdir)
	}
	return target, nil
}

// topdirOf reports the mount point a trash root belongs to, when it is named
// the way the spec names a topdir trash.
func topdirOf(root string) (string, bool) {
	base := filepath.Base(root)
	if !strings.HasPrefix(base, ".Trash-") {
		return "", false
	}
	if _, err := strconv.Atoi(strings.TrimPrefix(base, ".Trash-")); err != nil {
		return "", false
	}
	return filepath.Dir(root), true
}

// within reports whether path is base or below it.
func within(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// relativeWithin returns path relative to base when it is below base.
func relativeWithin(base, path string) (string, bool) {
	if !within(base, path) {
		return "", false
	}
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." {
		return "", false
	}
	return rel, true
}

// uniqueTrashPath picks a free name in dir, appending .2, .3 and so on.
//
// Looking is not reserving, so Trash no longer uses this: the info file is
// created exclusively instead. It remains because the naming rule it encodes
// is the one the reservation follows.
func uniqueTrashPath(dir, name string) (string, error) {
	candidate := filepath.Join(dir, name)
	if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
		return candidate, nil
	}

	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; i < 10000; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s.%d%s", stem, i, ext))
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("could not find a free trash name for %s", name)
}

// escapeTrashPath percent-encodes the characters the spec reserves. The
// trashinfo spec explicitly requires '/' to stay literal, so segments are
// escaped individually and rejoined; url.PathEscape on the whole path would
// produce %2F and desktop tools then fail to read the entry.
func escapeTrashPath(p string) string {
	segments := strings.Split(p, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}

// unescapeTrashPath is the inverse of escapeTrashPath. PathUnescape is used
// rather than QueryUnescape so a '+' stays a plus and not a space.
func unescapeTrashPath(p string) (string, error) {
	segments := strings.Split(p, "/")
	for i, seg := range segments {
		decoded, err := url.PathUnescape(seg)
		if err != nil {
			return "", err
		}
		segments[i] = decoded
	}
	return strings.Join(segments, "/"), nil
}

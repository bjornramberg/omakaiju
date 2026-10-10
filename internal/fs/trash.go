package fs

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrCrossDeviceTrash is returned when an entry cannot be moved into the trash
// because it lives on another filesystem. Copying gigabytes behind a one-key
// prompt would be worse than refusing, so the caller should offer force delete.
var ErrCrossDeviceTrash = errors.New("cannot trash across filesystems")

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

// Trash moves path into the trash and returns its new location. Following the
// freedesktop.org spec, the payload goes to <trash>/files and a matching
// <trash>/info/<name>.trashinfo records the original path and deletion time.
func Trash(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(abs); err != nil {
		return "", err
	}

	root, err := TrashRoot()
	if err != nil {
		return "", err
	}
	filesDir := filepath.Join(root, "files")
	infoDir := filepath.Join(root, "info")
	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(infoDir, 0o700); err != nil {
		return "", err
	}

	name := filepath.Base(abs)
	dst, err := uniqueTrashPath(filesDir, name)
	if err != nil {
		return "", err
	}

	// A rename is atomic and never follows symlinks, so directory trees move
	// whole and links are stored rather than followed.
	if err := os.Rename(abs, dst); err != nil {
		if errors.Is(err, os.ErrInvalid) || isCrossDevice(err) {
			return "", fmt.Errorf("%w: %s", ErrCrossDeviceTrash, abs)
		}
		return "", err
	}

	// The trashinfo name must match the final payload name, including any
	// collision suffix, so the two can be paired up.
	if err := writeTrashInfo(infoDir, filepath.Base(dst), abs, time.Now()); err != nil {
		// The payload is already moved; leaving the info file behind would
		// leave an unrestorable orphan, so surface the failure.
		return dst, err
	}
	return dst, nil
}

// uniqueTrashPath picks a free name in dir, appending .2, .3 and so on.
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

func writeTrashInfo(infoDir, payloadName, original string, when time.Time) error {
	infoPath := filepath.Join(infoDir, payloadName+".trashinfo")

	var b strings.Builder
	b.WriteString("[Trash Info]\n")
	b.WriteString("Path=" + escapeTrashPath(original) + "\n")
	b.WriteString("DeletionDate=" + when.Format("2006-01-02T15:04:05") + "\n")

	return os.WriteFile(infoPath, []byte(b.String()), 0o600)
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

// isCrossDevice reports whether err is a rename across filesystems, which
// Go reports as a link error rather than a typed one.
func isCrossDevice(err error) bool {
	var le *os.LinkError
	if errors.As(err, &le) {
		msg := le.Err.Error()
		return strings.Contains(msg, "invalid cross-device link") ||
			strings.Contains(msg, "cross-device")
	}
	return false
}

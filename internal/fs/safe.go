package fs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// tempPrefix names the scratch files a copy writes before renaming them into
// place. The leading dot keeps them out of most listings, and the prefix
// makes it obvious what left one behind.
const tempPrefix = ".omakaiju-tmp-"

// SameFile reports whether two paths name the same file on disk.
//
// Comparing path strings is not enough. One file can be reached by many
// paths: through a symlinked directory, a bind mount, or an SMB share that
// is mounted in two places at once. A copy that believes two names for one
// file are two files opens the destination with O_TRUNC, truncates the
// source it was about to read, and writes nothing back. The user is left
// with an empty source, an empty destination and no error at all.
//
// The comparison is by identity — device and inode — so it holds for every
// path that reaches the same bytes.
func SameFile(a, b string) (bool, error) {
	aInfo, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	bInfo, err := os.Stat(b)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// The destination does not exist yet, so it cannot be the source.
			return false, nil
		}
		return false, err
	}
	return os.SameFile(aInfo, bInfo), nil
}

// copyFileAtomic copies src to dst without ever exposing a partial file at
// dst.
//
// The bytes go to a scratch file in the destination's own directory, which is
// what makes the final rename atomic: a rename within a directory never
// crosses a filesystem, and the kernel swaps the name over in one step. A
// reader of dst sees either the previous content or the whole new content —
// never a half-written file, and never an empty one.
//
// The copy is checked against the source's size before the rename, so a
// truncated write — a share that dropped the connection, a full disk — is
// caught while the previous file is still intact.
//
// The source is never modified. A copy that cannot finish leaves dst exactly
// as it was.
func copyFileAtomic(src, dst string, mode os.FileMode) error {
	same, err := SameFile(src, dst)
	if err != nil {
		return err
	}
	if same {
		return fmt.Errorf("copy: %s would overwrite itself", src)
	}

	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, tempPrefix+"*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	// The scratch file belongs to the copy, so every failure takes it away
	// again. The destination is left exactly as it was, whatever went wrong.
	placed := false
	defer func() {
		if !placed {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	written, err := io.Copy(tmp, in)
	in.Close()
	if err != nil {
		return err
	}

	// The bytes have to be on the disk before the name points at them.
	// Without this, a crash — or a server that drops the connection on a
	// share — can leave the destination naming a file with no content.
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	// The copy has to be whole before it is allowed to replace anything. A
	// source that grew while it was being read is fine; one that came out
	// short is not, and that is exactly the case that empties a file.
	if written < srcInfo.Size() {
		return fmt.Errorf("copy: %s is incomplete (%d of %d bytes)", src, written, srcInfo.Size())
	}

	// The scratch file was created with the default mode, so the exact bits
	// are set here. A copied script that loses its executable bit is a
	// surprise the user would not expect from a file manager.
	if err := os.Chmod(tmpName, mode.Perm()); err != nil {
		return err
	}

	backup, err := replaceDestination(tmpName, dst)
	if err != nil {
		os.Remove(tmpName)
		return err
	}
	placed = true

	// The name is on the file, but the file is only really there if it can
	// be seen at its new name with its content. A share may take a moment
	// to catch up, and a rename that did not land has to be reported
	// rather than assumed.
	landed, err := os.Stat(dst)
	if err != nil {
		restoreBackup(backup, dst)
		return fmt.Errorf("copy: %s did not land: %w", dst, err)
	}
	if landed.Size() != written {
		// The old file goes back: what is at the destination is not the
		// file that was asked for.
		restoreBackup(backup, dst)
		return fmt.Errorf("copy: %s landed short (%d of %d bytes)", dst, landed.Size(), written)
	}
	// The replacement is good, so the old file can go.
	discardBackup(backup)

	// The rename itself has to survive a crash too, which means the
	// directory entry has to reach the disk.
	syncDir(dir)
	return nil
}

// replaceDestination puts the scratch file at dst, keeping the old
// destination recoverable until the replacement has been checked.
//
// The old destination is held by a hard link while the replacement is made,
// so a replacement that turns out not to have landed can be undone: the old
// file goes back to its name. The link is dropped once the new file has been
// confirmed.
//
// A filesystem that does not support links simply goes without the safety
// net. The replacement is still atomic, so the destination is never seen
// half-written.
//
// The returned backup must be disposed of: remove it when the replacement
// is good, or restore it when it is not.
func replaceDestination(tmp, dst string) (string, error) {
	backup := backupDestination(dst)
	if err := renameIntoPlace(tmp, dst); err != nil {
		restoreBackup(backup, dst)
		return "", err
	}
	return backup, nil
}

// backupDestination takes a hard link to the destination so the old file can
// be put back if the replacement does not land.
//
// A link rather than a copy: it costs nothing, and the old file keeps its
// content until the link is dropped. A destination that is not there, or a
// filesystem without links, simply goes without the safety net.
func backupDestination(dst string) string {
	if _, err := os.Lstat(dst); err != nil {
		return ""
	}
	res, err := os.CreateTemp(filepath.Dir(dst), tempPrefix+"*")
	if err != nil {
		return ""
	}
	backup := res.Name()
	res.Close()
	os.Remove(backup)
	if err := os.Link(dst, backup); err != nil {
		return ""
	}
	return backup
}

// restoreBackup puts the old destination back after a replacement that did
// not land.
//
// The backup is a link to the old file, so renaming it back puts the old
// content at the destination's name.
func restoreBackup(backup, dst string) {
	if backup == "" {
		return
	}
	if err := os.Rename(backup, dst); err == nil {
		return
	}
	os.Remove(backup)
}

// discardBackup drops the held-aside old destination, now that the
// replacement has been confirmed.
func discardBackup(backup string) {
	if backup == "" {
		return
	}
	os.Remove(backup)
}

// renameIntoPlace puts the scratch file at dst.
//
// A rename over an existing file is atomic on a local filesystem, but the
// SMB client does not promise that. When the server refuses the first
// attempt, the client unlinks the destination and tries again — so a
// rename that fails can leave the destination gone rather than untouched.
//
// The destination is therefore checked afterwards, and the rename is retried
// while the scratch file is still there to put in place. The alternative —
// treating a failed rename as "nothing happened" — is how a file ends up
// disappearing from a share.
func renameIntoPlace(tmp, dst string) error {
	err := os.Rename(tmp, dst)
	if err == nil {
		return nil
	}
	if _, statErr := os.Lstat(dst); statErr == nil {
		// The destination is still there, so the rename left it alone.
		return err
	}
	// The destination is gone, so there is nothing left to replace. Put
	// the scratch file there while it still exists.
	if retryErr := os.Rename(tmp, dst); retryErr == nil {
		return nil
	}
	return err
}

// copyTree copies the directory src into dst.
//
// Every file is written through a scratch file and renamed into place, so a
// failure part way through leaves the files that did land complete, and the
// rest of the tree untouched.
func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		entryInfo, err := os.Lstat(srcPath)
		if err != nil {
			return err
		}
		switch {
		case entryInfo.Mode()&os.ModeSymlink != 0:
			if err := copySymlink(srcPath, dstPath); err != nil {
				return err
			}
		case entryInfo.IsDir():
			if err := copyTree(srcPath, dstPath); err != nil {
				return err
			}
		default:
			if err := copyFileAtomic(srcPath, dstPath, entryInfo.Mode()); err != nil {
				return err
			}
		}
	}
	return nil
}

// verifyTree checks that every file in src arrived in dst with the same
// size.
//
// The source of a move is only removed once this passes, so a copy that
// silently lost a file — a share that dropped the connection, a file that
// vanished mid-copy — cannot turn into a deletion.
func verifyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		targetInfo, err := os.Lstat(target)
		if err != nil {
			return fmt.Errorf("move: %s did not arrive: %w", rel, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// A link either arrived or it did not. Its size is the length
			// of the path it stores, which says nothing about the copy.
			return nil
		}
		if targetInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("move: %s arrived as a link", rel)
		}
		if info.IsDir() {
			return nil
		}
		if targetInfo.Size() != info.Size() {
			return fmt.Errorf("move: %s arrived short (%d of %d bytes)", rel, targetInfo.Size(), info.Size())
		}
		return nil
	})
}

// moveAcrossDevices moves src to dst by copying it across and then removing
// the original.
//
// The order matters, and it is the whole point of this function. The copy is
// written to a scratch file, flushed, checked against the source and only
// then renamed into place. The source is removed after that, and never
// before. If anything goes wrong the source is still sitting where it was,
// with its contents intact.
func moveAcrossDevices(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		// A link carries no data: the target it names is recreated as a
		// link at the destination, and only then is the original removed.
		if err := copySymlink(src, dst); err != nil {
			return err
		}
		return os.Remove(src)
	}

	if info.IsDir() {
		if err := copyTree(src, dst); err != nil {
			return err
		}
		if err := verifyTree(src, dst); err != nil {
			return err
		}
		return os.RemoveAll(src)
	}

	if err := copyFileAtomic(src, dst, info.Mode()); err != nil {
		return err
	}

	// The source has to be the file that was copied. A source that changed
	// while it was on its way is not what arrived at the destination, so
	// removing it would throw away something else.
	after, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if after.Size() != info.Size() || after.ModTime().UnixNano() != info.ModTime().UnixNano() {
		return fmt.Errorf("move: %s changed while it was being copied", src)
	}
	return os.Remove(src)
}

// syncDir flushes a directory entry so a rename survives a crash.
//
// Not every filesystem supports it — some answer a directory fsync with
// EINVAL — so a failure here is not worth reporting. The rename has already
// happened; this only decides whether it survives a power cut.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer d.Close()
	_ = d.Sync()
}

// linkTarget resolves the file a symlink points at.
//
// A relative link target is read against the directory holding the link, so
// this is what turns "photo.jpg" next to "link.jpg" into the file the link
// actually names.
func linkTarget(link, target string) string {
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(filepath.Dir(link), target)
}

// isLinkToItsOwnDestination reports whether dst is the file that the symlink
// at src points at.
//
// Putting the link in the destination's place would mean removing that
// file, and the link would then point at itself. The file the link names is
// the user's, so the copy is refused instead.
func isLinkToItsOwnDestination(src, dst, target string) bool {
	resolved, err := os.Stat(linkTarget(src, target))
	if err != nil {
		return false
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		return false
	}
	return os.SameFile(resolved, dstInfo)
}

// isCrossDevice reports whether err is a rename across filesystems.
//
// Go reports this as a link error rather than a typed one. The syscall
// number is checked first because it is exact; the wording is checked as
// well, since the phrase differs between kernels ("invalid cross-device
// link" on Linux, "Invalid cross-device link" on macOS) and an error built
// by hand in a test carries no syscall number at all.
func isCrossDevice(err error) bool {
	if errors.Is(err, syscall.EXDEV) {
		return true
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		msg := strings.ToLower(le.Err.Error())
		return strings.Contains(msg, "cross-device") ||
			strings.Contains(msg, "cross device")
	}
	return false
}

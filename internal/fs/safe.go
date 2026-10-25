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

	// The source is opened without blocking and confirmed to be a regular
	// file after the open. A FIFO swapped in between the caller's check and
	// this open then fails at once, rather than waiting for a writer that
	// may never come.
	in, err := openSourceNonBlocking(src)
	if err != nil {
		return err
	}
	defer in.Close()
	srcInfo, err := in.Stat()
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
		return restoreFailure(fmt.Errorf("copy: %s did not land: %w", dst, err), backup, dst)
	}
	if landed.Size() != written {
		// The old file goes back: what is at the destination is not the
		// file that was asked for.
		return restoreFailure(fmt.Errorf("copy: %s landed short (%d of %d bytes)", dst, landed.Size(), written), backup, dst)
	}
	// The replacement is good, so the old file can go.
	discardBackup(backup)

	// The rename itself has to survive a crash too, which means the
	// directory entry has to reach the disk.
	syncDir(dir)
	return nil
}

// openSourceNonBlocking opens path for reading and confirms the file that
// was opened is a regular file.
//
// O_NONBLOCK is what makes the open safe: opening a FIFO read-only normally
// waits for a writer that may never come, which would hang the copy forever.
// With the flag the open returns at once, and the post-open check refuses a
// FIFO, a socket or a device by name. A regular file is unaffected by the
// flag: its reads behave exactly as before.
func openSourceNonBlocking(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, &refusedEntry{verb: "copy", path: path}
	}
	return f, nil
}

// renameIntoPlaceFn is the rename replaceDestination uses to put a staged
// entry in place.
//
// It is a variable so a test can make the replacement fail after the previous
// destination has been held aside. A local filesystem will not produce that
// failure on demand, and it is exactly the path that has to put the old file
// back. It is nil-safe in production: the initial value is the real rename.
var renameIntoPlaceFn = renameIntoPlace

// RefusedReplacement reports that a destination was not replaced because the
// file already at that name could not be held aside.
//
// Replacing a destination without losing the old file means keeping the old
// file recoverable until the new one has landed and been checked. A filesystem
// that cannot make that backup — one without hard links, a full disk, a
// destination that cannot be read — is refused rather than replaced with no
// way back. Path is the destination, so the caller can tell the user which
// file was kept, and Err is why the backup failed.
//
// It is exported so the UI can recognise the refusal and show it whole.
// Unwrapping the cause directly would reduce the message to a bare reason such
// as "operation not supported", which does not say that nothing was replaced
// or why.
type RefusedReplacement struct {
	Path string
	Err  error
}

// Error names the destination and the reason the backup failed, so it reads as
// a one-line result: "cannot keep the previous <path>: <reason>".
//
// The reason is the bare cause rather than the os.LinkError wrapping it. The
// link's own "link old new: " prefix repeats the path already named here, and
// the operation is the copy's internals, not something the user chose.
func (e *RefusedReplacement) Error() string {
	if e.Err == nil {
		return "cannot keep the previous " + e.Path
	}
	return fmt.Sprintf("cannot keep the previous %s: %s", e.Path, errorReason(e.Err))
}

// Unwrap returns the backup failure, so errors.Is and errors.As still reach
// the cause behind the refusal.
func (e *RefusedReplacement) Unwrap() error { return e.Err }

// errorReason is the bare cause of a filesystem error: the reason without the
// os.LinkError and os.PathError prefixes that repeat the paths involved.
func errorReason(err error) string {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) && linkErr.Err != nil {
		err = linkErr.Err
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) && pathErr.Err != nil {
		err = pathErr.Err
	}
	return err.Error()
}

// replaceDestination puts the scratch file at dst, keeping the old
// destination recoverable until the replacement has been checked.
//
// The old destination is held by a hard link while the replacement is made,
// so a replacement that turns out not to have landed can be undone: the old
// file goes back to its name. The link is dropped once the new file has been
// confirmed.
//
// The backup is required. A filesystem that cannot make the link refuses the
// replacement instead of replacing the destination with no way back, and the
// refusal is a *RefusedReplacement so the caller can show it without losing
// the reason. A destination that is not there needs no backup, and the
// replacement is still atomic.
//
// The returned backup must be disposed of: remove it when the replacement
// is good, or restore it when it is not.
func replaceDestination(tmp, dst string) (string, error) {
	backup, err := backupDestinationStrict(dst)
	if err != nil {
		return "", &RefusedReplacement{Path: dst, Err: err}
	}
	if err := renameIntoPlaceFn(tmp, dst); err != nil {
		return "", restoreFailure(err, backup, dst)
	}
	return backup, nil
}

// backupDestinationStrict takes a hard link to the destination so the old
// file can be put back if the replacement does not land.
//
// A link rather than a copy: it costs nothing, and the old file keeps its
// content until the link is dropped. A destination that is not there needs no
// backup and returns an empty name with no error. Any other failure — a
// filesystem without links, a full disk, a destination that cannot be read —
// is returned, so the caller can refuse to replace a destination it cannot
// preserve.
func backupDestinationStrict(dst string) (string, error) {
	if _, err := os.Lstat(dst); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	res, err := os.CreateTemp(filepath.Dir(dst), tempPrefix+"*")
	if err != nil {
		return "", err
	}
	backup := res.Name()
	if err := res.Close(); err != nil {
		os.Remove(backup)
		return "", err
	}
	if err := os.Remove(backup); err != nil {
		return "", err
	}
	if err := os.Link(dst, backup); err != nil {
		return "", err
	}
	return backup, nil
}

// restoreBackup puts the old destination back after a replacement that did
// not land.
//
// The backup is a link to the old file, so renaming it back puts the old
// content at the destination's name. A failed restore does NOT delete the
// backup: it is the last recoverable link to the old content, so it stays on
// disk and the failure is returned to the caller, naming where the old file
// is kept.
func restoreBackup(backup, dst string) error {
	if backup == "" {
		return nil
	}
	if err := os.Rename(backup, dst); err != nil {
		return fmt.Errorf("the previous %s is kept at %s: %w", dst, backup, err)
	}
	// When the backup and the destination are two links to the same file,
	// POSIX says the rename is a no-op: it reports success and leaves both
	// names in place. The destination already holds the old file, so the
	// backup name is the copy's own leftover and can go.
	if backupInfo, err := os.Lstat(backup); err == nil {
		if dstInfo, dstErr := os.Lstat(dst); dstErr == nil && os.SameFile(backupInfo, dstInfo) {
			os.Remove(backup)
		}
	}
	return nil
}

// restoreFailure restores the held-aside destination and returns err. If the
// restore also fails, both failures are reported, and the backup is left on
// disk by restoreBackup.
func restoreFailure(err error, backup, dst string) error {
	if rbErr := restoreBackup(backup, dst); rbErr != nil {
		return fmt.Errorf("%w; %v", err, rbErr)
	}
	return err
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
		case !entryInfo.Mode().IsRegular():
			// A FIFO or device cannot be streamed; opening it can block
			// forever. Refuse it by name instead.
			return &refusedEntry{verb: "move", path: srcPath}
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
	// The destination was checked before the rename was tried, but it can
	// appear between that check and this copy. A move never replaces what is
	// already there, whatever filesystem it is on.
	if err := refuseExistingDestination(dst); err != nil {
		return err
	}

	info, err := os.Lstat(src)
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		// A link carries no data: the target it names is recreated as a
		// link at the destination, and only then is the original removed.
		//
		// The source has to still be that link before it is deleted. A
		// link repointed while the copy was in flight names something
		// else now, and removing it would throw away a name the user did
		// not move.
		original, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := copySymlink(src, dst); err != nil {
			return err
		}
		after, err := os.Lstat(src)
		if err != nil {
			return err
		}
		if after.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("move: %s changed while it was being copied", src)
		}
		now, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if now != original {
			return fmt.Errorf("move: %s changed while it was being copied", src)
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

	if !info.Mode().IsRegular() {
		return &refusedEntry{verb: "move", path: src}
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

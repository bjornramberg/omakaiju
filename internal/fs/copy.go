package fs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// copyChunk is how many bytes a single Step moves. Bounds how far a copy can
// overshoot a cancel request.
const copyChunk = 256 << 10

// progress is an immutable snapshot of a copy's state.
//
// The UI reads the snapshot on every frame while the copy runs on another
// goroutine, so the numbers it shows must belong to the same moment. A
// snapshot replaced as a whole guarantees that: the bar cannot report a byte
// count from one file and a file name from another.
type progress struct {
	// name is the file being written.
	name string
	// srcPath is the path the file being written was read from. A failed
	// transfer names it, so the entry can be offered for another try.
	srcPath string
	// fileDone and fileSize describe progress within that file.
	fileDone int64
	fileSize int64
	// copied is the total number of bytes written so far.
	copied int64
	// filesDone counts fully written files.
	filesDone int
	// dstPath is the scratch file being written, so an abort can remove it.
	dstPath string
	// finalPath is the name the scratch file takes once it is whole.
	finalPath string
}

// Copier copies a set of sources into a destination directory as a resumable
// state machine: the caller drives it with Step so all mutation happens on one
// goroutine. That keeps the UI loop free of blocking reads, channels, and locks,
// and makes the whole job deterministic and testable.
//
// The UI reads the progress through an atomic snapshot rather than through
// the fields themselves. Reading the fields directly raced with Step: the
// bounds check and the index expression in CurrentName were two separate
// reads of a counter the copy goroutine was advancing, which could index
// past the end of the job list.
type Copier struct {
	// state is the published snapshot. It is replaced, never mutated, so a
	// reader always sees a consistent set of numbers.
	state atomic.Pointer[progress]

	// aborted stops the copy at the next chunk boundary.
	aborted atomic.Bool

	// --- everything below is owned by the goroutine that calls Step ---

	jobs  []copyJob
	dirs  []dirJob
	index int

	src     *os.File
	dst     *os.File
	dstPath string

	// finalPath is the name the scratch file takes once it is whole.
	finalPath string

	fileDone int64
	fileSize int64
	copied   int64
	total    int64

	// jobMode is the permission bits of the file being written, so the
	// finished copy can carry them over.
	jobMode os.FileMode

	// move makes the copier remove each source once its copy has landed.
	// It is set for a move across filesystems, where a rename is not
	// available.
	move bool
	// renamed counts the entries a rename settled. A rename costs nothing,
	// so those entries are finished before the first step.
	renamed int
	// failures records the entries that could not be moved at all.
	failures []MoveFailure
	// srcDirs are the source directories a move emptied. The files are
	// removed as they land, so what is left behind are the directories
	// that held them.
	srcDirs []string
	// createdDirs are the directories the copy made for itself. A copy
	// that is cancelled or fails takes them away again, so the user's
	// folder is not left with an empty tree in it.
	createdDirs []string
	// pending counts the queued jobs that still have to run. It is fixed
	// once the copier is built, so it can be read from any goroutine.
	pending int
	// settled records that a move's sources have already been removed, so
	// the settling does not run twice.
	settled bool
	// settleOK records that the sources were removed, so a move that did
	// not finish can say so.
	settleOK bool

	filesDone int
}

// copyJob is one file to copy, with its size known ahead of time.
type copyJob struct {
	src  string
	dst  string
	size int64
	// mode carries the permission bits of the source, so the copy can keep
	// them.
	mode os.FileMode
	// modTime is when the source was last changed, as it was when the copy
	// was queued. A source that changed since then is not the file that was
	// copied, so a move leaves it alone.
	modTime int64
	// link is the target of a symlink to recreate. A job with isLink set
	// creates a symlink instead of copying bytes.
	link string
	// isLink marks a job that recreates a symlink rather than copying the
	// file it points at.
	isLink bool
}

// dirJob is a directory to create at the destination.
//
// The files a copy writes bring their parent directories with them, but a
// folder that holds no files has no file to bring it, so it is listed here
// and created on its own.
type dirJob struct {
	path string
	// src is the directory the destination was copied from. A move empties
	// the original, and this is where it goes once it is empty.
	src  string
	mode os.FileMode
}

// NewCopier scans srcs and prepares to copy them into dstDir. The total size is
// computed up front by stat alone, so the progress bar always has a denominator
// without reading any file data.
func NewCopier(srcs []string, dstDir string) (*Copier, error) {
	c := &Copier{}
	for _, src := range srcs {
		// Copying a directory into its own subtree would make the copy
		// discover the directories it is creating and nest forever.
		if inside, err := isInside(src, dstDir); err == nil && inside {
			return nil, fmt.Errorf("copy: cannot copy %s into itself", src)
		}
		if err := c.add(src, dstDir); err != nil {
			return nil, err
		}
	}
	c.total = c.scheduledBytes()
	c.pending = len(c.jobs) + len(c.dirs)
	c.publish()
	return c, nil
}

// Pending counts the work still to do: the files to copy and the
// directories to create.
//
// It is fixed once the copier is built, so it can be read from any
// goroutine. A mover with nothing pending is already finished: every entry
// it was given was settled by a rename, or there was nothing to carry —
// an empty folder still has to be created at the destination.
func (c *Copier) Pending() int { return c.pending }

// add queues src and, for directories, every file beneath it.
//
// Symlinks are queued as links rather than as the files they point at. A
// symlink to a directory is a directory as far as a stat is concerned, but
// walking it yields a file, and copying that "file" fails with "is a
// directory". Recreating the link is both correct and what the user meant.
func (c *Copier) add(src, dstDir string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}

	// Copying an entry onto itself would truncate the source before reading
	// it back. The depth guard below catches a directory copied into its own
	// subtree, but a copy at exactly the same path slips past it, so refuse
	// it here where the destination is known. The check is by identity, so
	// it also catches two different paths for one file — a symlinked
	// directory, or a share mounted in two places.
	dst := filepath.Join(dstDir, filepath.Base(src))
	if same, err := SameFile(src, dst); err == nil && same {
		return fmt.Errorf("copy: %s onto itself", src)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		c.jobs = append(c.jobs, copyJob{
			src:    src,
			dst:    dst,
			mode:   info.Mode(),
			link:   target,
			isLink: true,
		})
		return nil
	}
	if !info.IsDir() {
		c.jobs = append(c.jobs, copyJob{
			src:     src,
			dst:     dst,
			size:    info.Size(),
			mode:    info.Mode(),
			modTime: info.ModTime().UnixNano(),
		})
		return nil
	}

	// A directory is copied as a whole: it lands in the destination under
	// its own name, with everything inside it. A file lands under its own
	// name too, and the two have to agree — a copy that puts a folder's
	// contents loose in the target directory is not the copy the user
	// asked for, and it scatters the files over whatever was already
	// there.
	root := dst
	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if fi.IsDir() {
			// The directory has to exist even when it holds no files, or a
			// copy of an empty folder would produce nothing at all.
			c.dirs = append(c.dirs, dirJob{
				path: filepath.Join(root, rel),
				src:  filepath.Join(src, rel),
				mode: fi.Mode().Perm(),
			})
			if c.move {
				c.srcDirs = append(c.srcDirs, filepath.Join(src, rel))
			}
			return nil
		}
		job := copyJob{
			src:     path,
			dst:     filepath.Join(root, rel),
			size:    fi.Size(),
			mode:    fi.Mode(),
			modTime: fi.ModTime().UnixNano(),
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			job.link = target
			job.isLink = true
			job.size = 0
		}
		c.jobs = append(c.jobs, job)
		return nil
	})
}

func (c *Copier) scheduledBytes() int64 {
	var total int64
	for _, j := range c.jobs {
		total += j.size
	}
	return total
}

// Total is the byte count of everything queued. It is fixed once the copier
// is built, so it can be read from any goroutine.
func (c *Copier) Total() int64 { return c.total }

// FilesTotal counts queued files. Like Total, it never changes.
//
// A move counts the entries a rename settled as well: they are finished
// before the first step, and leaving them out would make the bar count
// entries that never existed.
func (c *Copier) FilesTotal() int {
	if c.move {
		return len(c.jobs) + c.renamed
	}
	return len(c.jobs)
}

// IsMove reports whether this is a move rather than a copy. A move removes
// the source once the copy has landed.
func (c *Copier) IsMove() bool { return c.move }

// snapshot is the last published state of the copy.
func (c *Copier) snapshot() progress {
	if p := c.state.Load(); p != nil {
		return *p
	}
	return progress{}
}

// publish records the current state for the UI to read.
func (c *Copier) publish() {
	c.state.Store(&progress{
		name:      c.currentName(),
		srcPath:   c.currentSource(),
		fileDone:  c.fileDone,
		fileSize:  c.fileSize,
		copied:    c.copied,
		filesDone: c.filesDone,
		dstPath:   c.dstPath,
	})
}

// Copied is the byte count finished so far.
func (c *Copier) Copied() int64 { return c.snapshot().copied }

// FilesDone counts fully written files.
func (c *Copier) FilesDone() int {
	done := c.snapshot().filesDone
	if c.move {
		done += c.renamed
	}
	return done
}

// CurrentName is the file being written, for display.
func (c *Copier) CurrentName() string { return c.snapshot().name }

// CurrentSource is the path of the file being written. It is what a failed
// transfer names, so the entry can be offered for another try.
func (c *Copier) CurrentSource() string { return c.snapshot().srcPath }

// FileCopied and FileSize describe progress within the current file.
func (c *Copier) FileCopied() int64 { return c.snapshot().fileDone }
func (c *Copier) FileSize() int64   { return c.snapshot().fileSize }

// Percent is overall completion across every queued file. Preferring the
// aggregate keeps the bar monotonic across file boundaries; the per-file
// position is reported separately by FileCopied and FileSize.
func (c *Copier) Percent() int {
	total := c.total
	if total <= 0 {
		return 100
	}
	p := int(c.snapshot().copied * 100 / total)
	if p > 100 {
		return 100
	}
	return p
}

// currentName is the file the copy is on. It reads the job list directly, so
// it may only be called from the goroutine driving Step.
func (c *Copier) currentName() string {
	if c.index < len(c.jobs) {
		return filepath.Base(c.jobs[c.index].src)
	}
	return ""
}

// currentSource is the path the file the copy is on was read from. Like
// currentName, it may only be called from the goroutine driving Step.
func (c *Copier) currentSource() string {
	if c.index < len(c.jobs) {
		return c.jobs[c.index].src
	}
	return ""
}

// Step copies up to copyChunk bytes. It reports done when every file is
// complete; any error is returned with the copier already closed.
func (c *Copier) Step() (done bool, err error) {
	if c.aborted.Load() {
		c.stop()
		return true, nil
	}

	// The directory structure is created before the first file lands, so a
	// folder that holds no files is still there once the copy is done. It
	// comes before the check for work left to do, because a copy of nothing
	// but empty folders is still a copy.
	if len(c.dirs) > 0 {
		for _, dir := range c.dirs {
			// A directory the copy had to make is remembered, so a copy
			// that is cancelled can take it away again rather than leave
			// an empty tree behind.
			if _, err := os.Lstat(dir.path); os.IsNotExist(err) {
				c.createdDirs = append(c.createdDirs, dir.path)
			}
			if err := os.MkdirAll(dir.path, dir.mode); err != nil {
				c.stop()
				return false, err
			}
		}
		c.dirs = nil
	}

	if c.index >= len(c.jobs) {
		return c.complete()
	}

	if c.dst == nil {
		if err := c.openCurrent(); err != nil {
			return false, err
		}
		// A symlink job is finished the moment the link exists: there are
		// no bytes to move.
		if c.jobs[c.index].isLink {
			c.index++
			c.filesDone++
			c.publish()
			if c.index >= len(c.jobs) {
				return c.complete()
			}
			return false, nil
		}
	}

	// io.CopyN writes at most n bytes and returns io.EOF when the source is
	// shorter, which is how we detect the end of this file.
	n, err := io.CopyN(c.dst, c.src, copyChunk)
	c.fileDone += n
	c.copied += n

	// An abort may have landed while the chunk was being written. The
	// destination is unlinked by the abort itself, so there is nothing left
	// to do but stop.
	if c.aborted.Load() {
		c.stop()
		return true, nil
	}

	switch {
	case err == nil:
		// More remains in this file.
		c.publish()
		return false, nil
	case err == io.EOF:
		// File finished; put it in place and try the next one.
		if err := c.finishCurrent(); err != nil {
			c.stop()
			return false, err
		}
		// An abort that landed while the last chunk was being written ends
		// the copy here rather than counting a file that never landed.
		if c.aborted.Load() {
			c.stop()
			return true, nil
		}
		c.filesDone++
		c.index++
		c.publish()
		if c.index >= len(c.jobs) {
			return c.complete()
		}
		return false, nil
	default:
		c.stop()
		return false, err
	}
}

// openCurrent opens the next queued file for reading and writing, or creates
// the next queued symlink.
//
// The bytes go to a scratch file beside the destination rather than to the
// destination itself, so the name the user expects only ever points at a
// whole file. An abort or a failure removes the scratch file and leaves the
// destination exactly as it was.
func (c *Copier) openCurrent() error {
	job := c.jobs[c.index]

	if err := os.MkdirAll(filepath.Dir(job.dst), 0o755); err != nil {
		return err
	}

	if job.isLink {
		// The destination may be the file the link points at, and putting
		// the link in its place would destroy it.
		if isLinkToItsOwnDestination(job.src, job.dst, job.link) {
			return fmt.Errorf("copy: %s points at its own destination", job.src)
		}
		// os.Symlink fails if the path exists, and the destination may be
		// left over from an earlier run.
		if err := os.Remove(job.dst); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Symlink(job.link, job.dst); err != nil {
			return err
		}
		// The source of a move goes with the rest of the sources, once
		// everything has landed.
		c.src = nil
		c.dst = nil
		c.dstPath = ""
		c.finalPath = ""
		c.fileDone = 0
		c.fileSize = 0
		return nil
	}

	// The destination and the source can be two names for one file — a
	// symlinked directory, a share mounted twice — and writing to the
	// destination would then truncate the source before a byte of it was
	// read.
	same, err := SameFile(job.src, job.dst)
	if err != nil {
		return err
	}
	if same {
		return fmt.Errorf("copy: %s would overwrite itself", job.src)
	}

	src, err := os.Open(job.src)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(job.dst), tempPrefix+"*")
	if err != nil {
		src.Close()
		return err
	}

	c.src = src
	c.dst = tmp
	c.dstPath = tmp.Name()
	c.finalPath = job.dst
	c.jobMode = job.mode
	c.fileDone = 0
	c.fileSize = job.size
	// Publish the scratch file's path straight away, so an abort that
	// arrives before the first chunk is written can remove it.
	c.publish()
	return nil
}

// finishCurrent puts a finished copy in place.
//
// The scratch file is flushed, checked against what was written, given the
// source's permissions and renamed over the destination. The rename is
// atomic, so the destination is never seen half-written: it holds either the
// old content or the whole new one.
//
// The destination that was there before is held by a link until the new file
// has been confirmed, so a replacement that turns out not to have landed can
// be undone and the old file put back.
//
// Nothing is removed until the copy has landed and been checked.
func (c *Copier) finishCurrent() error {
	if c.src != nil {
		if err := c.src.Close(); err != nil {
			c.src = nil
			c.closeCurrent()
			return err
		}
		c.src = nil
	}
	if c.dst == nil {
		return nil
	}

	tmp := c.dst
	c.dst = nil
	name := c.dstPath
	final := c.finalPath
	c.dstPath = ""
	c.finalPath = ""

	// From here on the scratch file belongs to this function, so every
	// failure takes it away again.
	fail := func(err error) error {
		os.Remove(name)
		return err
	}

	// The bytes have to be on the disk before the name points at them.
	// Without this, a crash — or a server that drops the connection on a
	// share — can leave the destination naming a file with no content.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		return fail(err)
	}
	// An abort may have landed while the last chunk was being written. The
	// scratch file goes and the copy is abandoned, which is what the user
	// asked for.
	if c.aborted.Load() {
		return fail(nil)
	}

	// The copy has to be whole before it is allowed to replace anything. A
	// source that came out short of what it promised is not the file that
	// was asked for.
	if c.fileDone < c.fileSize {
		return fail(fmt.Errorf("copy: %s is incomplete (%d of %d bytes)", c.currentName(), c.fileDone, c.fileSize))
	}
	// The scratch file has to hold what was written to it. A share that
	// accepted the write but kept less of it than was sent would otherwise
	// put an empty file in the destination's place.
	written, err := os.Stat(name)
	if err != nil {
		return fail(err)
	}
	if written.Size() != c.fileDone {
		return fail(fmt.Errorf("copy: %s is incomplete (%d of %d bytes)", c.currentName(), written.Size(), c.fileDone))
	}

	if c.jobMode.Perm() != 0 {
		if err := os.Chmod(name, c.jobMode.Perm()); err != nil {
			return fail(err)
		}
	}

	backup, err := replaceDestination(name, final)
	if err != nil {
		os.Remove(name)
		return err
	}
	syncDir(filepath.Dir(final))

	// The name is on the file, but the file is only really there if it can
	// be seen at its new name with its content.
	landed, err := os.Stat(final)
	if err != nil {
		restoreBackup(backup, final)
		return fmt.Errorf("copy: %s did not land: %w", final, err)
	}
	if landed.Size() != c.fileDone {
		restoreBackup(backup, final)
		return fmt.Errorf("copy: %s landed short (%d of %d bytes)", final, landed.Size(), c.fileDone)
	}
	// The replacement is good, so the old file can go.
	discardBackup(backup)
	return nil
}

// closeCurrent closes the open files without putting the copy in place. The
// scratch file is left for stop to remove.
func (c *Copier) closeCurrent() {
	if c.src != nil {
		c.src.Close()
		c.src = nil
	}
	if c.dst != nil {
		c.dst.Close()
		c.dst = nil
	}
	c.dstPath = ""
	c.finalPath = ""
}

// complete reports that the transfer has finished.
//
// For a move this is where the sources go: once every file has landed and
// been checked, the originals can be removed.
func (c *Copier) complete() (bool, error) {
	if err := c.settleMove(); err != nil {
		return false, err
	}
	// The copy landed, so the directories it made are the user's now and
	// are not to be taken away.
	c.createdDirs = nil
	return true, nil
}

// settleMove removes the sources of a move, now that every file has landed
// and been checked.
//
// The sources go together or not at all. A file that failed to copy leaves
// its source exactly where it was, so a transfer that stopped half way never
// leaves the user with half a move and a hole where the rest of it was.
//
// The check comes first: every destination has to be there, whole, and the
// size the source promised before any source is removed. A source that
// changed while it was being copied is not the file that was copied, so
// removing it would throw away something else.
func (c *Copier) settleMove() error {
	if !c.move || c.settled {
		return nil
	}
	c.settled = true

	var problems []string
	for _, job := range c.jobs {
		if job.isLink {
			// A link either arrived or it did not. Its size is the length
			// of the path it stores, which says nothing about the copy.
			if _, err := os.Lstat(job.dst); err != nil {
				problems = append(problems, fmt.Sprintf("%s did not arrive", job.dst))
			}
			continue
		}
		src, err := os.Lstat(job.src)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s could not be checked", job.src))
			continue
		}
		if src.Size() != job.size {
			problems = append(problems, fmt.Sprintf("%s changed while it was being copied", job.src))
			continue
		}
		if job.modTime != 0 && src.ModTime().UnixNano() != job.modTime {
			problems = append(problems, fmt.Sprintf("%s changed while it was being copied", job.src))
			continue
		}
		dst, err := os.Stat(job.dst)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s did not arrive", job.dst))
			continue
		}
		if dst.Size() != job.size {
			problems = append(problems, fmt.Sprintf("%s arrived short", job.dst))
		}
	}
	if len(problems) > 0 {
		// Nothing is removed: the sources stay, so the move can be tried
		// again once the cause is fixed.
		return fmt.Errorf("move: %s", strings.Join(problems, ", "))
	}
	c.settleOK = true

	// Everything arrived, so the sources can go. A source that refuses to
	// be removed is reported, but the rest still go: the copies are all
	// there, so nothing is lost by finishing the move.
	for _, job := range c.jobs {
		if err := os.Remove(job.src); err != nil && !os.IsNotExist(err) {
			problems = append(problems, fmt.Sprintf("%s could not be removed", job.src))
		}
	}
	c.removeEmptySourceDirs()

	if len(problems) > 0 {
		return fmt.Errorf("move: %s", strings.Join(problems, ", "))
	}
	return nil
}

// removeEmptySourceDirs removes the directories a move emptied.
//
// The files were removed as they landed, so what is left behind are the
// directories that held them. os.Remove only succeeds on an empty
// directory, so a directory that still holds something is left alone rather
// than being emptied.
func (c *Copier) removeEmptySourceDirs() {
	for i := len(c.srcDirs) - 1; i >= 0; i-- {
		_ = os.Remove(c.srcDirs[i])
	}
	c.srcDirs = nil
}

// stop unwinds a cancelled or failed copy: the open files are closed and the
// scratch file is removed. It runs on the goroutine that owns the copy, so
// it can touch the copy's own state.
//
// The destination is never touched. A copy that was cancelled or that failed
// leaves whatever was already there in place.
func (c *Copier) stop() {
	partial := c.dstPath
	c.closeCurrent()
	c.dirs = nil
	c.index = len(c.jobs)
	c.publish()
	if partial != "" {
		// The abort usually removed it already; this covers the case where
		// the copy was stopped by an error instead.
		_ = os.Remove(partial)
	}
	c.removeCreatedDirs()
}

// removeCreatedDirs takes away the directories the copy made for itself.
//
// A copy that was cancelled or that failed leaves the user's folder as it
// was, rather than with an empty tree in it. os.Remove only succeeds on an
// empty directory, so a directory that has something in it is left alone.
func (c *Copier) removeCreatedDirs() {
	for i := len(c.createdDirs) - 1; i >= 0; i-- {
		_ = os.Remove(c.createdDirs[i])
	}
	c.createdDirs = nil
}

// Abort stops the copy and removes the partially written destination file.
// Files already finished are left in place, since they are complete.
//
// It is safe to call while a step is in flight on another goroutine. The
// stop is signalled with a flag the owner checks at the next chunk boundary,
// and the partial file is unlinked here: on POSIX, removing a file that is
// still open leaves the writer on its now-unlinked inode and frees the path
// immediately, which is what the user asked for.
func (c *Copier) Abort() error {
	c.aborted.Store(true)
	partial := c.snapshot().dstPath
	if partial == "" {
		return nil
	}
	if err := os.Remove(partial); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

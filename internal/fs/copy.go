package fs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
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

	// stepping marks that a Step is running. Abort only touches the fields
	// below when it can take this flag for itself, so a cancel on the UI
	// goroutine never races a step for the state Step owns.
	stepping atomic.Bool
	// cleaned records that the copy has been unwound. Abort, Step and Close
	// can all arrive; exactly one of them does the teardown.
	cleaned atomic.Bool

	// --- everything below is owned by the goroutine that calls Step ---

	jobs  []copyJob
	dirs  []dirJob
	index int

	src     *os.File
	dst     *os.File
	dstPath string

	// finalPath is the name the scratch file takes once it is whole.
	finalPath string

	// linkScratch is a symlink staged beside its destination, waiting for
	// the next step to rename it into place. Nothing at the destination is
	// touched until that rename.
	linkScratch string
	// linkFinal is the name the staged link takes.
	linkFinal string
	// linkTarget is the target the staged link stores.
	linkTarget string

	fileDone int64
	fileSize int64
	copied   int64
	// total is the byte count of the whole transfer. It is atomic because
	// a rename that turns out to cross devices at execution time queues a
	// fallback copy and grows it, while the UI reads Total and Percent on
	// every frame.
	total atomic.Int64

	// jobMode is the full mode of the file being written, so the finished
	// copy can carry it over. Perm is not enough: the setuid, setgid and
	// sticky bits are part of the mode too.
	jobMode os.FileMode
	// jobTimes are the source's times for the file being written. They are
	// set on the scratch file before it is renamed into place.
	jobTimes copyTimes

	// move makes the copier remove each source once its copy has landed.
	// It is set for a move across filesystems, where a rename is not
	// available.
	move bool
	// renamed counts the entries a rename settled. A rename costs nothing,
	// so those entries are counted as soon as they have been renamed. It is
	// atomic because the UI reads FilesDone and FilesTotal every frame while
	// the step goroutine advances the count.
	renamed atomic.Int64
	// movedJobs counts the entries a copy carried whose source has really
	// been removed. A source that refused to go is not a moved entry, so it
	// is not counted here. Atomic for the same reason as renamed.
	movedJobs atomic.Int64
	// filesTotal is the number of entries the transfer counts as its total.
	// It is atomic because a rename that falls back to a copy at execution
	// time adds more jobs while the UI is reading FilesTotal.
	filesTotal atomic.Int64
	// renames are the entries a move settles with a rename. Planning only
	// fills this queue: the renames run during the settle phase, after any
	// copy jobs have landed and been verified.
	renames []renameJob
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
	// createdMeta are the directories the copy made whose source's mode
	// and times it knows. A directory cannot be given its final mode or
	// times while its contents are still being written: writing moves its
	// time forward again, and a read-only mode would stop the copy. They
	// are applied once the whole copy has landed.
	createdMeta []createdDir
	// copyPending counts the copy jobs and directories that still have to
	// run. It is atomic because a UI may read it while Step advances.
	copyPending atomic.Int64
	// renamePending counts the renames that still have to run. Pending
	// includes them, so the caller cannot declare a move finished before
	// its renames have happened.
	renamePending atomic.Int64
	// settledJobs counts the copy jobs whose landing has been verified and
	// whose source has been removed. A move can settle more than once when
	// a rename turns out to cross devices at execution time and falls back
	// to a copy.
	settledJobs int
	// dstDir is the directory a move's entries are landing in, and
	// dstDirExisted records that it was there at planning time. A move does
	// not silently recreate a destination that vanished under it: that
	// would be a different move from the one the user started.
	dstDir        string
	dstDirExisted bool

	filesDone int
}

// copyJob is one file to copy, with its size known ahead of time.
type copyJob struct {
	src  string
	dst  string
	size int64
	// mode carries the source's full mode, so the copy can keep it
	// exactly, setuid, setgid and sticky bits included.
	mode os.FileMode
	// times are the source's access and modification times, as they were
	// when the copy was queued. They are set on the scratch file before it
	// is renamed into place.
	times copyTimes
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

// refusedEntry is an entry the copy will not stream because it is not a
// regular file.
//
// Opening a FIFO waits for a writer that may never come, and a device such as
// a character device has no end to copy to. Neither is something a copy can
// carry, so the entry is reported and the rest of the batch carries on.
type refusedEntry struct {
	verb string
	path string
}

func (e *refusedEntry) Error() string {
	return fmt.Sprintf("%s: %s is not a regular file", e.verb, e.path)
}

// errStopped reports that a cancel landed while an entry was about to be put
// in place. The caller unwinds the copy instead of counting the entry.
var errStopped = errors.New("copy: cancelled")

// beforeCommitHook runs between the last cancel check and the commit. It is
// nil in production. A test uses it to land a cancel inside the window
// between the check and the rename, which cannot be scheduled from another
// goroutine deterministically.
var beforeCommitHook func()

// runBeforeCommitHook calls the test seam when one is installed.
func runBeforeCommitHook() {
	if beforeCommitHook != nil {
		beforeCommitHook()
	}
}

// refuseExistingDestination reports the per-entry failure a move uses when the
// destination path is already taken.
//
// Lstat is deliberate: a symlink is a destination too, and must not be
// followed or replaced. A path that cannot be checked at all is refused rather
// than assumed free.
func refuseExistingDestination(dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("move: %s already exists", dst)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("move: cannot check %s: %w", dst, err)
	}
	return nil
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
	src string
	// mode is the source directory's full mode, including the setuid,
	// setgid and sticky bits. It is applied once the directory holds
	// everything it is meant to.
	mode os.FileMode
	// times are the source directory's times. Writing the contents moves
	// the directory's time forward again, so they are applied last.
	times copyTimes
}

// createdDir is a directory the copy made, with the metadata of the source
// directory it stands for.
//
// A directory that was already at the destination is not one of these: it is
// the user's, and the copy leaves its mode and times alone.
type createdDir struct {
	path  string
	mode  os.FileMode
	times copyTimes
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
	c.total.Store(c.scheduledBytes())
	c.filesTotal.Store(int64(len(c.jobs)))
	c.copyPending.Store(int64(len(c.jobs) + len(c.dirs)))
	c.publish()
	return c, nil
}

// Pending counts the work still to do: the files to copy, the directories to
// create, and the entries still waiting to be renamed.
//
// Renames count as outstanding work even though they cost nothing: the app
// uses Pending() == 0 to mean the move is finished, and a rename-only move
// is not finished until its renames have run. It may be read from any
// goroutine.
func (c *Copier) Pending() int {
	return int(c.copyPending.Load() + c.renamePending.Load())
}

// CopyPending counts the byte-copy work still to do: the file and directory
// jobs. It does not count renames, which need no progress bar.
//
// A caller can use it to tell a rename-only move — Pending() > 0 with
// CopyPending() == 0 — from one that actually has to carry bytes. It may be
// read from any goroutine.
func (c *Copier) CopyPending() int { return int(c.copyPending.Load()) }

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
		// Only regular files are streamed. A FIFO would block forever in
		// open, and a device can never be read to its end, so both are
		// refused here rather than at open time.
		if !info.Mode().IsRegular() {
			c.recordFailure(src, c.refuseRegular(src))
			return nil
		}
		c.jobs = append(c.jobs, copyJob{
			src:     src,
			dst:     dst,
			size:    info.Size(),
			mode:    info.Mode(),
			times:   timesOf(info),
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
				path:  filepath.Join(root, rel),
				src:   filepath.Join(src, rel),
				mode:  fi.Mode(),
				times: timesOf(fi),
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
			times:   timesOf(fi),
			modTime: fi.ModTime().UnixNano(),
		}
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			job.link = target
			job.isLink = true
			job.size = 0
		case !fi.Mode().IsRegular():
			// A FIFO, socket or device inside a tree is refused on its
			// own: the rest of the tree still copies.
			c.recordFailure(path, c.refuseRegular(path))
			return nil
		}
		c.jobs = append(c.jobs, job)
		return nil
	})
}

// refuseRegular builds the per-entry error for an entry that is not a
// regular file. The verb matches the operation, so a move's report reads like
// a move's.
func (c *Copier) refuseRegular(path string) *refusedEntry {
	verb := "copy"
	if c.move {
		verb = "move"
	}
	return &refusedEntry{verb: verb, path: path}
}

// recordFailure notes an entry the transfer could not carry, so the caller
// can report it without losing the rest of the batch.
func (c *Copier) recordFailure(path string, err error) {
	c.failures = append(c.failures, MoveFailure{
		Name: filepath.Base(path),
		Path: path,
		Err:  err,
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
func (c *Copier) Total() int64 { return c.total.Load() }

// FilesTotal counts queued files. Like Total, it never changes once
// planning is over, except for a rename that falls back to a copy at
// execution time.
//
// A move counts the entries waiting to be renamed, and the ones a rename
// already settled, as well: they are part of the move and leaving them out
// would make the bar count entries that never existed.
func (c *Copier) FilesTotal() int {
	return int(c.filesTotal.Load())
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
		done += int(c.renamed.Load())
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
	total := c.total.Load()
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
//
// Step owns the fields below the published snapshot for the duration of the
// call. Abort may be called from another goroutine: it only signals, and this
// call performs the teardown when it observes the signal. Exactly one of the
// two ever unwinds the copy; see Abort and Close.
func (c *Copier) Step() (done bool, err error) {
	c.acquireStep()
	defer func() {
		c.stepping.Store(false)
		// A cancel that landed while this step ran is unwound here, so it
		// is not left to a Step that may never come.
		c.stopIfAborted()
	}()

	if c.aborted.Load() {
		c.stop()
		return true, nil
	}

	// The directory structure is created before the first file lands, so a
	// folder that holds no files is still there once the copy is done. It
	// comes before the check for work left to do, because a copy of nothing
	// but empty folders is still a copy.
	if len(c.dirs) > 0 {
		// A move does not recreate a destination directory that existed
		// when it was planned and has since gone. The copy would land
		// somewhere the user no longer has open.
		if c.move && c.dstDirExisted {
			if _, err := os.Stat(c.dstDir); err != nil {
				c.stop()
				return false, err
			}
		}
		dirs := len(c.dirs)
		for _, dir := range c.dirs {
			// The directory is created writable by its owner so the copy
			// can fill it. Its exact mode is applied later, once it holds
			// everything: a read-only source directory would otherwise
			// refuse the contents meant for it, and writing them moves its
			// time forward again.
			created, err := mkdirAllTracked(dir.path, dir.mode|0o700, &c.createdDirs)
			if err != nil {
				c.stop()
				return false, err
			}
			if created {
				c.createdMeta = append(c.createdMeta, createdDir{
					path:  dir.path,
					mode:  dir.mode,
					times: dir.times,
				})
			}
		}
		c.dirs = nil
		c.copyPending.Add(-int64(dirs))
	}

	if c.index >= len(c.jobs) {
		return c.complete()
	}

	// A symlink is staged beside its destination by one step and renamed
	// into place by the next. Nothing is ever removed to make room for it,
	// so a cancel between the two leaves the destination exactly as it was.
	if c.jobs[c.index].isLink {
		if c.linkScratch == "" {
			if err := c.openCurrent(); err != nil {
				return false, err
			}
			return false, nil
		}
		if err := c.finishLink(); err != nil {
			c.stop()
			if errors.Is(err, errStopped) {
				return true, nil
			}
			return false, err
		}
		c.filesDone++
		c.index++
		c.copyPending.Add(-1)
		c.publish()
		if c.index >= len(c.jobs) {
			return c.complete()
		}
		return false, nil
	}

	if c.dst == nil {
		if err := c.openCurrent(); err != nil {
			var refused *refusedEntry
			if errors.As(err, &refused) {
				// The entry stopped being a regular file between being
				// queued and being opened. It is reported on its own,
				// and the rest of the batch carries on.
				c.recordFailure(refused.path, err)
				c.index++
				c.copyPending.Add(-1)
				c.publish()
				if c.index >= len(c.jobs) {
					return c.complete()
				}
				return false, nil
			}
			c.recordStepFailure(err)
			// openCurrent may already have created the entry's parent
			// directories. A failed step leaves the destination as it was,
			// so the teardown takes them away and closes anything open.
			c.stop()
			return false, err
		}
	}

	// io.CopyN writes at most n bytes and returns io.EOF when the source is
	// shorter, which is how we detect the end of this file.
	n, err := io.CopyN(c.dst, c.src, copyChunk)
	c.fileDone += n
	c.copied += n

	// An abort may have landed while the chunk was being written. The
	// teardown below removes the scratch file and closes the handles, so
	// there is nothing left to do but stop.
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
			// A cancel that landed in the last moment before the commit is
			// not a failure: the destination was left as it was and the
			// copy simply stops.
			if errors.Is(err, errStopped) {
				c.stop()
				return true, nil
			}
			c.recordStepFailure(err)
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
		c.copyPending.Add(-1)
		c.publish()
		if c.index >= len(c.jobs) {
			return c.complete()
		}
		return false, nil
	default:
		c.recordStepFailure(err)
		c.stop()
		return false, err
	}
}

// openCurrent opens the next queued file for reading and writing, or stages
// the next queued symlink.
//
// The bytes go to a scratch file beside the destination rather than to the
// destination itself, so the name the user expects only ever points at a
// whole file. A symlink is staged the same way: it is made under a scratch
// name and only renamed into place once it is whole. An abort or a failure
// removes the scratch entry and leaves the destination exactly as it was.
func (c *Copier) openCurrent() error {
	job := c.jobs[c.index]

	// A destination directory that was there at planning time and has
	// since gone is not recreated for a move: the copy fails instead of
	// landing somewhere the user no longer has open.
	if c.move && c.dstDirExisted {
		if _, err := os.Stat(c.dstDir); err != nil {
			return err
		}
	}

	dir := filepath.Dir(job.dst)

	// Everything that can refuse the entry is checked, and the source is
	// opened, before the destination's directories are made. A refusal
	// then leaves nothing behind to unwind: the copy did not create
	// anything for it.
	var src *os.File
	if job.isLink {
		// The destination may be the file the link points at, and putting
		// the link in its place would destroy it.
		if isLinkToItsOwnDestination(job.src, job.dst, job.link) {
			return fmt.Errorf("copy: %s points at its own destination", job.src)
		}
	} else {
		// Only a regular file may be opened and streamed. A source that
		// changed into a FIFO or a device since it was queued would
		// otherwise block the copy for good, or never end.
		info, err := os.Lstat(job.src)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return c.refuseRegular(job.src)
		}

		// The destination and the source can be two names for one file — a
		// symlinked directory, a share mounted twice — and writing to the
		// destination would then truncate the source before a byte of it
		// was read.
		same, err := SameFile(job.src, job.dst)
		if err != nil {
			return err
		}
		if same {
			return fmt.Errorf("copy: %s would overwrite itself", job.src)
		}

		// The source is opened without blocking, and checked again after
		// the open, so a file swapped for a FIFO between the check above
		// and the open fails at once instead of waiting for a writer that
		// may never come.
		src, err = openSourceNonBlocking(job.src)
		if err != nil {
			var refused *refusedEntry
			if errors.As(err, &refused) {
				return c.refuseRegular(job.src)
			}
			return err
		}
	}

	if _, err := mkdirAllTracked(dir, 0o755, &c.createdDirs); err != nil {
		if src != nil {
			src.Close()
		}
		return err
	}

	if job.isLink {
		// The link is made inside the destination's own directory, so the
		// rename that puts it in place is atomic and never crosses a
		// filesystem.
		scratch, err := scratchName(dir)
		if err != nil {
			return err
		}
		if err := os.Symlink(job.link, scratch); err != nil {
			os.Remove(scratch)
			return err
		}
		// The source of a move goes with the rest of the sources, once
		// everything has landed.
		c.src = nil
		c.dst = nil
		c.dstPath = scratch
		c.finalPath = job.dst
		c.linkScratch = scratch
		c.linkFinal = job.dst
		c.linkTarget = job.link
		c.fileDone = 0
		c.fileSize = 0
		c.publish()
		return nil
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
	c.jobTimes = job.times
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
// source's mode and times and renamed over the destination. The rename is
// atomic, so the destination is never seen half-written: it holds either the
// old content or the whole new one.
//
// The mode and times are set on the scratch file before the rename, so a
// failure to apply them takes the scratch file away and leaves the
// destination exactly as it was.
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

	// The scratch file was created with the default mode, so the exact bits
	// are set here — the permission bits and the setuid, setgid and sticky
	// bits the source carried. A copy that loses a bit is not the file the
	// user asked for.
	//
	// The times go on the scratch file too, before the rename. A copy that
	// kept the moment it was made instead of the source's time would look
	// newer than the file it came from, which is what many tools compare.
	if err := setCopyMode(name, c.jobMode); err != nil {
		return fail(err)
	}
	if err := setCopyTimes(name, c.jobTimes); err != nil {
		return fail(err)
	}

	// A move never replaces a destination that is already there. Planning
	// refused the entries it already knew about, but the destination can
	// appear between planning and this commit; the check is repeated here,
	// with Lstat, so a symlink counts as something that is already there.
	if c.move {
		if err := refuseExistingDestination(final); err != nil {
			return fail(err)
		}
	}

	runBeforeCommitHook()

	// The cancel is re-checked as late as it can be: this is the last
	// statement before the destination is replaced. The window between this
	// load and the rename below is the smallest the code can make it, and a
	// cancel that lands in it would have to be observed by the rename
	// itself. Inside the window the destination is still untouched, so a
	// cancel restores nothing but the scratch.
	if c.aborted.Load() {
		return fail(errStopped)
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
		return restoreFailure(fmt.Errorf("copy: %s did not land: %w", final, err), backup, final)
	}
	if landed.Size() != c.fileDone {
		return restoreFailure(fmt.Errorf("copy: %s landed short (%d of %d bytes)", final, landed.Size(), c.fileDone), backup, final)
	}
	// The replacement is good, so the old file can go.
	discardBackup(backup)
	return nil
}

// finishLink puts a staged symlink in place.
//
// The link was made beside the destination, so the rename that puts it there
// is atomic and within one filesystem: a reader sees either the entry that
// was already there or the whole new link. The destination is never removed
// first.
//
// An existing destination is held aside the same way a file copy holds one,
// so a rename that does not land can be undone and the old entry put back.
// Nothing is removed until the link has landed and been checked.
func (c *Copier) finishLink() error {
	scratch := c.linkScratch
	final := c.linkFinal
	target := c.linkTarget
	c.linkScratch = ""
	c.linkFinal = ""
	c.linkTarget = ""
	c.dstPath = ""
	c.finalPath = ""

	// From here on the staged link belongs to this function, so every
	// failure takes it away again.
	fail := func(err error) error {
		os.Remove(scratch)
		return err
	}

	// The staged entry has to be the link that was asked for.
	staged, err := os.Readlink(scratch)
	if err != nil {
		return fail(err)
	}
	if staged != target {
		return fail(fmt.Errorf("copy: %s was staged with the wrong target", final))
	}

	// A move never replaces a destination that is already there. The check
	// is by Lstat, so a symlink counts as existing and is not followed.
	if c.move {
		if err := refuseExistingDestination(final); err != nil {
			return fail(err)
		}
	}

	runBeforeCommitHook()

	// A cancel that landed before the link is put in place leaves the
	// destination alone: the link was never there, so there is nothing to
	// undo. The check is the last statement before the rename, which keeps
	// the window between the check and the commit as small as the code can
	// make it.
	if c.aborted.Load() {
		return fail(errStopped)
	}

	backup, err := replaceDestination(scratch, final)
	if err != nil {
		os.Remove(scratch)
		return err
	}
	syncDir(filepath.Dir(final))

	// The name is on the link, but it is only really there if it can be
	// seen at its new name, as a link, naming the target that was asked
	// for.
	landed, err := os.Lstat(final)
	if err != nil {
		return restoreFailure(fmt.Errorf("copy: %s did not land: %w", final, err), backup, final)
	}
	if landed.Mode()&os.ModeSymlink == 0 {
		return restoreFailure(fmt.Errorf("copy: %s did not land as a link", final), backup, final)
	}
	landedTarget, err := os.Readlink(final)
	if err != nil {
		return restoreFailure(fmt.Errorf("copy: %s did not land: %w", final, err), backup, final)
	}
	if landedTarget != target {
		return restoreFailure(fmt.Errorf("copy: %s landed with the wrong target", final), backup, final)
	}
	// The replacement is good, so the old entry can go.
	discardBackup(backup)
	return nil
}

// closeCurrent closes the open files without putting the copy in place. The
// scratch entry is left for stop to remove.
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
	c.linkScratch = ""
	c.linkFinal = ""
	c.linkTarget = ""
}

// complete reports that the transfer has finished.
//
// For a move this is where the sources go: once every file has landed and
// been checked, the originals can be removed.
func (c *Copier) complete() (bool, error) {
	if err := c.settleMove(); err != nil {
		// The move has stopped: the sources stay where they are and no
		// rename will run. A UI asking Pending must not be told there is
		// work left that never will.
		c.copyPending.Store(0)
		c.renamePending.Store(0)
		return false, err
	}
	// The tree is whole, so the directories can take their exact mode and
	// the source's times. This cannot happen when they are created: writing
	// their contents moves the time forward, and a read-only mode would
	// refuse the contents.
	if err := c.finishDirs(); err != nil {
		c.stop()
		return false, err
	}
	// The copy landed, so the directories it made are the user's now and
	// are not to be taken away.
	c.createdDirs = nil

	// The renames run only now: every cross-device copy has landed and
	// been verified, so a copy that failed left the cheap entries where
	// they were. A rename that falls back to a copy at this point queues
	// the copy and keeps the move unfinished; the next step carries it.
	if c.runRenames() {
		return false, nil
	}
	return true, nil
}

// finishDirs gives every directory the copy made its source's mode and times.
//
// It runs once the copy is whole, because writing a directory's contents moves
// its time forward again: a directory's time can only be set when nothing else
// is going to be written into it. The deepest directory is finished first, so
// a parent is still traversable while its children are being finalised.
//
// A failure is recorded against the directory and returned, never dropped: a
// directory that arrives without its mode could be readable by people it was
// not meant for, and one that arrives without its time makes the tree look
// newer than it is.
func (c *Copier) finishDirs() error {
	for i := len(c.createdMeta) - 1; i >= 0; i-- {
		dir := c.createdMeta[i]
		if err := setCopyMode(dir.path, dir.mode); err != nil {
			err = fmt.Errorf("copy: %s: cannot set the directory's mode: %w", dir.path, err)
			c.recordFailure(dir.path, err)
			return err
		}
		if err := setCopyTimes(dir.path, dir.times); err != nil {
			err = fmt.Errorf("copy: %s: cannot set the directory's times: %w", dir.path, err)
			c.recordFailure(dir.path, err)
			return err
		}
	}
	c.createdMeta = nil
	return nil
}

// settleMove removes the sources of a move, now that every file has landed
// and been checked.
//
// The sources of the jobs settled so far go together or not at all. A file
// that failed to copy leaves its source exactly where it was, so a transfer
// that stopped half way never leaves the user with half a move and a hole
// where the rest of it was.
//
// The check comes first: every destination has to be there, whole, and the
// size the source promised before any source is removed. A source that
// changed while it was being copied is not the file that was copied, so
// removing it would throw away something else.
//
// A move can settle more than once: a rename that turns out to cross devices
// at execution time queues a new copy job, and the next settle verifies that
// job on its own without re-checking sources that have already gone.
func (c *Copier) settleMove() error {
	if !c.move {
		return nil
	}
	if c.settledJobs >= len(c.jobs) {
		// There is no new copy work to verify. A tree that held no files
		// still leaves the directories it was carried in, so those are
		// removed here once everything else has landed.
		c.removeEmptySourceDirs()
		return nil
	}

	jobs := c.jobs[c.settledJobs:]
	var problems []string
	// fail records a problem for the user's report as well as for the
	// error the step returns. The entry stays in Failures so the app can
	// name it and offer it for another try.
	fail := func(job copyJob, err error) {
		problems = append(problems, err.Error())
		c.failures = append(c.failures, MoveFailure{
			Name: filepath.Base(job.src),
			Path: job.src,
			Err:  err,
		})
	}

	for _, job := range jobs {
		if job.isLink {
			// A link either arrived or it did not. Its size is the length
			// of the path it stores, which says nothing about the copy.
			if err := verifyLinkSource(job); err != nil {
				fail(job, err)
				continue
			}
			// The destination has to be the link that was planned, with
			// the same target. Anything else would mean the source was
			// removed in exchange for something that never arrived.
			landed, err := os.Lstat(job.dst)
			if err != nil {
				fail(job, fmt.Errorf("%s did not arrive", job.dst))
				continue
			}
			if landed.Mode()&os.ModeSymlink == 0 {
				fail(job, fmt.Errorf("%s did not arrive as a link", job.dst))
				continue
			}
			landedTarget, err := os.Readlink(job.dst)
			if err != nil || landedTarget != job.link {
				fail(job, fmt.Errorf("%s did not arrive with the right target", job.dst))
			}
			continue
		}
		src, err := os.Lstat(job.src)
		if err != nil {
			fail(job, fmt.Errorf("%s could not be checked", job.src))
			continue
		}
		if src.Size() != job.size {
			fail(job, fmt.Errorf("%s changed while it was being copied", job.src))
			continue
		}
		if job.modTime != 0 && src.ModTime().UnixNano() != job.modTime {
			fail(job, fmt.Errorf("%s changed while it was being copied", job.src))
			continue
		}
		dst, err := os.Stat(job.dst)
		if err != nil {
			fail(job, fmt.Errorf("%s did not arrive", job.dst))
			continue
		}
		if dst.Size() != job.size {
			fail(job, fmt.Errorf("%s arrived short", job.dst))
		}
	}
	if len(problems) > 0 {
		// Nothing is removed: the sources stay, so the move can be tried
		// again once the cause is fixed.
		return fmt.Errorf("move: %s", strings.Join(problems, ", "))
	}

	// Everything arrived, so the sources can go. A source that refuses to
	// be removed is reported, but the rest still go: the copies are all
	// there, so nothing is lost by finishing the move.
	//
	// A symlink is checked once more immediately before it is removed. The
	// source a move deletes must still be the link that was planned; a link
	// that was repointed since planning is not what arrived, and removing it
	// would throw away whatever it now names.
	for _, job := range jobs {
		if job.isLink {
			if err := verifyLinkSource(job); err != nil {
				fail(job, err)
				continue
			}
		}
		if err := os.Remove(job.src); err != nil && !os.IsNotExist(err) {
			fail(job, fmt.Errorf("%s could not be removed", job.src))
			continue
		}
		// The entry really moved: its copy landed and its source is gone.
		c.movedJobs.Add(1)
	}
	c.settledJobs = len(c.jobs)
	c.removeEmptySourceDirs()

	if len(problems) > 0 {
		return fmt.Errorf("move: %s", strings.Join(problems, ", "))
	}
	return nil
}

// verifyLinkSource checks that a move's symlink source is still the link that
// was planned: still a symlink, and still storing the same target.
//
// A link is a name, not content, so there is no size to compare. The target
// is the only thing that says whether the link the move is about to remove is
// the one whose copy landed.
func verifyLinkSource(job copyJob) error {
	info, err := os.Lstat(job.src)
	if err != nil {
		return fmt.Errorf("%s could not be checked", job.src)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s changed while it was being copied", job.src)
	}
	target, err := os.Readlink(job.src)
	if err != nil {
		return fmt.Errorf("%s could not be checked", job.src)
	}
	if target != job.link {
		return fmt.Errorf("%s changed while it was being copied", job.src)
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

// stop unwinds a cancelled or failed copy: the open files are closed, the
// scratch entry is removed and the directories the copy made are taken away.
// It runs on the goroutine that owns the copy, so it can touch the copy's own
// state.
//
// It is idempotent. Abort, Step and Close can all arrive, and only the first
// one does the work, so the teardown never runs twice.
//
// The destination is never touched. A copy that was cancelled or that failed
// leaves whatever was already there in place.
func (c *Copier) stop() {
	if !c.cleaned.CompareAndSwap(false, true) {
		return
	}
	partial := c.dstPath
	c.closeCurrent()
	c.dirs = nil
	c.index = len(c.jobs)
	// Nothing that was still pending will ever run now, so the work
	// counters are cleared. A UI reading Pending after a cancel must see a
	// finished transfer, not work that is already over.
	c.copyPending.Store(0)
	c.renamePending.Store(0)
	c.publish()
	if partial != "" {
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

// mkdirAllTracked creates dir and every missing parent, recording the
// directories the copy had to make so a cancelled copy can take them away
// again. A directory that was already there is not recorded: it is the
// user's, and it stays.
//
// Every directory it made is given mode explicitly. os.MkdirAll only passes
// the mode to the create, where the process umask masks it, so a directory
// created under a umask of 022 would not arrive as the 0777 it was asked for.
// The caller passes a mode its owner can write to while the copy fills the
// directory; the exact mode is applied once its contents are in place.
//
// It reports whether dir itself was created, so the caller knows whether the
// directory's source metadata is its to apply.
func mkdirAllTracked(dir string, mode os.FileMode, created *[]string) (bool, error) {
	var missing []string
	for p := filepath.Clean(dir); ; p = filepath.Dir(p) {
		if _, err := os.Lstat(p); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return false, err
		}
		missing = append(missing, p)
		if parent := filepath.Dir(p); parent == p {
			break
		}
	}
	if err := os.MkdirAll(dir, mode); err != nil {
		return false, err
	}
	// Shallowest first, so removeCreatedDirs, which walks backwards, takes
	// the deepest directory away first.
	madeDir := false
	for i := len(missing) - 1; i >= 0; i-- {
		*created = append(*created, missing[i])
		if filepath.Clean(missing[i]) == filepath.Clean(dir) {
			madeDir = true
		}
		// The umask has already been applied to the create. Set the mode
		// explicitly so the copy's directory keeps the bits it was given.
		if err := setCopyMode(missing[i], mode); err != nil {
			return false, err
		}
	}
	return madeDir, nil
}

// acquireStep takes ownership of the fields Step mutates. A cancel holds it
// only for the moment it takes to unwind, so this never waits for another
// step: there is one Step goroutine.
func (c *Copier) acquireStep() {
	for !c.stepping.CompareAndSwap(false, true) {
		runtime.Gosched()
	}
}

// stopIfAborted unwinds the copy when a cancel landed while a step was
// running. It is the step's half of the Abort contract: when Abort could not
// take the step flag, this does the teardown instead. The cleaned flag in
// stop makes sure only one of them ever does.
func (c *Copier) stopIfAborted() {
	if !c.aborted.Load() {
		return
	}
	if !c.stepping.CompareAndSwap(false, true) {
		return
	}
	defer c.stepping.Store(false)
	c.stop()
}

// Abort stops the copy and unwinds it: the partially written file and the
// directories the copy made for itself are removed, and the open source and
// destination files are closed. Files already finished are left in place,
// since they are complete.
//
// It is safe to call while a step is in flight on another goroutine. The stop
// is signalled with a flag. If no step is running — the usual case, since the
// application stops stepping before it cancels — Abort performs the whole
// teardown itself, so the destination is left exactly as it was without
// waiting for a Step that will never come. If a step is in flight, that step
// performs the teardown when it observes the flag. Exactly one of them does
// it.
//
// Because the teardown may be left to a step that is still in flight, call
// Close when the caller needs the cleanup to have happened by the time it
// returns.
func (c *Copier) Abort() error {
	c.aborted.Store(true)
	if c.stepping.CompareAndSwap(false, true) {
		defer c.stepping.Store(false)
		c.stop()
	}
	return nil
}

// Close stops the copy if it is still running and guarantees that the copy has
// been unwound: when Close returns, the partial file is gone, the directories
// the copy created are removed, and the source and destination handles are
// closed. A copy that already finished is left exactly as it is.
//
// Close is idempotent and safe to call from any goroutine. Unlike Abort it may
// wait for a step that is in flight, which is what makes the guarantee
// complete when it returns.
func (c *Copier) Close() error {
	c.aborted.Store(true)
	for !c.stepping.CompareAndSwap(false, true) {
		runtime.Gosched()
	}
	defer c.stepping.Store(false)
	c.stop()
	return nil
}

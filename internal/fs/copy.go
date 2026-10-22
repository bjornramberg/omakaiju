package fs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	// fileDone and fileSize describe progress within that file.
	fileDone int64
	fileSize int64
	// copied is the total number of bytes written so far.
	copied int64
	// filesDone counts fully written files.
	filesDone int
	// dstPath is the partially written destination file, so an abort can
	// remove it.
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

	// --- everything below is owned by the goroutine that calls Step ---

	jobs  []copyJob
	index int

	src     *os.File
	dst     *os.File
	dstPath string

	fileDone int64
	fileSize int64
	copied   int64
	total    int64

	filesDone int
}

// copyJob is one file to copy, with its size known ahead of time.
type copyJob struct {
	src  string
	dst  string
	size int64
	// link is the target of a symlink to recreate. A job with isLink set
	// creates a symlink instead of copying bytes.
	link string
	// isLink marks a job that recreates a symlink rather than copying the
	// file it points at.
	isLink bool
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
	c.publish()
	return c, nil
}

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
	// it here where the destination is known.
	if filepath.Clean(filepath.Join(dstDir, filepath.Base(src))) == filepath.Clean(src) {
		return fmt.Errorf("copy: %s onto itself", src)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		c.jobs = append(c.jobs, copyJob{
			src:    src,
			dst:    filepath.Join(dstDir, filepath.Base(src)),
			link:   target,
			isLink: true,
		})
		return nil
	}
	if !info.IsDir() {
		c.jobs = append(c.jobs, copyJob{
			src:  src,
			dst:  filepath.Join(dstDir, filepath.Base(src)),
			size: info.Size(),
		})
		return nil
	}

	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		job := copyJob{
			src:  path,
			dst:  filepath.Join(dstDir, rel),
			size: fi.Size(),
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
func (c *Copier) FilesTotal() int { return len(c.jobs) }

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
func (c *Copier) FilesDone() int { return c.snapshot().filesDone }

// CurrentName is the file being written, for display.
func (c *Copier) CurrentName() string { return c.snapshot().name }

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

// Step copies up to copyChunk bytes. It reports done when every file is
// complete; any error is returned with the copier already closed.
func (c *Copier) Step() (done bool, err error) {
	if c.aborted.Load() {
		c.stop()
		return true, nil
	}

	if c.index >= len(c.jobs) {
		return true, nil
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
				return true, nil
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
		// File finished; close it out and try the next one.
		if err := c.closeCurrent(); err != nil {
			return false, err
		}
		c.filesDone++
		c.index++
		c.publish()
		if c.index >= len(c.jobs) {
			return true, nil
		}
		return false, nil
	default:
		c.stop()
		return false, err
	}
}

// openCurrent opens the next queued file for reading and writing, or creates
// the next queued symlink.
func (c *Copier) openCurrent() error {
	job := c.jobs[c.index]

	if err := os.MkdirAll(filepath.Dir(job.dst), 0o755); err != nil {
		return err
	}

	if job.isLink {
		// os.Symlink fails if the path exists, and the destination may be
		// left over from an earlier run.
		if err := os.Remove(job.dst); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Symlink(job.link, job.dst); err != nil {
			return err
		}
		c.src = nil
		c.dst = nil
		c.dstPath = ""
		c.fileDone = 0
		c.fileSize = 0
		return nil
	}

	src, err := os.Open(job.src)
	if err != nil {
		return err
	}
	dst, err := os.Create(job.dst)
	if err != nil {
		src.Close()
		return err
	}

	c.src = src
	c.dst = dst
	c.dstPath = job.dst
	c.fileDone = 0
	c.fileSize = job.size
	return nil
}

// closeCurrent finalises the current destination file.
func (c *Copier) closeCurrent() error {
	if c.src != nil {
		if err := c.src.Close(); err != nil {
			c.src = nil
			c.dst.Close()
			c.dst = nil
			return err
		}
		c.src = nil
	}
	if c.dst != nil {
		err := c.dst.Close()
		c.dst = nil
		if err != nil {
			return err
		}
	}
	c.dstPath = ""
	return nil
}

// stop unwinds a cancelled or failed copy: the open files are closed and the
// partially written destination file is removed. It runs on the goroutine
// that owns the copy, so it can touch the copy's own state.
func (c *Copier) stop() {
	partial := c.dstPath
	c.closeCurrent()
	c.dstPath = ""
	c.index = len(c.jobs)
	c.publish()
	if partial != "" {
		// The abort usually removed it already; this covers the case where
		// the copy was stopped by an error instead.
		_ = os.Remove(partial)
	}
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

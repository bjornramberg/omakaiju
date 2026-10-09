package fs

import (
	"io"
	"os"
	"path/filepath"
)

// copyChunk is how many bytes a single Step moves. Bounds how far a copy can
// overshoot a cancel request.
const copyChunk = 256 << 10

// Copier copies a set of sources into a destination directory as a resumable
// state machine: the caller drives it with Step so all mutation happens on one
// goroutine. That keeps the UI loop free of blocking reads, channels, and locks,
// and makes the whole job deterministic and testable.
type Copier struct {
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
}

// NewCopier scans srcs and prepares to copy them into dstDir. The total size is
// computed up front by stat alone, so the progress bar always has a denominator
// without reading any file data.
func NewCopier(srcs []string, dstDir string) (*Copier, error) {
	c := &Copier{}
	for _, src := range srcs {
		if err := c.add(src, dstDir); err != nil {
			return nil, err
		}
	}
	c.total = c.scheduledBytes()
	return c, nil
}

// add queues src and, for directories, every file beneath it.
func (c *Copier) add(src, dstDir string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
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
		c.jobs = append(c.jobs, copyJob{
			src:  path,
			dst:  filepath.Join(dstDir, rel),
			size: fi.Size(),
		})
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

// Total is the byte count of everything queued.
func (c *Copier) Total() int64 { return c.total }

// Copied is the byte count finished so far.
func (c *Copier) Copied() int64 { return c.copied }

// FilesDone counts fully written files.
func (c *Copier) FilesDone() int { return c.filesDone }

// FilesTotal counts queued files.
func (c *Copier) FilesTotal() int { return len(c.jobs) }

// CurrentName is the file being written, for display.
func (c *Copier) CurrentName() string {
	if c.index < len(c.jobs) {
		return filepath.Base(c.jobs[c.index].src)
	}
	return ""
}

// FileCopied and FileSize describe progress within the current file.
func (c *Copier) FileCopied() int64 { return c.fileDone }
func (c *Copier) FileSize() int64   { return c.fileSize }

// Percent is overall completion across every queued file. Preferring the
// aggregate keeps the bar monotonic across file boundaries; the per-file
// position is reported separately by FileCopied and FileSize.
func (c *Copier) Percent() int {
	if c.total <= 0 {
		return 100
	}
	p := int(c.copied * 100 / c.total)
	if p > 100 {
		return 100
	}
	return p
}

// Step copies up to copyChunk bytes. It reports done when every file is
// complete; any error is returned with the copier already closed.
func (c *Copier) Step() (done bool, err error) {
	if c.index >= len(c.jobs) {
		return true, nil
	}

	if c.dst == nil {
		if err := c.openCurrent(); err != nil {
			return false, err
		}
	}

	// io.CopyN writes at most n bytes and returns io.EOF when the source is
	// shorter, which is how we detect the end of this file.
	n, err := io.CopyN(c.dst, c.src, copyChunk)
	c.fileDone += n
	c.copied += n

	switch {
	case err == nil:
		// More remains in this file.
		return false, nil
	case err == io.EOF:
		// File finished; close it out and try the next one.
		if err := c.closeCurrent(); err != nil {
			return false, err
		}
		c.filesDone++
		c.index++
		if c.index >= len(c.jobs) {
			return true, nil
		}
		return false, nil
	default:
		c.Abort()
		return false, err
	}
}

// openCurrent opens the next queued file for reading and writing.
func (c *Copier) openCurrent() error {
	job := c.jobs[c.index]

	if err := os.MkdirAll(filepath.Dir(job.dst), 0o755); err != nil {
		return err
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

// Abort stops the copy and removes the partially written destination file.
// Files already finished are left in place, since they are complete.
func (c *Copier) Abort() error {
	partial := c.dstPath

	if c.src != nil {
		c.src.Close()
		c.src = nil
	}
	if c.dst != nil {
		c.dst.Close()
		c.dst = nil
	}
	c.dstPath = ""
	c.index = len(c.jobs)

	if partial != "" {
		return os.Remove(partial)
	}
	return nil
}

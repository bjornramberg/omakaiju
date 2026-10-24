package fs

import (
	"fmt"
	"os"
	"path/filepath"
)

// MoveFailure is one entry a move could not move, with the reason.
//
// The name is the entry's own name, so a failure can be reported without
// repeating the path the user is already looking at. The path is the
// entry's source, so a failed move can be offered for another try.
type MoveFailure struct {
	Name string
	Path string
	Err  error
}

// NewMover prepares to move srcs into dstDir.
//
// A move within one filesystem is a rename: it costs nothing and it is
// atomic, so those entries are moved as the Mover is built. A move onto
// another filesystem — an SMB share, say — cannot be a rename, so those
// entries are copied across instead, and the original is removed only once
// the copy has landed and been checked.
//
// The copying is driven a step at a time, like a copy, so the caller can
// watch the progress and cancel the work. A move that is cancelled, or that
// fails, leaves the source exactly where it was.
func NewMover(srcs []string, dstDir string) (*Copier, error) {
	c := &Copier{move: true}

	for _, src := range srcs {
		dst := filepath.Join(dstDir, filepath.Base(src))
		name := filepath.Base(src)

		// A source and a destination that are two names for one file would
		// otherwise be a silent no-op that looks like a successful move.
		if same, err := SameFile(src, dst); err == nil && same {
			c.failures = append(c.failures, MoveFailure{
				Name: name,
				Path: src,
				Err:  fmt.Errorf("move: %s and %s are the same file", src, dst),
			})
			continue
		}

		err := os.Rename(src, dst)
		switch {
		case err == nil:
			// The rename settled it. Nothing left to copy.
			c.renamed++
		case isCrossDevice(err):
			// The entry lives on another filesystem, so it has to be
			// copied across. The original goes when the copy lands.
			before := len(c.jobs)
			dirsBefore := len(c.dirs)
			if err := c.add(src, dstDir); err != nil {
				// The walk failed part way through, so the entry is not
				// fully queued. The half of it that was queued is dropped:
				// copying part of a tree is not the move the user asked
				// for, and it would leave the sources in a state where
				// some of them had gone and some had not.
				c.jobs = c.jobs[:before]
				c.dirs = c.dirs[:dirsBefore]
				c.failures = append(c.failures, MoveFailure{Name: name, Path: src, Err: err})
			}
		default:
			c.failures = append(c.failures, MoveFailure{Name: name, Path: src, Err: err})
		}
	}

	c.total = c.scheduledBytes()
	c.pending = len(c.jobs) + len(c.dirs)
	c.publish()
	return c, nil
}

// Renamed counts the entries a rename settled. They cost nothing and they
// are finished before the first step.
func (c *Copier) Renamed() int { return c.renamed }

// Failures reports the entries that could not be moved at all.
func (c *Copier) Failures() []MoveFailure { return c.failures }

// Moved counts the entries that have reached the destination, whether by a
// rename or by a copy.
//
// The entries a rename settled are counted straight away: they are already
// moved. The entries a copy carried are counted only once every file has
// landed and the sources have gone, because until then their sources are
// still where they were and nothing has really moved.
func (c *Copier) Moved() int {
	moved := c.renamed
	if c.move && c.settleOK {
		moved += c.snapshot().filesDone
	}
	return moved
}

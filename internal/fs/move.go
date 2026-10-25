package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

// renameJob is an entry a move settles with a rename.
//
// The dstDir is kept as well, so an entry that turns out to cross devices at
// execution time can be queued for the copy path without recomputing where
// it was meant to go.
type renameJob struct {
	src    string
	dst    string
	dstDir string
	name   string
}

// NewMover prepares to move srcs into dstDir.
//
// Planning performs no filesystem mutation. Each entry is classified as a
// same-filesystem rename or a cross-device copy, but the renames themselves
// run during the move's settle phase: after every copy has landed and been
// verified. A copy that fails therefore leaves the cheap entries where they
// were, and a batch can never be half-renamed before the transfer has even
// been tried.
//
// Two entries that would land on the same destination are a collision: the
// whole batch is refused and every source is reported, because moving either
// one first would destroy the other. The check is by destination path, so a
// file and a directory with the same basename collide just as two files do.
func NewMover(srcs []string, dstDir string) (*Copier, error) {
	c := &Copier{move: true, dstDir: dstDir}
	if info, err := os.Stat(dstDir); err == nil && info.IsDir() {
		c.dstDirExisted = true
	}
	if len(srcs) == 0 {
		c.publish()
		return c, nil
	}

	type plan struct {
		src    string
		dst    string
		name   string
		dstKey string
	}

	plans := make([]plan, 0, len(srcs))
	byDestination := make(map[string][]int, len(srcs))
	for _, src := range srcs {
		name := filepath.Base(src)
		dst := filepath.Join(dstDir, name)
		key := cleanAbsolute(dst)
		plans = append(plans, plan{src: src, dst: dst, name: name, dstKey: key})
		byDestination[key] = append(byDestination[key], len(plans)-1)
	}

	var collisions []string
	for key, indexes := range byDestination {
		if len(indexes) > 1 {
			names := make([]string, 0, len(indexes))
			for _, i := range indexes {
				names = append(names, plans[i].src)
			}
			collisions = append(collisions, fmt.Sprintf("%s would all be moved to %s", strings.Join(names, " and "), key))
		}
	}
	if len(collisions) > 0 {
		// Queue nothing at all. Moving any one of the colliding entries
		// would put it where another was meant to go, so no entry is a
		// safe partial result.
		reason := fmt.Sprintf("move: the batch was refused: %s", strings.Join(collisions, "; "))
		for _, p := range plans {
			indexes := byDestination[p.dstKey]
			err := fmt.Errorf("%s", reason)
			if len(indexes) > 1 {
				names := make([]string, 0, len(indexes))
				for _, i := range indexes {
					names = append(names, plans[i].src)
				}
				err = fmt.Errorf("move: %s would all be moved to %s", strings.Join(names, " and "), p.dstKey)
			}
			c.failures = append(c.failures, MoveFailure{Name: p.name, Path: p.src, Err: err})
		}
		c.publish()
		return c, nil
	}

	for _, p := range plans {
		info, err := os.Lstat(p.src)
		if err != nil {
			c.failures = append(c.failures, MoveFailure{Name: p.name, Path: p.src, Err: err})
			continue
		}

		// A source and a destination that are two names for one file would
		// otherwise be a silent no-op that looks like a successful move.
		if same, err := SameFile(p.src, p.dst); err == nil && same {
			c.failures = append(c.failures, MoveFailure{
				Name: p.name,
				Path: p.src,
				Err:  fmt.Errorf("move: %s and %s are the same file", p.src, p.dst),
			})
			continue
		}

		if sameFilesystem(info, dstDir) {
			c.renames = append(c.renames, renameJob{
				src:    p.src,
				dst:    p.dst,
				dstDir: dstDir,
				name:   p.name,
			})
			continue
		}

		// A move never replaces a destination that is already there. The
		// same-filesystem path checks this immediately before its rename;
		// the copy path has to check it here, before anything is queued, so
		// the entry is refused as a whole and its source stays put. Lstat
		// is used so a symlink counts as existing and is not followed.
		if err := refuseExistingDestination(p.dst); err != nil {
			c.failures = append(c.failures, MoveFailure{Name: p.name, Path: p.src, Err: err})
			continue
		}

		// The entry lives on another filesystem, so it has to be copied
		// across. The original goes when the copy lands.
		before := len(c.jobs)
		dirsBefore := len(c.dirs)
		srcDirsBefore := len(c.srcDirs)
		if err := c.add(p.src, dstDir); err != nil {
			// The walk failed part way through, so the entry is not
			// fully queued. The half of it that was queued is dropped:
			// copying part of a tree is not the move the user asked
			// for, and it would leave the sources in a state where
			// some of them had gone and some had not.
			c.jobs = c.jobs[:before]
			c.dirs = c.dirs[:dirsBefore]
			c.srcDirs = c.srcDirs[:srcDirsBefore]
			c.failures = append(c.failures, MoveFailure{Name: p.name, Path: p.src, Err: err})
		}
	}

	c.total.Store(c.scheduledBytes())
	c.filesTotal.Store(int64(len(c.jobs) + len(c.renames)))
	c.copyPending.Store(int64(len(c.jobs) + len(c.dirs)))
	c.renamePending.Store(int64(len(c.renames)))
	c.publish()
	return c, nil
}

// Renamed counts the entries a rename settled. A rename only runs once the
// copy work has landed, so this is zero until the settle phase.
func (c *Copier) Renamed() int { return int(c.renamed.Load()) }

// Failures reports the entries that could not be moved at all.
func (c *Copier) Failures() []MoveFailure { return c.failures }

// Moved counts the entries that have reached the destination, whether by a
// rename or by a copy.
//
// The entries a rename settled are counted as they run. The entries a copy
// carried are counted only once their source has really been removed: a copy
// whose source could not be deleted is not a moved entry, whatever landed at
// the destination. The counts are atomic because the UI reads them.
func (c *Copier) Moved() int {
	moved := int(c.renamed.Load())
	if c.move {
		moved += int(c.movedJobs.Load())
	}
	return moved
}

// recordStepFailure notes the entry a move's copy failed on, so the failure is
// visible through Failures() as well as through the error Step returned.
func (c *Copier) recordStepFailure(err error) {
	if !c.move || c.index >= len(c.jobs) {
		return
	}
	job := c.jobs[c.index]
	c.failures = append(c.failures, MoveFailure{
		Name: filepath.Base(job.src),
		Path: job.src,
		Err:  err,
	})
}

// runRenames settles the queued renames. It reports whether it queued new
// copy work for a rename that turned out to cross devices.
//
// A destination that is already there is refused rather than replaced: the
// check is repeated immediately before each rename because planning may be
// stale. A refused rename is a per-entry failure, its source stays where it
// was, and the rest of the batch continues.
func (c *Copier) runRenames() (moreCopyWork bool) {
	for len(c.renames) > 0 {
		job := c.renames[0]
		c.renames = c.renames[1:]
		c.renamePending.Add(-1)

		if _, err := os.Lstat(job.dst); err == nil {
			c.failures = append(c.failures, MoveFailure{
				Name: job.name,
				Path: job.src,
				Err:  fmt.Errorf("move: %s already exists", job.dst),
			})
			continue
		} else if !os.IsNotExist(err) {
			c.failures = append(c.failures, MoveFailure{
				Name: job.name,
				Path: job.src,
				Err:  fmt.Errorf("move: cannot check %s: %w", job.dst, err),
			})
			continue
		}

		err := os.Rename(job.src, job.dst)
		if err == nil {
			c.renamed.Add(1)
			continue
		}

		if !isCrossDevice(err) {
			c.failures = append(c.failures, MoveFailure{Name: job.name, Path: job.src, Err: err})
			continue
		}

		// The classification was only a hint. The entry has to be
		// copied across after all, and no further rename may run until
		// that copy has landed and been verified.
		before := len(c.jobs)
		dirsBefore := len(c.dirs)
		srcDirsBefore := len(c.srcDirs)
		if addErr := c.add(job.src, job.dstDir); addErr != nil {
			c.jobs = c.jobs[:before]
			c.dirs = c.dirs[:dirsBefore]
			c.srcDirs = c.srcDirs[:srcDirsBefore]
			c.failures = append(c.failures, MoveFailure{Name: job.name, Path: job.src, Err: addErr})
			continue
		}
		queued := (len(c.jobs) - before) + (len(c.dirs) - dirsBefore)
		if queued > 0 {
			// The bytes of the fallback copy are part of the move now.
			// The rename entry that was counted once is replaced by the
			// jobs the fallback queued, so the total moves by the
			// difference.
			c.total.Store(c.scheduledBytes())
			c.filesTotal.Add(int64(queued - 1))
			c.copyPending.Add(int64(queued))
			return true
		}
	}
	return false
}

// cleanAbsolute makes a path comparable without requiring it to exist.
func cleanAbsolute(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

// sameFilesystem reports whether a rename between src and dstDir is likely to
// stay on one filesystem.
//
// It is only a hint: the device numbers are read at planning time and a mount
// can change under the move. Every rename still keeps its cross-device
// fallback at execution time. The destination directory is stat'ed rather
// than lstat'ed, so a symlinked destination is followed, which is where the
// files will really land.
func sameFilesystem(srcInfo os.FileInfo, dstDir string) bool {
	dstInfo, err := os.Stat(dstDir)
	if err != nil {
		// Unknown: let the copy path, which can create the destination,
		// try rather than committing to a rename that may fail.
		return false
	}
	srcDev, ok := deviceID(srcInfo)
	if !ok {
		return false
	}
	dstDev, ok := deviceID(dstInfo)
	if !ok {
		return false
	}
	return srcDev == dstDev
}

// deviceID returns the device number a file lives on.
func deviceID(info os.FileInfo) (uint64, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true
}

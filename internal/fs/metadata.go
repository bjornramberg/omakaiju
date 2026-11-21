package fs

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// MetaWarning reports metadata a copy could not carry.
//
// The bytes always land exactly; what is lost is the mode or the times.
// Some filesystems — FAT, exFAT, many SMB shares — simply refuse chmod or
// utimes, and failing the whole transfer over that would make copying to
// them impossible. The warning names the destination and what was dropped,
// so the result can say the file arrived without pretending it is exact.
type MetaWarning struct {
	// Path is the destination entry whose metadata was dropped.
	Path string
	// What names what was dropped: "mode" or "times".
	What string
	// Err is why the filesystem refused it.
	Err error
}

// isMetaUnsupported reports whether err means the filesystem cannot take
// metadata at all, as opposed to a real failure.
//
// EPERM is what a share answers when it does not implement chmod; EOPNOTSUPP
// and ENOSYS are the same refusal in other words. Anything else — a vanished
// path, I/O trouble — still fails the copy, because the destination may not
// be what was asked for.
func isMetaUnsupported(err error) bool {
	return errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.EOPNOTSUPP) ||
		errors.Is(err, syscall.ENOSYS)
}

// firstCollector returns the first warnings collector a caller passed, or
// nil when none was given. The exported copy functions take their collector
// as a variadic argument so every existing call site keeps compiling with
// the strict contract, while new code opts into graceful metadata
// degradation by passing one.
func firstCollector(warn []*[]MetaWarning) *[]MetaWarning {
	if len(warn) > 0 {
		return warn[0]
	}
	return nil
}

// dropMeta records metadata the filesystem refused as a warning and reports
// whether the caller should carry on without it.
//
// warn collects the warnings; a nil collector means the caller keeps the
// strict contract and every refusal fails. That is what the existing callers
// — and their tests — rely on: without a collector nothing changes. A
// refusal that is not an unsupported-filesystem error never degrades either,
// so a vanished path or I/O trouble still stops the copy with the destination
// untouched.
func dropMeta(warn *[]MetaWarning, path, what string, err error) bool {
	if warn == nil || !isMetaUnsupported(err) {
		return false
	}
	*warn = append(*warn, MetaWarning{Path: path, What: what, Err: err})
	return true
}

// copyTimes are the times a copy carries over from its source.
//
// The modification time is what many tools use to decide whether a file has
// changed, and the access time says when it was last looked at. Both are set
// on the scratch entry before it is renamed into place, so a copy that cannot
// carry them fails with the destination untouched, exactly as every other
// part of the copy does.
type copyTimes struct {
	atime time.Time
	mtime time.Time
}

// timesOf returns the access and modification times recorded in info.
//
// os.FileInfo only exposes the modification time. The access time is read from
// the underlying stat, which is where the filesystem keeps it. A FileInfo that
// is not backed by one falls back to the modification time, so a copy still
// carries the time it can see.
func timesOf(info os.FileInfo) copyTimes {
	mtime := info.ModTime()
	times := copyTimes{atime: mtime, mtime: mtime}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		sec, nsec := st.Atim.Unix()
		times.atime = time.Unix(sec, nsec)
	}
	return times
}

// setCopyTimes applies a source's times to path.
//
// The times go on the scratch entry before the final rename, which is what
// makes them safe to carry: a failure leaves the destination exactly as it
// was. A copy that silently dropped the source's time would look newer than
// the file it came from.
func setCopyTimes(path string, times copyTimes) error {
	return chtimesFn(path, times.atime, times.mtime)
}

// chtimesFn applies access and modification times.
//
// It is a variable so a test can make the times change fail deterministically
// on a filesystem that would otherwise accept it, mirroring chmodFn.
var chtimesFn = os.Chtimes

// chmodFn applies the mode of a copy's entries.
//
// It is a variable so a test can make the mode change fail deterministically
// after the scratch file is whole. A local filesystem will not refuse a chmod
// on a file the process owns, and the failure path is exactly the one that has
// to leave the destination alone.
var chmodFn = os.Chmod

// setCopyMode applies a source entry's exact mode to path.
//
// The whole mode is passed, not just the low permission bits: os.Chmod carries
// the setuid, setgid and sticky bits on Unix, and those are part of how a file
// behaves. A copied helper that loses its setuid bit, or a copied directory
// that loses its sticky bit, is not the entry the user asked for.
//
// Ownership and ACLs are not carried: they cannot be set without privileges.
// Extended attributes and sparseness are not carried either: they are outside
// what a stream copy does, and a sparse file arrives with all its blocks.
func setCopyMode(path string, mode os.FileMode) error {
	return chmodFn(path, mode)
}

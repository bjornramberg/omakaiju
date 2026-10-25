package fs

import (
	"os"
	"syscall"
	"time"
)

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
	return os.Chtimes(path, times.atime, times.mtime)
}

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

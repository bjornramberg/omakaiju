package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// drainMover runs a mover to completion.
func drainMover(t *testing.T, c *Copier) {
	t.Helper()
	for i := 0; i < 10000; i++ {
		done, err := c.Step()
		if err != nil {
			t.Fatalf("move step failed: %v", err)
		}
		if done {
			return
		}
	}
	t.Fatal("mover did not finish")
}

// A move that stops half way leaves every source where it was. The sources
// go together or not at all, so a file that could not be copied does not
// take the rest of the move down with it.
func TestMoveThatFailsKeepsEverySource(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	base := t.TempDir()
	other := crossDeviceDir(t, base)

	good := filepath.Join(base, "good.bin")
	if err := os.WriteFile(good, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(base, "bad.bin")
	if err := os.WriteFile(bad, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	// The second file cannot be read, so the copy of it fails.
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(bad, 0o644)

	c, err := NewMover([]string{good, bad}, other)
	if err != nil {
		t.Fatal(err)
	}
	// The move fails when it reaches the file it cannot read.
	var moveErr error
	for i := 0; i < 100 && moveErr == nil; i++ {
		done, err := c.Step()
		if err != nil {
			moveErr = err
			break
		}
		if done {
			break
		}
	}
	if moveErr == nil {
		t.Fatal("a move of an unreadable file should fail")
	}

	// The file that could be copied is still at its source: the sources of
	// a move go together.
	if _, err := os.Stat(good); err != nil {
		t.Errorf("the source of a file that copied cleanly should still be there: %v", err)
	}
}

// A source that changed while it was on its way is not the file that
// arrived, so it is left where it is rather than removed.
func TestMoveLeavesASourceThatChangedAlone(t *testing.T) {
	base := t.TempDir()
	other := crossDeviceDir(t, base)

	src := filepath.Join(base, "movie.mkv")
	if err := os.WriteFile(src, make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := NewMover([]string{src}, other)
	if err != nil {
		t.Fatal(err)
	}

	// The source is changed behind the move's back.
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(src, future, future); err != nil {
		t.Fatal(err)
	}

	var moveErr error
	for i := 0; i < 100 && moveErr == nil; i++ {
		done, err := c.Step()
		if err != nil {
			moveErr = err
			break
		}
		if done {
			break
		}
	}
	if moveErr == nil {
		t.Fatal("a move of a changed source should report that it did not finish")
	}
	if !strings.Contains(moveErr.Error(), "changed") {
		t.Errorf("error = %q, want it to say the source changed", moveErr)
	}

	if _, err := os.Stat(src); err != nil {
		t.Errorf("a source that changed should be left alone: %v", err)
	}
}

// An empty folder is still a folder. It has no files to carry, but it has
// to arrive at the destination all the same.
func TestMoveOfAnEmptyDirectoryAcrossDevices(t *testing.T) {
	base := t.TempDir()
	other := crossDeviceDir(t, base)

	src := filepath.Join(base, "empty")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	c, err := NewMover([]string{src}, other)
	if err != nil {
		t.Fatal(err)
	}
	if c.Pending() == 0 {
		t.Error("a mover with a directory to create is not finished")
	}
	drainMover(t, c)

	landed := filepath.Join(other, "empty")
	info, err := os.Stat(landed)
	if err != nil {
		t.Fatalf("the empty folder did not arrive: %v", err)
	}
	if !info.IsDir() {
		t.Error("the empty folder arrived as a file")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("the source folder should be gone: %v", err)
	}
}

// A move within one filesystem is a rename: the file lands whole and the
// original is gone.
func TestMoverRenamesWithinOneFilesystem(t *testing.T) {
	base := t.TempDir()
	dst := t.TempDir()

	src := filepath.Join(base, "a.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := NewMover([]string{src}, dst)
	if err != nil {
		t.Fatal(err)
	}
	drainMover(t, c)

	if got := readFile(t, filepath.Join(dst, "a.txt")); got != "payload" {
		t.Errorf("a.txt = %q, want %q", got, "payload")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("the source should be gone: %v", err)
	}
	if c.Renamed() != 1 {
		t.Errorf("Renamed() = %d, want 1", c.Renamed())
	}
	if len(c.Failures()) != 0 {
		t.Errorf("Failures() = %v, want none", c.Failures())
	}
}

// A move onto another filesystem — a share, say — cannot be a rename, so the
// file is copied across and the original is removed once the copy has landed.
func TestMoverCopiesAcrossDevicesAndRemovesTheSource(t *testing.T) {
	base := t.TempDir()
	other := crossDeviceDir(t, base)

	src := filepath.Join(base, "movie.mkv")
	const payload = "a long movie that has to travel to the share"
	if err := os.WriteFile(src, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := NewMover([]string{src}, other)
	if err != nil {
		t.Fatal(err)
	}
	drainMover(t, c)

	if got := readFile(t, filepath.Join(other, "movie.mkv")); got != payload {
		t.Errorf("the moved file is wrong: got %q, want %q", got, payload)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("the source should be gone after a successful move: %v", err)
	}
	if c.Renamed() != 0 {
		t.Errorf("Renamed() = %d, want 0 for a cross-device move", c.Renamed())
	}
	// Nothing of the move's own may be left in the destination.
	assertNoScratchFiles(t, other)
}

// A directory moved across devices arrives whole, and the original is only
// removed once every file in it has.
func TestMoverCopiesDirectoryAcrossDevices(t *testing.T) {
	base := t.TempDir()
	other := crossDeviceDir(t, base)

	src := filepath.Join(base, "album")
	if err := os.MkdirAll(filepath.Join(src, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "inner", "b.txt"), []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := NewMover([]string{src}, other)
	if err != nil {
		t.Fatal(err)
	}
	drainMover(t, c)

	landed := filepath.Join(other, "album")
	if got := readFile(t, filepath.Join(landed, "a.txt")); got != "alpha" {
		t.Errorf("a.txt = %q, want %q", got, "alpha")
	}
	if got := readFile(t, filepath.Join(landed, "inner", "b.txt")); got != "beta" {
		t.Errorf("inner/b.txt = %q, want %q", got, "beta")
	}
	// The original tree is gone: its files went as they landed and the
	// directories that held them went after them.
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("the source directory should be gone: %v", err)
	}
}

// A move that cannot finish leaves the source exactly where it was.
func TestMoverAcrossDevicesThatFailsKeepsTheSource(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	base := t.TempDir()
	other := crossDeviceDir(t, base)

	src := filepath.Join(base, "movie.mkv")
	const payload = "a long movie that has to travel to the share"
	if err := os.WriteFile(src, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(other, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(other, 0o755)

	c, err := NewMover([]string{src}, other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Step(); err == nil {
		t.Fatal("a move into a read-only directory should fail")
	}
	if got := readFile(t, src); got != payload {
		t.Errorf("the source was damaged: got %q, want %q", got, payload)
	}
}

// A move onto the file itself is refused, and the refusal is reported rather
// than swallowed.
func TestMoverRefusesMovingAFileOntoItself(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	link := filepath.Join(base, "link")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	src := filepath.Join(real, "photo.jpg")
	const payload = "the only copy of this photo"
	if err := os.WriteFile(src, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := NewMover([]string{src}, link)
	if err != nil {
		t.Fatal(err)
	}
	drainMover(t, c)

	if len(c.Failures()) != 1 {
		t.Fatalf("Failures() = %v, want one failure", c.Failures())
	}
	if c.Failures()[0].Name != "photo.jpg" {
		t.Errorf("the failure names %q, want %q", c.Failures()[0].Name, "photo.jpg")
	}
	if got := readFile(t, src); got != payload {
		t.Errorf("the source was damaged: got %q, want %q", got, payload)
	}
}

// The progress a mover reports covers the bytes it is actually copying, and
// the entries a rename settled are counted as moved.
func TestMoverReportsWhatItMoved(t *testing.T) {
	base := t.TempDir()
	dst := t.TempDir()
	other := crossDeviceDir(t, base)

	// One entry stays on this filesystem and is renamed.
	renamed := filepath.Join(base, "a.txt")
	if err := os.WriteFile(renamed, []byte("small"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The other has to travel, so it is copied.
	copied := filepath.Join(other, "b.bin")
	if err := os.WriteFile(copied, make([]byte, 500), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := NewMover([]string{renamed, copied}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if c.Total() != 500 {
		t.Errorf("Total() = %d, want 500", c.Total())
	}
	drainMover(t, c)

	if c.Moved() != 2 {
		t.Errorf("Moved() = %d, want 2", c.Moved())
	}
	if c.Copied() != 500 {
		t.Errorf("Copied() = %d, want 500", c.Copied())
	}
	if c.Renamed() != 1 {
		t.Errorf("Renamed() = %d, want 1", c.Renamed())
	}
}

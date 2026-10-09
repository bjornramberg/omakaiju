package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func writeBytes(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

// drain runs a copier to completion.
func drain(t *testing.T, c *Copier) {
	t.Helper()
	for i := 0; i < 10000; i++ {
		done, err := c.Step()
		if err != nil {
			t.Fatalf("step failed: %v", err)
		}
		if done {
			return
		}
	}
	t.Fatal("copier did not finish")
}

func TestCopierPrescanTotals(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeBytes(t, filepath.Join(src, "a.bin"), 1000)
	writeBytes(t, filepath.Join(src, "b.bin"), 2000)

	c, err := NewCopier([]string{src}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if c.Total() != 3000 {
		t.Errorf("Total() = %d, want 3000", c.Total())
	}
	if c.Copied() != 0 {
		t.Errorf("Copied() = %d, want 0 before stepping", c.Copied())
	}
	if c.FilesTotal() != 2 {
		t.Errorf("FilesTotal() = %d, want 2", c.FilesTotal())
	}
}

func TestCopierSingleFile(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeBytes(t, filepath.Join(src, "a.bin"), 500)

	c, err := NewCopier([]string{filepath.Join(src, "a.bin")}, dst)
	if err != nil {
		t.Fatal(err)
	}
	drain(t, c)

	if c.Copied() != 500 {
		t.Errorf("Copied() = %d, want 500", c.Copied())
	}
	info, err := os.Stat(filepath.Join(dst, "a.bin"))
	if err != nil {
		t.Fatalf("destination missing: %v", err)
	}
	if info.Size() != 500 {
		t.Errorf("destination size = %d, want 500", info.Size())
	}
}

func TestCopierProgressIsMonotonic(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	// Larger than one chunk so several steps are required.
	size := copyChunk*2 + 1234
	writeBytes(t, filepath.Join(src, "big.bin"), size)

	c, err := NewCopier([]string{filepath.Join(src, "big.bin")}, dst)
	if err != nil {
		t.Fatal(err)
	}

	var last int64
	steps := 0
	for {
		done, err := c.Step()
		if err != nil {
			t.Fatal(err)
		}
		got := c.Copied()
		if got < last {
			t.Fatalf("progress went backwards: %d then %d", last, got)
		}
		if got > c.Total() {
			t.Fatalf("progress %d exceeds total %d", got, c.Total())
		}
		last = got
		steps++
		if done {
			break
		}
		if steps > 100 {
			t.Fatal("too many steps")
		}
	}

	if steps < 3 {
		t.Errorf("steps = %d, expected the copy to span several chunks", steps)
	}
	if last != int64(size) {
		t.Errorf("final copied = %d, want %d", last, size)
	}
}

func TestCopierDirectoryTree(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeBytes(t, filepath.Join(src, "top.bin"), 100)
	writeBytes(t, filepath.Join(src, "sub", "nested.bin"), 300)
	writeBytes(t, filepath.Join(src, "sub", "deeper", "deep.bin"), 50)

	c, err := NewCopier([]string{src}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if c.Total() != 450 {
		t.Errorf("Total() = %d, want 450", c.Total())
	}
	if c.FilesTotal() != 3 {
		t.Errorf("FilesTotal() = %d, want 3", c.FilesTotal())
	}
	drain(t, c)

	for _, rel := range []string{"top.bin", "sub/nested.bin", "sub/deeper/deep.bin"} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err != nil {
			t.Errorf("%s missing: %v", rel, err)
		}
	}
}

func TestCopierMultipleSources(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	a := filepath.Join(src, "a.bin")
	b := filepath.Join(src, "b.bin")
	writeBytes(t, a, 10)
	writeBytes(t, b, 20)

	c, err := NewCopier([]string{a, b}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if c.Total() != 30 {
		t.Errorf("Total() = %d, want 30", c.Total())
	}
	drain(t, c)

	for _, n := range []string{"a.bin", "b.bin"} {
		if _, err := os.Stat(filepath.Join(dst, n)); err != nil {
			t.Errorf("%s missing: %v", n, err)
		}
	}
}

func TestCopierAbortRemovesPartialFile(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	size := copyChunk * 3
	writeBytes(t, filepath.Join(src, "big.bin"), size)

	c, err := NewCopier([]string{filepath.Join(src, "big.bin")}, dst)
	if err != nil {
		t.Fatal(err)
	}

	// Step once so a partial file exists.
	if _, err := c.Step(); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(dst, "big.bin")
	if _, err := os.Stat(partial); err != nil {
		t.Fatalf("partial file should exist mid-copy: %v", err)
	}

	if err := c.Abort(); err != nil {
		t.Fatalf("abort failed: %v", err)
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Error("abort should remove the partial destination file")
	}
}

func TestCopierAbortKeepsCompletedFiles(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeBytes(t, filepath.Join(src, "a.bin"), 100)
	writeBytes(t, filepath.Join(src, "b.bin"), copyChunk*2)

	c, err := NewCopier([]string{src}, dst)
	if err != nil {
		t.Fatal(err)
	}
	// Finish the first file, then stop mid-second.
	for {
		done, err := c.Step()
		if err != nil {
			t.Fatal(err)
		}
		if c.FilesDone() == 1 {
			break
		}
		if done {
			t.Fatal("expected more than one file")
		}
	}
	if err := c.Abort(); err != nil {
		t.Fatalf("abort failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "a.bin")); err != nil {
		t.Errorf("completed file should survive abort: %v", err)
	}
}

func TestCopierStepAfterAbortIsDone(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeBytes(t, filepath.Join(src, "a.bin"), copyChunk*2)

	c, err := NewCopier([]string{filepath.Join(src, "a.bin")}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Step(); err != nil {
		t.Fatal(err)
	}
	if err := c.Abort(); err != nil {
		t.Fatal(err)
	}

	done, err := c.Step()
	if err != nil {
		t.Fatalf("step after abort should not error: %v", err)
	}
	if !done {
		t.Error("step after abort should report done")
	}
}

func TestCopierZeroByteFile(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeBytes(t, filepath.Join(src, "empty.bin"), 0)

	c, err := NewCopier([]string{filepath.Join(src, "empty.bin")}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if c.Percent() != 100 {
		t.Errorf("Percent() for empty file = %d, want 100", c.Percent())
	}
	drain(t, c)
	if _, err := os.Stat(filepath.Join(dst, "empty.bin")); err != nil {
		t.Errorf("empty file should be created: %v", err)
	}
}

func TestCopierMissingSourceErrors(t *testing.T) {
	dst := t.TempDir()
	if _, err := NewCopier([]string{filepath.Join(t.TempDir(), "nope.bin")}, dst); err == nil {
		t.Error("missing source should fail the scan")
	}
}

func TestCopierCurrentNameTracksFile(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeBytes(t, filepath.Join(src, "alpha.bin"), 10)
	writeBytes(t, filepath.Join(src, "beta.bin"), copyChunk*2)

	c, err := NewCopier([]string{src}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if c.CurrentName() == "" {
		t.Error("CurrentName should be set before the first step")
	}
	drain(t, c)
}

// The bar must advance across the whole job, not sit at 100% while files are
// still being written.
func TestCopierPercentIsAggregateAndMonotonic(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	for _, n := range []string{"a.bin", "b.bin", "c.bin"} {
		writeBytes(t, filepath.Join(src, n), 1<<20)
	}

	c, err := NewCopier([]string{src}, dst)
	if err != nil {
		t.Fatal(err)
	}

	if c.Percent() != 0 {
		t.Errorf("Percent() before any work = %d, want 0", c.Percent())
	}

	var last int
	for {
		done, err := c.Step()
		if err != nil {
			t.Fatal(err)
		}
		if p := c.Percent(); p < last {
			t.Fatalf("percent went backwards: %d then %d", last, p)
		} else {
			last = p
		}
		if done {
			break
		}
	}

	if last != 100 {
		t.Errorf("final percent = %d, want 100", last)
	}
}

func TestCopierPercentMidway(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeBytes(t, filepath.Join(src, "half.bin"), 2*copyChunk)

	c, err := NewCopier([]string{filepath.Join(src, "half.bin")}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Step(); err != nil { // one of two chunks
		t.Fatal(err)
	}
	if p := c.Percent(); p != 50 {
		t.Errorf("percent after half = %d, want 50", p)
	}
}

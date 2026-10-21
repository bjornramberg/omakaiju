package fs

import (
	"path/filepath"
	"sync"
	"testing"
)

// The UI reads the copy's progress on every frame while the copy runs on
// another goroutine. Reading the copy's fields directly raced with Step: the
// bounds check and the index expression in CurrentName were two separate
// reads of a counter the copy goroutine was advancing, which could index
// past the end of the job list.
//
// Run with -race, this is the test that finds it.
func TestCopierProgressIsSafeToReadWhileCopying(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeBytes(t, filepath.Join(src, "a.bin"), copyChunk*8)
	writeBytes(t, filepath.Join(src, "b.bin"), copyChunk*8)
	writeBytes(t, filepath.Join(src, "c.bin"), copyChunk*8)

	c, err := NewCopier([]string{src}, dst)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup

	// The copy goroutine drives the state machine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			done, err := c.Step()
			if err != nil || done {
				return
			}
		}
	}()

	// The UI goroutine reads the progress, exactly as the render path does.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5000; i++ {
			_ = c.CurrentName()
			_ = c.Percent()
			_ = c.Copied()
			_ = c.FilesDone()
			_ = c.FilesTotal()
			_ = c.Total()
			_ = c.FileCopied()
			_ = c.FileSize()
		}
	}()

	wg.Wait()
}

// Aborting while a step is in flight must be safe: the abort runs on the UI
// goroutine and the step on the copy goroutine.
func TestCopierAbortIsSafeWhileStepping(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeBytes(t, filepath.Join(src, "big.bin"), copyChunk*8)

	c, err := NewCopier([]string{filepath.Join(src, "big.bin")}, dst)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			done, err := c.Step()
			if err != nil || done {
				return
			}
		}
	}()

	// Abort repeatedly from the UI side, as a user hammering esc would.
	for i := 0; i < 50; i++ {
		if err := c.Abort(); err != nil {
			t.Errorf("abort %d failed: %v", i, err)
		}
	}
	wg.Wait()
}

// The snapshot must be internally consistent: a byte count and a file name
// that belong to different moments would make the progress bar lie.
func TestCopierSnapshotIsInternallyConsistent(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeBytes(t, filepath.Join(src, "first.bin"), copyChunk*4)
	writeBytes(t, filepath.Join(src, "second.bin"), copyChunk*4)

	c, err := NewCopier([]string{src}, dst)
	if err != nil {
		t.Fatal(err)
	}

	for {
		done, err := c.Step()
		if err != nil {
			t.Fatal(err)
		}
		name := c.CurrentName()
		fileDone := c.FileCopied()
		fileSize := c.FileSize()

		// The per-file counters must belong to the file being named: a
		// count larger than the file's size, or a negative one, would mean
		// the snapshot mixed two moments.
		if fileDone < 0 || fileSize < 0 {
			t.Fatalf("negative counters: done=%d size=%d", fileDone, fileSize)
		}
		if fileDone > fileSize {
			t.Fatalf("fileDone %d exceeds fileSize %d for %q", fileDone, fileSize, name)
		}
		if c.Copied() > c.Total() {
			t.Fatalf("copied %d exceeds total %d", c.Copied(), c.Total())
		}
		if done {
			break
		}
	}
}

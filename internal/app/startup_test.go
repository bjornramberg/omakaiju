package app

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func TestStartupTrackerWaitsForTheFrame(t *testing.T) {
	var out bytes.Buffer
	tracker := NewStartupTracker(&out, time.Now())

	// Before the model has produced a frame, a write is not the screen
	// arriving: it is the renderer asking the terminal what it can do.
	if _, err := tracker.Write([]byte("query")); err != nil {
		t.Fatal(err)
	}
	if got := tracker.Elapsed(); got != 0 {
		t.Errorf("the startup was measured before the frame was ready: %v", got)
	}

	tracker.FrameReady()
	if _, err := tracker.Write([]byte("frame")); err != nil {
		t.Fatal(err)
	}
	if got := tracker.Elapsed(); got <= 0 {
		t.Errorf("the startup was not measured after the frame was ready: %v", got)
	}
	if !bytes.Contains(out.Bytes(), []byte("query")) || !bytes.Contains(out.Bytes(), []byte("frame")) {
		t.Errorf("the output did not reach the terminal: %q", out.String())
	}
}

func TestStartupTrackerMeasuresOnce(t *testing.T) {
	var out bytes.Buffer
	tracker := NewStartupTracker(&out, time.Now())
	tracker.FrameReady()
	tracker.Write([]byte("first"))
	first := tracker.Elapsed()
	time.Sleep(2 * time.Millisecond)
	tracker.Write([]byte("second"))
	if tracker.Elapsed() != first {
		t.Errorf("the startup changed after it was measured: %v then %v", first, tracker.Elapsed())
	}
}

func TestStartupTrackerIsATerminal(t *testing.T) {
	// Bubble Tea only treats the output as a terminal when it can ask for the
	// file descriptor. Without it the app never learns the window size and
	// never draws anything.
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tracker := NewStartupTracker(f, time.Now())
	if tracker.Fd() != f.Fd() {
		t.Errorf("the tracker reports fd %d, want %d", tracker.Fd(), f.Fd())
	}
}

func TestProcessStartIsInThePast(t *testing.T) {
	now := time.Now()
	start := ProcessStart(now)
	if start.After(now) {
		t.Errorf("the process start is in the future: %v", start)
	}
	age := now.Sub(start)
	if age < 0 || age > 24*time.Hour {
		t.Errorf("the process start is %v old, which is not plausible", age)
	}
	if age > time.Minute {
		t.Logf("the process start is %v old (the process has been running a while)", age)
	}
}

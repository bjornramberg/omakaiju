package app

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// StartupTracker measures how long the app took to reach the screen.
//
// The number it keeps is the time from the process starting to the first
// frame reaching the terminal — the startup a user actually feels. It is
// measured from the outside, at the point where the bytes leave, because
// that is the moment the screen changes.
//
// The tracker is also the program's output: everything the renderer writes
// passes through it on its way to the terminal.
type StartupTracker struct {
	out io.Writer
	// file is the terminal behind out, when there is one. The tracker stands
	// in for the program's output, and Bubble Tea only treats an output as a
	// terminal when it can ask for its file descriptor — so the tracker has
	// to be able to answer that, or the app never learns the size of the
	// window and never draws.
	file *os.File

	// frameReady is set once the model has produced its first frame. The
	// renderer writes capability queries and other setup sequences before
	// that, and those are not the screen arriving.
	frameReady atomic.Bool

	// elapsedNanos holds the measured startup in nanoseconds. Zero means the
	// frame has not reached the terminal yet.
	elapsedNanos atomic.Int64

	// processStart is when the process began, as the system records it.
	processStart time.Time
}

// NewStartupTracker returns a tracker that writes to out and measures from
// processStart. A zero processStart means "from now", which is what happens
// when the system cannot say when the process started.
func NewStartupTracker(out io.Writer, processStart time.Time) *StartupTracker {
	t := &StartupTracker{out: out, processStart: processStart}
	if f, ok := out.(*os.File); ok {
		t.file = f
	}
	return t
}

// Read passes reads through to the terminal. The tracker stands in for the
// output only, but the terminal interface it has to satisfy includes reads.
func (t *StartupTracker) Read(p []byte) (int, error) {
	if t.file == nil {
		return 0, os.ErrInvalid
	}
	return t.file.Read(p)
}

// Close does not close the terminal. The program does not own stdout's
// lifetime, and the tracker only borrows it.
func (t *StartupTracker) Close() error { return nil }

// Fd is the terminal's file descriptor, which is how Bubble Tea recognises
// that its output is a terminal at all.
func (t *StartupTracker) Fd() uintptr {
	if t.file == nil {
		return ^uintptr(0)
	}
	return t.file.Fd()
}

// Write passes the bytes to the terminal and, when the first frame is on
// its way, records how long the startup took.
func (t *StartupTracker) Write(p []byte) (int, error) {
	if t == nil {
		return 0, nil
	}
	if t.frameReady.Load() && t.elapsedNanos.Load() == 0 {
		t.elapsedNanos.Store(int64(time.Since(t.start())))
	}
	if t.out == nil {
		return len(p), nil
	}
	return t.out.Write(p)
}

// FrameReady says the model has produced its first frame. The write that
// follows is the one the user sees.
func (t *StartupTracker) FrameReady() {
	if t != nil {
		t.frameReady.Store(true)
	}
}

// Elapsed is how long the startup took, or zero if the first frame has not
// reached the terminal yet.
func (t *StartupTracker) Elapsed() time.Duration {
	if t == nil {
		return 0
	}
	return time.Duration(t.elapsedNanos.Load())
}

// start is the moment the startup is measured from.
func (t *StartupTracker) start() time.Time {
	if t.processStart.IsZero() {
		return time.Now()
	}
	return t.processStart
}

// ProcessStart returns when the process began.
//
// The kernel's own record is the truth: /proc/self/stat says when the
// process started, and /proc/uptime says how long the machine has been
// up, so the two together place the start on the wall clock. Where that
// is not available, the fallback is the moment the program was entered,
// which leaves out the time the runtime spent starting up.
func ProcessStart(fallback time.Time) time.Time {
	if t, ok := linuxProcessStart(time.Now()); ok {
		return t
	}
	return fallback
}

// linuxProcessStart reads the kernel's record of when the process started.
func linuxProcessStart(now time.Time) (time.Time, bool) {
	stat, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return time.Time{}, false
	}
	// The second field is the command name in parentheses, and it may
	// contain spaces and parentheses of its own, so the fields after it are
	// found from the last closing parenthesis.
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return time.Time{}, false
	}
	fields := strings.Fields(string(stat[end+1:]))
	// After the command name, field 3 is the state and field 22 is the
	// start time in clock ticks since the machine booted.
	const startTimeField = 22 - 3
	if len(fields) <= startTimeField {
		return time.Time{}, false
	}
	ticks, err := strconv.ParseUint(fields[startTimeField], 10, 64)
	if err != nil {
		return time.Time{}, false
	}

	uptime, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return time.Time{}, false
	}
	fields = strings.Fields(string(uptime))
	if len(fields) == 0 {
		return time.Time{}, false
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return time.Time{}, false
	}

	// The kernel counts in USER_HZ, which is 100 on every Linux.
	const userHZ = 100
	startedSinceBoot := time.Duration(float64(ticks) / userHZ * float64(time.Second))
	age := time.Duration(seconds*float64(time.Second)) - startedSinceBoot
	if age < 0 {
		age = 0
	}
	return now.Add(-age), true
}

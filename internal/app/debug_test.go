package app

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

// ctrlD builds a Ctrl+D press the way a terminal delivers it: the base key in
// Code and ctrl in Mod.
func ctrlD() tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: 'b', Text: "B", ShiftedCode: 'B', Mod: tea.ModShift})
}

// Shift+B is deliberately not a nav binding: it toggles the hidden pane
// directly, so it never appears in the keybinding table.
func TestDebugPaneTogglesWithCtrlD(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 100, 40

	if m.debugActive {
		t.Fatal("the debug pane must start closed")
	}

	updated, cmd := m.Update(ctrlD())
	m = updated.(Model)
	if !m.debugActive {
		t.Fatal("Shift+B should open the debug pane")
	}
	if cmd == nil {
		t.Fatal("opening the pane should schedule the first sample tick")
	}

	updated, cmd = m.Update(ctrlD())
	m = updated.(Model)
	if m.debugActive {
		t.Fatal("Shift+B should close the debug pane")
	}
	if cmd != nil {
		t.Fatal("closing the pane must return no command")
	}
}

// A closed pane costs nothing: toggling it off returns no command, and a tick
// that was already in flight is dropped without sampling or re-arming.
func TestDebugPaneClosedSchedulesNothingAndDropsLateTick(t *testing.T) {
	m, _ := fixture(t)
	m.debugActive = true
	m.debugStats = m.sampleDebugStats()
	before := m.debugStats

	updated, cmd := m.Update(ctrlD())
	m = updated.(Model)
	if m.debugActive {
		t.Fatal("Shift+B should close the pane")
	}
	if cmd != nil {
		t.Fatal("closing the pane must not clone the tick loop")
	}

	updated, cmd = m.Update(debugTickMsg{})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("a late tick must not schedule the next one")
	}
	if m.debugStats != before {
		t.Fatal("a late tick must not sample while the pane is closed")
	}
}

// While the pane is open a tick re-samples and schedules the next tick.
func TestDebugTickResamplesAndRearms(t *testing.T) {
	m, _ := fixture(t)
	m.debugActive = true
	m.startedAt = time.Now().Add(-time.Second)
	if m.debugStats != (ui.DebugStats{}) {
		t.Fatal("the model should start without a sample")
	}

	updated, cmd := m.Update(debugTickMsg{})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("an open pane's tick should schedule the next one")
	}
	if m.debugStats.Alloc == 0 {
		t.Error("tick did not record heap usage")
	}
	if m.debugStats.Goroutines < 1 {
		t.Errorf("tick recorded %d goroutines", m.debugStats.Goroutines)
	}
	if m.debugStats.Uptime < time.Second {
		t.Errorf("uptime %v, want at least one second", m.debugStats.Uptime)
	}
}

// The sampler fills in heap, goroutine and GC numbers, and the uptime advances
// between samples.
func TestDebugSamplerFillsStatsAndUptimeAdvances(t *testing.T) {
	m, _ := fixture(t)
	m.startedAt = time.Now().Add(-time.Second)

	// Make sure at least one collection is counted, whatever the test binary
	// has done so far.
	runtime.GC()

	first := m.sampleDebugStats()
	if first.Alloc == 0 {
		t.Error("sampler reported zero heap in use")
	}
	if first.Sys == 0 {
		t.Error("sampler reported zero memory obtained from the OS")
	}
	if first.Goroutines < 1 {
		t.Errorf("sampler reported %d goroutines", first.Goroutines)
	}
	if first.NumGC == 0 {
		t.Error("sampler reported no GC runs after forcing one")
	}

	time.Sleep(20 * time.Millisecond)
	second := m.sampleDebugStats()
	if second.Uptime <= first.Uptime {
		t.Errorf("uptime did not advance: %v then %v", first.Uptime, second.Uptime)
	}
}

// The pane is actually drawn when open, and the frame still fits the terminal.
func TestDebugPaneViewDrawsAndFits(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 100, 60
	m.debugActive = true
	m.debugStats = m.sampleDebugStats()

	out := stripTestANSI(m.View().Content)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != m.height {
		t.Fatalf("rendered %d rows, want %d", len(lines), m.height)
	}
	for i, line := range lines {
		if got := visibleWidth(line); got > m.width {
			t.Fatalf("row %d is %d cells wide, want at most %d", i, got, m.width)
		}
	}
	for _, want := range []string{"debug", "version", "uptime", "preview"} {
		if !strings.Contains(out, want) {
			t.Errorf("open pane does not show %q:\n%s", want, out)
		}
	}
}

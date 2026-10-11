package ui

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
)

func TestSpinnerFrameCycles(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < len(spinnerFrames); i++ {
		seen[SpinnerFrame(i)] = true
	}
	if len(seen) != len(spinnerFrames) {
		t.Errorf("expected %d distinct frames, got %d", len(spinnerFrames), len(seen))
	}

	// The animation must wrap rather than run off the end.
	if SpinnerFrame(len(spinnerFrames)) != SpinnerFrame(0) {
		t.Error("spinner did not cycle back to the first frame")
	}
	if SpinnerFrame(-1) != SpinnerFrame(0) {
		t.Error("a negative frame should fall back to the first")
	}
}

// While the walk runs the row must carry an animated glyph, so a slow search
// looks alive instead of frozen.
func TestScanningRowIsAnimated(t *testing.T) {
	f := NewFuzzyFinder(100, 30, config.DefaultTheme())
	f.Loading = true
	f.Root = "/home/user"
	// The animation only runs once there is a query to wait on.
	f.Input = "need"
	f.Spinner = 3

	out := stripANSI(f.Render())
	if !strings.Contains(out, "scanning") {
		t.Errorf("expected a scanning notice, got:\n%s", out)
	}
	if !strings.Contains(out, SpinnerFrame(3)) {
		t.Errorf("expected frame %q in the row, got:\n%s", SpinnerFrame(3), out)
	}

	// A later frame must render differently, which is what makes it read as
	// motion rather than decoration.
	f.Spinner = 4
	if stripANSI(f.Render()) == out {
		t.Error("advancing the spinner did not change the rendered row")
	}
}

// Opening the finder must not animate: a walk that lands immediately would
// otherwise flash a spinner for results nobody has had time to see.
func TestScanningRowIsStillBeforeFirstKeypress(t *testing.T) {
	f := NewFuzzyFinder(100, 30, config.DefaultTheme())
	f.Loading = true
	f.Root = "/home/user"
	f.Input = ""

	out := stripANSI(f.Render())
	if !strings.Contains(out, "scanning") {
		t.Errorf("expected a scanning notice, got:\n%s", out)
	}
	for _, frame := range spinnerFrames {
		if strings.Contains(out, frame) {
			t.Errorf("spinner %q shown before anything was typed", frame)
		}
	}
}

// Once loading ends the animation must not linger on screen.
func TestSpinnerAbsentWhenLoaded(t *testing.T) {
	f := NewFuzzyFinder(100, 30, config.DefaultTheme())
	f.Loading = false
	f.Spinner = 2
	f.Results = []string{"/tmp/one.go"}

	out := stripANSI(f.Render())
	for _, frame := range spinnerFrames {
		if strings.Contains(out, frame) {
			t.Errorf("spinner frame %q still shown after loading finished", frame)
		}
	}
}

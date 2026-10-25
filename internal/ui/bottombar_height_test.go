package ui

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
)

// renderedLines counts the rows a rendered string occupies. The bottom bar
// reserves Layout.BottomBarHeight rows, so every state has to render exactly
// that many.
func renderedLines(s string) int {
	return strings.Count(s, "\n") + 1
}

// A copy in flight must not shrink the bar: the panes above it would move at
// the moment the user starts watching the copy.
func TestBottomBarProgressKeepsBarHeight(t *testing.T) {
	theme := config.DefaultTheme()
	want := NewLayout(80, 24, theme).BottomBarHeight()

	idle := NewBottomBar(80, theme)
	if got := renderedLines(idle.Render()); got != want {
		t.Fatalf("idle bar is %d rows, want %d", got, want)
	}

	p := testProgress(2359296, 4718592, 50, 3, 28)
	busy := NewBottomBar(80, theme)
	busy.Progress = &p
	if got := renderedLines(busy.Render()); got != want {
		t.Errorf("progress bar is %d rows, want %d (same as idle)", got, want)
	}
}

// Every state the bar can be in has to occupy the same number of rows, or the
// layout jumps whenever the state changes.
func TestBottomBarHeightSameInEveryState(t *testing.T) {
	theme := config.DefaultTheme()
	want := NewLayout(80, 24, theme).BottomBarHeight()
	p := testProgress(2359296, 4718592, 50, 3, 28)

	states := []struct {
		name  string
		setup func(*BottomBar)
	}{
		{"error", func(b *BottomBar) { b.Error = "permission denied" }},
		{"result", func(b *BottomBar) { b.OpResult = "moved 3 file(s)" }},
		{"prompt", func(b *BottomBar) { b.Prompt = "rename"; b.PromptInput = "new-name" }},
		{"input", func(b *BottomBar) { b.Input = "delete?" }},
		{"fuzzy", func(b *BottomBar) { b.FuzzyActive = true; b.FuzzyInput = "arch" }},
		{"progress", func(b *BottomBar) { b.Progress = &p }},
		{"hints", func(b *BottomBar) {}},
	}

	for _, tc := range states {
		b := NewBottomBar(80, theme)
		tc.setup(&b)
		if got := renderedLines(b.Render()); got != want {
			t.Errorf("%s bar is %d rows, want %d", tc.name, got, want)
		}
	}
}

// Making the bar the right height must not cost any of the progress detail.
func TestBottomBarProgressStillShowsCountersAndBar(t *testing.T) {
	theme := config.DefaultTheme()
	bar := NewBottomBar(80, theme)
	p := testProgress(2359296, 4718592, 50, 3, 28)
	bar.Progress = &p

	out := stripANSI(bar.Render())
	for _, want := range []string{
		"copying",
		"50%",
		"2MB/5MB",
		"file 3/28",
		"█",
		"░",
		"(esc: cancel)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("progress bar lost %q: %q", want, out)
		}
	}
}

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/bjornramberg/omakaiju/internal/config"
)

// The pane is drawn in a fixed column, so every row must be clipped to the
// panel's content width and the frame must never grow past its allocation.
func TestDebugPaneClipsRowsToWidth(t *testing.T) {
	theme := config.DefaultTheme()
	long := strings.Repeat("very-long-directory-name/", 30) + "file.txt"

	for _, tc := range []struct{ w, h int }{
		{20, 20}, {30, 16}, {40, 8}, {80, 24}, {20, 5}, {30, 3},
	} {
		d := NewDebug(tc.w, tc.h, theme)
		d.Info = DebugInfo{
			Version:     "v0.12.0",
			PID:         4242,
			ActivePath:  long,
			PreviewPath: long,
			ThemePath:   long,
			Filter:      strings.Repeat("q", 200),
			Width:       200,
			Height:      60,
		}

		out := d.Render()
		lines := strings.Split(out, "\n")
		if len(lines) != tc.h {
			t.Errorf("%dx%d: rendered %d rows, want %d", tc.w, tc.h, len(lines), tc.h)
		}
		for i, line := range lines {
			if got := DisplayWidth(line); got != tc.w {
				t.Errorf("%dx%d: row %d is %d cells, want %d: %q", tc.w, tc.h, i, got, tc.w, line)
			}
		}
	}
}

// The pane reports every value it was given, with honest placeholders where
// there is nothing to report.
func TestDebugPaneReportsValues(t *testing.T) {
	d := NewDebug(100, 40, config.DefaultTheme())
	d.Info = DebugInfo{
		Stats: DebugStats{
			Alloc:      4 << 20,
			Sys:        64 << 20,
			NumGC:      7,
			Goroutines: 12,
			Uptime:     90 * time.Second,
		},
		Version:     "v0.12.0",
		PID:         4242,
		ActivePath:  "/home/user/projects/omakaiju",
		Entries:     8,
		Cursor:      2,
		Marked:      3,
		Sort:        "size↓",
		Filter:      "go",
		PreviewPath: "/home/user/projects/omakaiju/README.md",
		ThemePath:   "/home/user/.config/omarchy/current/theme/colors.toml",
		Width:       120,
		Height:      40,
	}

	out := d.Render()
	for _, want := range []string{
		"debug",
		"v0.12.0",
		"4242",
		"4.0 MiB",
		"64.0 MiB",
		"12",
		"7",
		"1m30s",
		"omakaiju",
		"8 (cursor 3)",
		"size↓",
		"go",
		"README.md",
		"colors.toml",
		"120x40",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("debug pane does not show %q:\n%s", want, out)
		}
	}

	empty := NewDebug(100, 40, config.DefaultTheme())
	empty.Info = DebugInfo{Version: "dev", Width: 80, Height: 24}
	out = empty.Render()
	for _, want := range []string{"(none)", "(built-in palette)"} {
		if !strings.Contains(out, want) {
			t.Errorf("empty debug pane does not show %q:\n%s", want, out)
		}
	}
}

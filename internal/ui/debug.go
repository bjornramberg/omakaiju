package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/bjornramberg/omakaiju/internal/config"
)

// DebugStats is one sample of the runtime telemetry the debug pane reports.
// It is gathered by the app, never by the renderer, and only while the pane is
// open.
type DebugStats struct {
	Alloc      uint64
	Sys        uint64
	NumGC      uint32
	Goroutines int
	Uptime     time.Duration
}

// DebugInfo is everything the debug pane draws. The values are gathered by the
// caller; the pane only formats and frames them.
type DebugInfo struct {
	Stats   DebugStats
	Version string
	PID     int
	// StartedAt is when the app began, so the pane can show the boot time
	// next to the uptime.
	StartedAt time.Time
	// Startup is how long the app took to reach the screen: from the process
	// starting to the first frame reaching the terminal.
	Startup       time.Duration
	ActivePath    string
	Entries       int
	Cursor        int
	Marked        int
	Sort          string
	Filter        string
	PreviewPath   string
	PreviewHidden bool
	ThemePath     string
	Width         int
	Height        int
}

// Debug is the hidden telemetry pane. It shares the preview's panel style so
// the two read as one system, and clips every row to its content width so a
// long path can never wrap or push the frame past its allocation.
type Debug struct {
	Width  int
	Height int
	Theme  config.Theme
	Info   DebugInfo
}

func NewDebug(width, height int, theme config.Theme) Debug {
	return Debug{Width: width, Height: height, Theme: theme}
}

// ContentWidth is the usable width inside the panel border and padding.
func (d Debug) ContentWidth() int {
	w := d.Width - 2 /*border*/ - 4 /*padding*/
	if w < 1 {
		return 1
	}
	return w
}

// ContentHeight is the rows left inside the panel border and padding. A pane
// too short to have any content returns zero; the frame is still drawn.
func (d Debug) ContentHeight() int {
	h := d.Height - 2 /*border*/ - 2 /*vertical padding*/
	if h < 0 {
		return 0
	}
	return h
}

// debugLabelWidth keeps the value column aligned across every row.
const debugLabelWidth = 11

// valueBudget is how many cells a value may use before the row is clipped.
func (d Debug) valueBudget() int {
	w := d.ContentWidth() - debugLabelWidth - 1
	if w < 1 {
		return 1
	}
	return w
}

func (d Debug) row(label, value string) string {
	if w := DisplayWidth(label); w < debugLabelWidth {
		label += strings.Repeat(" ", debugLabelWidth-w)
	}
	return d.Theme.StatusText().Render(label) + d.Theme.Text().Render(" "+value)
}

// pathValue shortens a path from the left, which keeps the part that locates
// the user, and sanitises it before it can reach the terminal.
func (d Debug) pathValue(path, empty string) string {
	if path == "" {
		return empty
	}
	return TrimPathLeft(SanitizeName(path), d.valueBudget())
}

func (d Debug) rows() []string {
	stats := d.Info.Stats
	uptime := stats.Uptime
	if uptime < 0 {
		uptime = 0
	}

	cursor := 0
	if d.Info.Entries > 0 {
		cursor = d.Info.Cursor + 1
		if cursor > d.Info.Entries {
			cursor = d.Info.Entries
		}
		if cursor < 1 {
			cursor = 1
		}
	}
	entries := fmt.Sprintf("%d (cursor %d)", d.Info.Entries, cursor)

	filter := d.Info.Filter
	if filter == "" {
		filter = "(none)"
	} else {
		filter = SanitizeName(filter)
	}

	preview := d.pathValue(d.Info.PreviewPath, "(none)")
	if d.Info.PreviewHidden {
		preview = "(preview hidden)"
	}

	version := d.Info.Version
	if version == "" {
		version = "dev"
	}

	sortMode := d.Info.Sort
	if sortMode == "" {
		sortMode = "name"
	}

	return []string{
		d.row("version", version),
		d.row("pid", fmt.Sprintf("%d", d.Info.PID)),
		d.row("heap", formatBytes(stats.Alloc)),
		d.row("memory", formatBytes(stats.Sys)),
		d.row("goroutines", fmt.Sprintf("%d", stats.Goroutines)),
		d.row("gc", fmt.Sprintf("%d", stats.NumGC)),
		d.row("uptime", uptime.Round(time.Second).String()),
		d.row("started", d.startedValue()),
		d.row("startup", d.startupValue()),
		d.row("path", d.pathValue(d.Info.ActivePath, "(none)")),
		d.row("entries", entries),
		d.row("marked", fmt.Sprintf("%d", d.Info.Marked)),
		d.row("sort", sortMode),
		d.row("filter", filter),
		d.row("preview", preview),
		d.row("theme", d.pathValue(d.Info.ThemePath, "(built-in palette)")),
		d.row("size", fmt.Sprintf("%dx%d", d.Info.Width, d.Info.Height)),
	}
}

// startedValue renders when the app was started.
//
// The boot time is what a bug report wants next to the uptime: one says
// how long it has been running, the other says when it began, and the
// two together place the run on a timeline.
func (d Debug) startedValue() string {
	if d.Info.StartedAt.IsZero() {
		return "(none)"
	}
	return d.Info.StartedAt.Format("2006-01-02 15:04:05")
}

// startupValue renders how long the app took to reach the screen.
//
// The number is measured where the bytes leave, so it includes the time
// the runtime spent starting up and the time the terminal took to answer
// the capability queries — which is what a user waiting for the window
// actually waits through.
func (d Debug) startupValue() string {
	if d.Info.Startup <= 0 {
		return "(pending)"
	}
	return d.Info.Startup.Round(100 * time.Microsecond).String()
}

func (d Debug) Render() string {
	header := d.Theme.AccentText().Bold(true).Render(TruncateName("debug", d.ContentWidth()))
	rows := append([]string{header}, d.rows()...)

	// Clip before joining: one source row must stay one rendered row, so a
	// long path cannot wrap and make the frame taller than its allocation.
	rows = clipLines(rows, d.ContentWidth())
	if max := d.ContentHeight(); len(rows) > max {
		rows = rows[:max]
	}

	return d.Theme.PreviewPanel().
		Width(d.Width).
		MaxWidth(d.Width).
		Height(d.Height).
		MaxHeight(d.Height).
		Render(strings.Join(rows, "\n"))
}

// formatBytes renders a byte count the way a person reads memory: a single
// decimal and the largest binary unit that keeps the number below 1024.
func formatBytes(n uint64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	value := float64(n)
	unit := -1
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

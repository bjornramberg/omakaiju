package app

import (
	"runtime"
	"time"

	"github.com/bjornramberg/omakaiju/internal/ui"
)

// sampleDebugStats gathers the debug pane's runtime telemetry in one pass.
//
// It is called only when the pane is open: once when it is toggled on and once
// per 500 ms tick. Nothing gathers telemetry while the pane is closed. ReadMemStats
// stops the world for the sample, so keeping it off the render path and out of
// the closed state is the whole point of the lazy loop.
func (m Model) sampleDebugStats() ui.DebugStats {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ui.DebugStats{
		Alloc:      ms.Alloc,
		Sys:        ms.Sys,
		NumGC:      ms.NumGC,
		Goroutines: runtime.NumGoroutine(),
		Uptime:     time.Since(m.startedAt),
	}
}

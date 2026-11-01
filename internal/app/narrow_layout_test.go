package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/ui"
)

// narrowPreviewModel builds a model showing a single small file at the given
// terminal size. The preview is not gathered, so the test sees the command an
// updatePreview would dispatch.
func narrowPreviewModel(t *testing.T, width, height int) (Model, string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "only.txt")
	if err := os.WriteFile(path, []byte("narrow preview body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "nonexistent.toml")})
	m.width, m.height = width, height
	m.leftPath, m.leftFiles = dir, files
	m.rightPath, m.rightFiles = dir, files
	return m, path
}

// When the layout has given the preview's column to the file panes, there is
// nothing to show it in, so gathering it would be a full read and highlight
// whose result is thrown away.
func TestPreviewNotGatheredWhenLayoutSuppressesIt(t *testing.T) {
	const width, height = 60, 30
	m, path := narrowPreviewModel(t, width, height)
	if !m.layout().PreviewSuppressed() {
		t.Fatalf("setup: the preview should be suppressed at %dx%d", width, height)
	}

	if cmd := m.updatePreview(); cmd != nil {
		t.Error("a preview was loaded even though the layout does not draw it")
	}
	if m.previewContent.Text != nil {
		t.Errorf("preview content = %q, want nothing gathered", m.previewContent.Text)
	}
	if m.previewCachePath != "" {
		t.Errorf("previewCachePath = %q, want empty", m.previewCachePath)
	}
	if m.previewPath != path {
		t.Errorf("previewPath = %q, want the file under the cursor %q", m.previewPath, path)
	}

	// The terminal too small for a layout at all is the same story.
	m.width = 40
	if cmd := m.updatePreview(); cmd != nil {
		t.Error("a preview was loaded for a terminal that cannot even draw the layout")
	}
}

// A terminal with room for the column still previews the file under the
// cursor, and the bar says nothing about the preview.
func TestPreviewGatheredWhenLayoutHasRoom(t *testing.T) {
	const width, height = 120, 40
	m, path := narrowPreviewModel(t, width, height)
	if !m.layout().PreviewVisible() {
		t.Fatalf("setup: the preview should be visible at %dx%d", width, height)
	}

	cmd := m.updatePreview()
	if cmd == nil {
		t.Fatal("no load was dispatched for a visible preview")
	}
	updated, _ := m.Update(cmd())
	m = updated.(Model)

	if m.previewCachePath != path {
		t.Errorf("previewCachePath = %q, want %q", m.previewCachePath, path)
	}
	if !previewContains(m, "narrow preview body") {
		t.Errorf("the preview did not show the file under the cursor: %q", previewText(m))
	}
	if view := plainText(m.View().Content); strings.Contains(view, previewSuppressedNote) {
		t.Errorf("the bar mentions a suppressed preview even though it is drawn:\n%s", view)
	}
}

// The bar distinguishes a preview the layout had to drop for lack of room from
// one the user turned off with Shift+I.
func TestBottomBarTellsSuppressedPreviewFromHiddenPreview(t *testing.T) {
	t.Run("suppressed by width", func(t *testing.T) {
		m, _ := narrowPreviewModel(t, 60, 30)
		if view := plainText(m.View().Content); !strings.Contains(view, previewSuppressedNote) {
			t.Errorf("the bar does not say the preview was suppressed:\n%s", view)
		}
	})

	t.Run("turned off by the user", func(t *testing.T) {
		m, _ := narrowPreviewModel(t, 60, 30)
		updated, cmd := m.Update(shiftKey('i'))
		m = updated.(Model)
		m = applyEveryMessage(t, m, cmd)
		if !m.previewHidden {
			t.Fatal("Shift+I did not turn the preview off")
		}
		if view := plainText(m.View().Content); strings.Contains(view, previewSuppressedNote) {
			t.Errorf("the bar blames the width for a preview the user turned off:\n%s", view)
		}
	})

	t.Run("drawn", func(t *testing.T) {
		m, _ := narrowPreviewModel(t, 120, 40)
		m = settlePreview(t, m)
		if view := plainText(m.View().Content); strings.Contains(view, previewSuppressedNote) {
			t.Errorf("the bar mentions a suppressed preview even though it is drawn:\n%s", view)
		}
	})
}

// The debug pane samples runtime telemetry only while it is actually drawn.
// An open pane with no room stays quiet but keeps its request, so it resumes
// when the terminal grows.
func TestDebugTelemetryFollowsTheLayout(t *testing.T) {
	m, _ := fixture(t)
	m.startedAt = time.Now().Add(-time.Second)

	// Narrow: the pane is open, but the layout gives the right column to the
	// file panes.
	m.width, m.height = 60, 30
	m.debugActive = true
	if m.debugDrawn() {
		t.Fatal("setup: the debug pane should not be drawn at 60x30")
	}
	m.debugStats = ui.DebugStats{}
	updated, _ := m.Update(debugTickMsg{})
	m = updated.(Model)
	if m.debugStats != (ui.DebugStats{}) {
		t.Errorf("telemetry was sampled for an undrawn pane: %+v", m.debugStats)
	}
	if !m.debugActive {
		t.Error("an undrawn pane must keep the request so it returns with room")
	}

	// Opening the pane while there is no room must not sample either.
	m.debugActive = false
	m.debugStats = ui.DebugStats{}
	updated, _ = m.Update(ctrlD())
	m = updated.(Model)
	if !m.debugActive {
		t.Fatal("setup: the pane should have opened")
	}
	if m.debugStats != (ui.DebugStats{}) {
		t.Errorf("opening an undrawn pane sampled telemetry: %+v", m.debugStats)
	}

	// Wide enough to draw it, the next tick samples.
	m.width, m.height = 120, 40
	if !m.debugDrawn() {
		t.Fatal("setup: the debug pane should be drawn at 120x40")
	}
	updated, _ = m.Update(debugTickMsg{})
	m = updated.(Model)
	if m.debugStats.Alloc == 0 {
		t.Error("telemetry was not gathered for a drawn pane")
	}
}

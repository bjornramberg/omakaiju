package ui

import (
	"os"
	"strings"
	"testing"

	"omakaiju/internal/config"
	"omakaiju/internal/fs"
)

func writeFileSized(path string, n int) error {
	return os.WriteFile(path, make([]byte, n), 0o644)
}

func testProgress(copied, total int64, percent int, files, fileTotal int) Progress {
	return Progress{
		Name:      "big-archive.tar.gz",
		Copied:    copied,
		Total:     total,
		Percent:   percent,
		FileIndex: files,
		FileTotal: fileTotal,
		Theme:     config.DefaultTheme(),
	}
}

func TestProgressRenderFitsWidth(t *testing.T) {
	p := testProgress(2359296, 4718592, 50, 3, 28)

	for _, width := range []int{20, 40, 60, 80, 120} {
		out := p.Render(width)
		if w := DisplayWidth(out); w > width {
			t.Errorf("width %d: rendered %d cells", width, w)
		}
	}
}

func TestProgressRenderContainsPercentAndSizes(t *testing.T) {
	p := testProgress(2359296, 4718592, 50, 3, 28)
	out := stripANSI(p.Render(100))

	if !strings.Contains(out, "50%") {
		t.Errorf("missing percent: %q", out)
	}
	if !strings.Contains(out, "2.2 MB/4.5 MB") {
		t.Errorf("missing aggregate sizes: %q", out)
	}
	if !strings.Contains(out, "file 3/28") {
		t.Errorf("missing file counter: %q", out)
	}
	if !strings.Contains(out, "(esc: cancel)") {
		t.Errorf("missing cancel hint: %q", out)
	}
}

func TestProgressRenderOmitsFileCounterForSingleFile(t *testing.T) {
	p := testProgress(500, 1000, 50, 1, 1)
	out := stripANSI(p.Render(100))
	if strings.Contains(out, "file ") {
		t.Errorf("single-file copy should not show a file counter: %q", out)
	}
}

func TestProgressBarIsProportional(t *testing.T) {
	half := stripANSI(testProgress(50, 100, 50, 1, 1).bar(10))
	full := stripANSI(testProgress(100, 100, 100, 1, 1).bar(10))
	empty := stripANSI(testProgress(0, 100, 0, 1, 1).bar(10))

	if n := strings.Count(half, "█"); n != 5 {
		t.Errorf("50%% bar has %d filled cells, want 5", n)
	}
	if n := strings.Count(full, "█"); n != 10 {
		t.Errorf("100%% bar has %d filled cells, want 10", n)
	}
	if strings.Count(empty, "█") != 0 {
		t.Error("0% bar should have no filled cells")
	}
	if len([]rune(half)) != 10 {
		t.Errorf("bar width = %d, want 10", len([]rune(half)))
	}
}

func TestProgressBarHandlesZeroTotal(t *testing.T) {
	// A zero total must not divide by zero or panic.
	p := testProgress(0, 0, 0, 1, 1)
	if out := p.Render(80); DisplayWidth(out) > 80 {
		t.Errorf("zero-total render too wide: %d", DisplayWidth(out))
	}
}

func TestProgressNarrowWidthFallsBackToCounters(t *testing.T) {
	p := testProgress(50, 100, 50, 1, 1)
	// Too narrow for a bar, but the line must still be bounded.
	for _, width := range []int{4, 8, 12} {
		out := p.Render(width)
		if w := DisplayWidth(out); w > width {
			t.Errorf("width %d: rendered %d cells", width, w)
		}
	}
}

func TestProgressFromCopier(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/a.bin"
	if err := writeFileSized(src, 4096); err != nil {
		t.Fatal(err)
	}
	dst := dir + "/out"

	c, err := fs.NewCopier([]string{src}, dst)
	if err != nil {
		t.Fatal(err)
	}

	p := NewProgress(c, config.DefaultTheme())
	if p.Name != "a.bin" {
		t.Errorf("Name = %q, want a.bin", p.Name)
	}
	if p.Total != 4096 {
		t.Errorf("Total = %d, want 4096", p.Total)
	}
	if p.FileIndex != 1 || p.FileTotal != 1 {
		t.Errorf("file counter = %d/%d, want 1/1", p.FileIndex, p.FileTotal)
	}
}

func TestBottomBarShowsProgress(t *testing.T) {
	theme := config.DefaultTheme()
	bar := NewBottomBar(80, theme)
	p := testProgress(50, 100, 50, 1, 1)
	bar.Progress = &p

	out := stripANSI(bar.Render())
	if !strings.Contains(out, "50%") {
		t.Errorf("bottom bar should show progress: %q", out)
	}
}

func TestBottomBarProgressBeatsOpResult(t *testing.T) {
	theme := config.DefaultTheme()
	bar := NewBottomBar(80, theme)
	bar.OpResult = "pasted 2 file(s)"
	p := testProgress(50, 100, 50, 1, 1)
	bar.Progress = &p

	out := stripANSI(bar.Render())
	if !strings.Contains(out, "50%") {
		t.Error("progress should take precedence over a stale op result")
	}
	if strings.Contains(out, "pasted 2") {
		t.Error("op result should be hidden while a copy runs")
	}
}

func TestBottomBarFallsBackToOpResultWhenNoProgress(t *testing.T) {
	theme := config.DefaultTheme()
	bar := NewBottomBar(80, theme)
	bar.OpResult = "moved 3 file(s)"

	if out := stripANSI(bar.Render()); !strings.Contains(out, "moved 3") {
		t.Errorf("expected op result when idle: %q", out)
	}
}

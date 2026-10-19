package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/version"
)

// The splash timeline is fixed: a short ramp in, a hold landing the fade-out
// start around the three second mark, and a short ramp out.
func TestSplashTimelineMatchesDesign(t *testing.T) {
	if SplashTick != 100*time.Millisecond {
		t.Errorf("expected 100ms frames, got %v", SplashTick)
	}

	rampIn := SplashAlpha(0)
	if rampIn <= 0 || rampIn >= 0.25 {
		t.Errorf("first frame should be barely visible, got alpha %v", rampIn)
	}
	if a := SplashAlpha(splashFadeInSteps - 1); a != 1 {
		t.Errorf("fade-in complete by step %d, got alpha %v", splashFadeInSteps-1, a)
	}
	for step := splashFadeInSteps; step < splashFadeInSteps+splashHoldSteps; step++ {
		if a := SplashAlpha(step); a != 1 {
			t.Fatalf("hold phase should be at full strength, step %d got %v", step, a)
		}
	}

	fadeOutStart := splashFadeInSteps + splashHoldSteps
	if a := SplashAlpha(fadeOutStart); a >= 0.95 || a <= 0.6 {
		t.Errorf("fade-out should start descending from full, step %d got %v", fadeOutStart, a)
	}
	if a := SplashAlpha(splashTotalSteps - 1); a != 0 {
		t.Errorf("final frame should be fully invisible, got %v", a)
	}
	for _, step := range []int{splashTotalSteps, splashTotalSteps + 50, -7} {
		if a := SplashAlpha(step); a != 0 {
			t.Errorf("out-of-range step %d should be invisible, got %v", step, a)
		}
	}
}

// The animation loop must end on its own so no hidden ticker runs forever.
func TestSplashActiveEndsWhenFadedOut(t *testing.T) {
	for step := 0; step < splashTotalSteps-1; step++ {
		if !SplashActive(step) {
			t.Fatalf("splash stopped early at step %d", step)
		}
	}
	for _, step := range []int{splashTotalSteps - 1, splashTotalSteps, splashTotalSteps + 1} {
		if SplashActive(step) {
			t.Errorf("splash still active at step %d", step)
		}
	}
}

func TestSplashLabelsStampedVersion(t *testing.T) {
	orig := version.Version
	defer func() { version.Version = orig }()

	version.Version = "0.9.1"
	if got := stripANSI(Splash(config.DefaultTheme(), splashFadeInSteps)); got != "omakaiju v0.9.1" {
		t.Errorf("stamped version label wrong: %q", got)
	}

	// The Makefile stamps raw `git describe` output, so a leading v can
	// arrive and must not be doubled.
	version.Version = "v0.9.1-dirty"
	if got := stripANSI(Splash(config.DefaultTheme(), splashFadeInSteps)); got != "omakaiju v0.9.1-dirty" {
		t.Errorf("describe output mangled: %q", got)
	}

	version.Version = "dev"
	if got := stripANSI(Splash(config.DefaultTheme(), splashFadeInSteps)); got != "omakaiju dev" {
		t.Errorf("unstamped build should read 'omakaiju dev', got %q", got)
	}
}

func TestSplashInvisibleStepsRenderEmpty(t *testing.T) {
	theme := config.DefaultTheme()
	for _, step := range []int{-1, splashTotalSteps - 1, splashTotalSteps + 3} {
		if got := Splash(theme, step); got != "" {
			t.Errorf("step %d should render nothing, got %q", step, got)
		}
	}
}

// Even an unusable palette must still show the version, at full strength
// rather than refusing to render.
func TestSplashFallsBackWhenPaletteUnparsable(t *testing.T) {
	theme := config.Theme{BGDark: "nothex", Subtle: "nothex"}
	if got := stripANSI(Splash(theme, splashFadeInSteps)); got == "" {
		t.Error("bad palette should fall back to plain text, got empty render")
	}
}

// joinSplash must land the splash flush on the right edge of the content
// area, or drop it when the text would wrap.
func TestJoinSplashRightAligns(t *testing.T) {
	// Wide enough that the help text and the splash both fit with room to
	// spare: 88 cells of help, 15 of splash, inside 110-4 = 106 content cells.
	b := NewBottomBar(110, config.DefaultTheme())
	b.Splash = "omakaiju v0.9.1"

	help := "h/j/k/l:nav  y:yank  m:move  p:paste  d:delete  /:filter  o:sort  s:side-by-side  q:quit"
	joined := b.joinSplash(b.Theme.StatusText().Render(help))
	if got := DisplayWidth(joined); got != b.contentWidth() {
		t.Errorf("splash row should exactly fill the content area, got %d of %d cells", got, b.contentWidth())
	}
	if !strings.Contains(stripANSI(joined), "omakaiju v0.9.1") {
		t.Error("splash text missing from the joined row")
	}

	// A terminal too narrow for help plus splash keeps the help intact.
	b.Width = 80
	narrow := b.joinSplash(b.Theme.StatusText().Render(help))
	if DisplayWidth(narrow) != DisplayWidth(help) {
		t.Errorf("splash must be dropped when it cannot fit: got %d cells, help alone is %d",
			DisplayWidth(narrow), DisplayWidth(help))
	}
}

// The splash rides the help row only: a bar taken over by an error has no
// default help row, so the splash text must not appear anywhere in it.
func TestSplashIgnoredOutsideHelpBranch(t *testing.T) {
	b := NewBottomBar(80, config.DefaultTheme())
	b.Splash = "omakaiju v0.9.1"
	b.Error = "boom"
	if got := stripANSI(b.Render()); strings.Contains(got, "omakaiju") {
		t.Errorf("error row must not carry the splash text: %q", got)
	}
}

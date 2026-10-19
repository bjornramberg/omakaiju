package ui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/lucasb-eyer/go-colorful"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/version"
)

// The version splash draws "omakaiju vX.Y.Z" on the bottom bar's right edge,
// fades it in quickly, lets it rest, and fades it back out into the bar
// background. The whole timeline advances one SplashTick per frame, so every
// client just counts the steps: model ticks, ui renders, tests assert.
const (
	SplashTick = 100 * time.Millisecond

	splashFadeInSteps  = 6 // about 0.6s to reach full strength
	splashHoldSteps    = 24
	splashFadeOutSteps = 8

	// fade in + a hold that, taken together, puts the fade-out start right
	// around the three second mark.
	splashTotalSteps = splashFadeInSteps + splashHoldSteps + splashFadeOutSteps
)

// SplashAlpha maps a frame step onto the splash's remaining strength, in a
// fixed-point friendly [0, 1]. Steps outside the timeline render as nothing.
func SplashAlpha(step int) float64 {
	switch {
	case step < 0:
		return 0
	case step < splashFadeInSteps:
		return float64(step+1) / float64(splashFadeInSteps)
	case step < splashFadeInSteps+splashHoldSteps:
		return 1
	case step < splashTotalSteps:
		return 1 - float64(step-(splashFadeInSteps+splashHoldSteps)+1)/float64(splashFadeOutSteps)
	default:
		return 0
	}
}

// SplashActive reports whether step still needs animation: the loop stops on
// its own once the splash has faded out, so no hidden ticker outlives it.
func SplashActive(step int) bool {
	return step < splashTotalSteps-1
}

// splashLabel formats the release name. GoReleaser stamps the bare patch
// number ("0.9.1"), while the Makefile stamps raw `git describe` output
// ("v0.9.1-dirty"), so the leading v is normalised away rather than doubled.
func splashLabel() string {
	name := version.Version
	if name == "" || name == "dev" {
		return "omakaiju dev"
	}
	return "omakaiju v" + strings.TrimPrefix(name, "v")
}

// splashColor mixes the label colour from the bar's background toward its
// help-text colour by alpha. Faulty palette values fall back to plain
// StatusText at full strength, so a theme with a raw colour string the old
// scheme cannot parse still shows a readable, if unmoving, splash.
func splashColor(theme config.Theme, alpha float64) (string, bool) {
	bg, err1 := colorful.Hex(theme.BGDark)
	fg, err2 := colorful.Hex(theme.Subtle)
	if err1 != nil || err2 != nil {
		return "", false
	}
	return bg.BlendLuv(fg, alpha).Hex(), true
}

// Splash renders the splash content for a frame step, carrying its own
// styling so callers drop it straight into a layout line. An empty string
// means "currently invisible — draw nothing"; width is ignored on purpose,
// placement in the bar is decided by BottomBar based on the help text.
func Splash(theme config.Theme, step int) string {
	alpha := SplashAlpha(step)
	if alpha <= 0 {
		return ""
	}
	label := splashLabel()
	hex, rawok := splashColor(theme, alpha)
	if !rawok {
		return theme.StatusText().Render(label)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render(label)
}

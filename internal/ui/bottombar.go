package ui

import (
	"strings"

	"github.com/bjornramberg/omakaiju/internal/config"
)

type BottomBar struct {
	Width       int
	Input       string
	Error       string
	OpResult    string
	FuzzyActive bool
	FuzzyInput  string
	Prompt      string
	PromptInput string
	// ConfirmQuestion and ConfirmOptions are a two-line confirmation. The
	// question keeps its beginning (unlike a text prompt, which keeps its
	// tail) and the options sit on a row of their own, so both stay readable
	// at the advertised minimum width.
	ConfirmQuestion string
	ConfirmOptions  string
	Progress        *Progress
	// Splash is pre-rendered version text placed flush right on the hints
	// row while a version splash is fading. Empty means no splash.
	Splash string
	Theme  config.Theme
}

func NewBottomBar(width int, theme config.Theme) BottomBar {
	return BottomBar{Width: width, Theme: theme}
}

func (b BottomBar) Render() string {
	if b.FuzzyActive {
		prompt := b.Theme.AccentText().Render("fuzzy> ")
		return b.line(prompt + b.tail("fuzzy> ", b.FuzzyInput))
	}

	if b.Prompt != "" {
		label := b.Prompt + "> "
		return b.line(b.Theme.AccentText().Render(label) + b.tail(label, b.PromptInput))
	}

	if b.ConfirmQuestion != "" {
		return b.line(b.confirmContent())
	}

	if b.Error != "" {
		return b.line(b.Theme.ErrorText().Render(" Error: " + SanitizeName(b.Error)))
	}

	if b.Input != "" {
		return b.line(b.Theme.AccentText().Render("> ") + b.tail("> ", b.Input))
	}

	// An active copy takes over the bar so progress is not hidden behind a
	// stale result message. It goes through line() like every other state so
	// the bar stays BottomBarHeight rows whether or not a copy is running.
	if b.Progress != nil {
		return b.line(b.Progress.Render(b.contentWidth()))
	}

	if b.OpResult != "" {
		// Operation results quote file names, which are filesystem data and
		// may contain bytes the terminal would interpret.
		return b.line(b.Theme.AccentText().Render(" " + SanitizeName(b.OpResult)))
	}

	hints := b.Theme.StatusText().Render("shift+k:keys  q:quit")
	return b.line(b.joinSplash(hints))
}

// joinSplash pads the help text so a fading version splash lands flush
// against the content area's right edge. A splash that would not fit beside
// the help is dropped rather than wrapping the row.
func (b BottomBar) joinSplash(help string) string {
	if b.Splash == "" {
		return help
	}
	gap := b.contentWidth() - DisplayWidth(help) - DisplayWidth(b.Splash)
	if gap < 1 {
		return help
	}
	return help + strings.Repeat(" ", gap) + b.Splash
}

// line renders the bar bounded to the terminal width, so a long message can
// never wrap and shift the layout.
func (b BottomBar) line(content string) string {
	return b.Theme.BottomBar().
		Width(b.Width).
		MaxWidth(b.Width).
		Height(3).
		MaxHeight(3).
		Render(content)
}

// confirmContent draws a confirmation as a question and its answers on
// separate rows. The question keeps its start because that is where the verb
// and the target are; a text prompt keeps its end because that is where the
// typing is. Each row is bounded independently, so a long file name cannot
// push the answers off the screen.
func (b BottomBar) confirmContent() string {
	avail := b.contentWidth() - 2
	question := TruncateEnd(SanitizeName(b.ConfirmQuestion), avail)
	lines := []string{b.Theme.AccentText().Render("> " + question)}
	if b.ConfirmOptions != "" {
		options := TruncateEnd(SanitizeName(b.ConfirmOptions), avail)
		lines = append(lines, b.Theme.StatusText().Render("  "+options))
	}
	return strings.Join(lines, "\n")
}

// tail clips an input string to whatever space the prompt leaves, keeping the
// newest characters visible while typing. The caller must pass the space
// already reduced by the prompt and the bar's own padding, otherwise the two
// together exceed the terminal and the bar wraps.
func (b BottomBar) tail(prompt, input string) string {
	avail := b.contentWidth() - DisplayWidth(prompt)
	if avail < 1 {
		return ""
	}
	if DisplayWidth(input) <= avail {
		return input
	}
	// ansi.TruncateLeft drops cells from the front, so it cannot be used to keep
	// the tail; rightCells bounds the trailing run explicitly.
	return ellipsis + rightCells(input, avail-1)
}

// contentWidth is the space inside the bar's horizontal padding.
func (b BottomBar) contentWidth() int {
	w := b.Width - 4
	if w < 1 {
		return 1
	}
	return w
}

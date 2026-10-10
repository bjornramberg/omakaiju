package ui

import (
	"omakaiju/internal/config"
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
	Progress    *Progress
	Theme       config.Theme
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

	if b.Error != "" {
		return b.line(b.Theme.ErrorText().Render(" Error: " + b.Error))
	}

	if b.Input != "" {
		return b.line(b.Theme.AccentText().Render("> ") + b.tail("> ", b.Input))
	}

	// An active copy takes over the bar so progress is not hidden behind a
	// stale result message.
	if b.Progress != nil {
		return b.Theme.BottomBar().
			Width(b.Width).
			MaxWidth(b.Width).
			Render(b.Progress.Render(b.Width - 4))
	}

	if b.OpResult != "" {
		return b.line(b.Theme.AccentText().Render(" " + b.OpResult))
	}

	help := b.Theme.StatusText().Render("h/j/k/l:nav  y:yank  m:move  p:paste  d:delete  /:filter  q:quit")
	return b.line(help)
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

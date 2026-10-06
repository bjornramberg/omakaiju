package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"omakaiju/internal/config"
	"omakaiju/internal/fuzzy"
)

type FuzzyFinder struct {
	Width      int
	Height     int
	Theme      config.Theme
	Input      string
	Results    []string
	Cursor     int
	AllFiles   []string
}

func NewFuzzyFinder(width, height int, theme config.Theme) FuzzyFinder {
	return FuzzyFinder{Width: width, Height: height, Theme: theme}
}

func (f *FuzzyFinder) Update(input string) {
	f.Input = input
	f.Results = fuzzy.Filter(input, f.AllFiles)
	f.Cursor = 0
}

func (f *FuzzyFinder) MoveUp() {
	if f.Cursor > 0 {
		f.Cursor--
	}
}

func (f *FuzzyFinder) MoveDown() {
	if f.Cursor < len(f.Results)-1 {
		f.Cursor++
	}
}

func (f *FuzzyFinder) Selected() string {
	if f.Cursor < len(f.Results) {
		return f.Results[f.Cursor]
	}
	return ""
}

func (f FuzzyFinder) Render() string {
	maxVisible := f.Height - 6
	if maxVisible < 1 {
		maxVisible = 1
	}

	header := f.Theme.AccentText().Bold(true).Render(" fuzzy find ")
	prompt := f.Theme.AccentText().Render("> ")
	input := f.Theme.Text().Render(f.Input)

	var results []string
	if len(f.Results) == 0 {
		results = append(results, f.Theme.StatusText().Render("no results"))
	} else {
		start := 0
		if f.Cursor >= maxVisible {
			start = f.Cursor - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(f.Results) {
			end = len(f.Results)
		}

		for i := start; i < end; i++ {
			path := f.Results[i]
			displayPath := path
			if len(displayPath) > f.Width-8 {
				displayPath = "..." + displayPath[len(displayPath)-(f.Width-11):]
			}

			if i == f.Cursor {
				results = append(results, f.Theme.FileItemActive().Render(" "+displayPath))
			} else {
				results = append(results, f.Theme.FileItem().Render(" "+displayPath))
			}
		}
	}

	footer := f.Theme.StatusText().Render(fmt.Sprintf("%d results | j/k:nav | enter:select | esc:cancel", len(f.Results)))

	content := header + "\n\n" + prompt + input + "\n\n" + strings.Join(results, "\n") + "\n\n" + footer

	return f.Theme.PreviewPanel().
		Width(f.Width).
		MaxHeight(f.Height).
		Render(content)
}

func (f FuzzyFinder) HighlightMatches(path string) string {
	if f.Input == "" {
		return path
	}

	_, ok := fuzzy.Match(f.Input, filepath.Base(path))
	if !ok {
		return path
	}

	var sb strings.Builder
	pathLower := strings.ToLower(path)
	inputLower := strings.ToLower(f.Input)

	pi := 0
	for i := 0; i < len(path); i++ {
		if pi < len(inputLower) && pathLower[i] == inputLower[pi] {
			sb.WriteString(f.Theme.AccentText().Bold(true).Render(string(path[i])))
			pi++
		} else {
			sb.WriteString(string(path[i]))
		}
	}

	return sb.String()
}

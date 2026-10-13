package fuzzy

import (
	"path/filepath"
	"sort"
	"strings"
)

type Result struct {
	Score     int
	Index     int
	Positions []int
}

func Match(pattern, text string) (Result, bool) {
	pattern = strings.ToLower(pattern)
	text = strings.ToLower(text)

	if pattern == "" {
		return Result{Score: 0, Index: 0}, true
	}

	positions := make([]int, 0, len(pattern))
	score := 0
	prevMatch := -1

	pi := 0
	for ti := 0; ti < len(text) && pi < len(pattern); ti++ {
		if text[ti] == pattern[pi] {
			positions = append(positions, ti)

			charScore := 10

			if prevMatch >= 0 && ti == prevMatch+1 {
				charScore += 15
			}

			if ti == 0 {
				charScore += 10
			} else if ti > 0 && (text[ti-1] == '/' || text[ti-1] == '-' || text[ti-1] == '_' || text[ti-1] == ' ') {
				charScore += 8
			}

			if ti > 0 && text[ti-1] >= 'a' && text[ti-1] <= 'z' && text[ti] >= 'A' && text[ti] <= 'Z' {
				charScore += 5
			}

			if prevMatch >= 0 {
				gap := ti - prevMatch - 1
				charScore -= gap
			}

			score += charScore
			prevMatch = ti
			pi++
		}
	}

	if pi < len(pattern) {
		return Result{}, false
	}

	if len(pattern) == len(text) {
		score += 20
	}

	return Result{Score: score, Index: positions[0], Positions: positions}, true
}

// Filter matches pattern against the basename of each path so typing more
// letters hones in: "con" matches context.md, Reconsider.txt and file.con,
// while "context" matches only context.md. Matching the full absolute path
// instead lets letters scatter across directories, so a fully typed filename
// still matches hundreds of unrelated paths. A pattern containing a separator
// falls back to full-path matching so directory fragments stay searchable.
// There is intentionally no result cap: the finder is allowed to be expensive
// while it runs, and truncating is what made typing look like it did nothing.
func Filter(pattern string, items []string) []string {
	if pattern == "" {
		return items
	}

	patternLower := strings.ToLower(pattern)
	usePath := strings.ContainsRune(patternLower, '/')

	type scored struct {
		item  string
		score int
	}

	matches := make([]scored, 0, len(items))
	for _, item := range items {
		hay := item
		if !usePath {
			hay = filepath.Base(item)
		}
		if score, ok := matchScoreLowered(patternLower, strings.ToLower(hay)); ok {
			matches = append(matches, scored{item, score})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	result := make([]string, len(matches))
	for i, m := range matches {
		result[i] = m.item
	}
	return result
}

// matchScoreLowered is the allocation-free scoring path used by Filter. Both
// inputs must already be lowercased. Unlike Match it keeps no Positions slice,
// which Filter never reads.
func matchScoreLowered(patternLower, textLower string) (int, bool) {
	if patternLower == "" {
		return 0, true
	}

	score := 0
	prevMatch := -1
	firstMatch := -1

	pi := 0
	for ti := 0; ti < len(textLower) && pi < len(patternLower); ti++ {
		if textLower[ti] == patternLower[pi] {
			if firstMatch < 0 {
				firstMatch = ti
			}

			charScore := 10

			if prevMatch >= 0 && ti == prevMatch+1 {
				charScore += 15
			}

			if ti == 0 {
				charScore += 10
			} else if ti > 0 && (textLower[ti-1] == '/' || textLower[ti-1] == '-' || textLower[ti-1] == '_' || textLower[ti-1] == ' ') {
				charScore += 8
			}

			if prevMatch >= 0 {
				gap := ti - prevMatch - 1
				charScore -= gap
			}

			score += charScore
			prevMatch = ti
			pi++
		}
	}

	if pi < len(patternLower) {
		return 0, false
	}

	if len(patternLower) == len(textLower) {
		score += 20
	}

	return score, true
}

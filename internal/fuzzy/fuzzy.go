package fuzzy

import (
	"sort"
	"strings"
)

type Result struct {
	Score     int
	Index     int
	Positions []int
}

// MaxResults caps how many matches Filter returns. The finder only renders
// ~10 rows, so holding tens of thousands of string headers in fuzzyResults
// only pins memory for a count nobody reads.
const MaxResults = 1000

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

func Filter(pattern string, items []string) []string {
	if pattern == "" {
		if len(items) > MaxResults {
			return items[:MaxResults]
		}
		return items
	}

	patternLower := strings.ToLower(pattern)

	type scored struct {
		item  string
		score int
	}

	matches := make([]scored, 0, min(len(items), MaxResults*2))
	for _, item := range items {
		if score, ok := matchScoreLowered(patternLower, strings.ToLower(item)); ok {
			matches = append(matches, scored{item, score})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	if len(matches) > MaxResults {
		matches = matches[:MaxResults]
	}

	result := make([]string, len(matches))
	for i, m := range matches {
		result[i] = m.item
	}
	return result
}

// FilterLowered filters using pre-lowered haystacks so per-keystroke refilters
// don't re-allocate a lowered copy of every path. items and lowered must be
// parallel; pattern is lowered once here.
func FilterLowered(pattern string, items, lowered []string) []string {
	if len(lowered) != len(items) {
		return Filter(pattern, items)
	}
	if pattern == "" {
		if len(items) > MaxResults {
			return items[:MaxResults]
		}
		return items
	}

	patternLower := strings.ToLower(pattern)

	type scored struct {
		item  string
		score int
	}

	matches := make([]scored, 0, min(len(items), MaxResults*2))
	for i, item := range items {
		if score, ok := matchScoreLowered(patternLower, lowered[i]); ok {
			matches = append(matches, scored{item, score})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	if len(matches) > MaxResults {
		matches = matches[:MaxResults]
	}

	result := make([]string, len(matches))
	for i, m := range matches {
		result[i] = m.item
	}
	return result
}

// Lowered returns a parallel slice of lowered strings for use with
// FilterLowered. Computed once at walk time instead of per keystroke.
func Lowered(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = strings.ToLower(s)
	}
	return out
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

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

func Match(pattern, text string) (Result, bool) {
	pattern = strings.ToLower(pattern)
	text = strings.ToLower(text)

	if pattern == "" {
		return Result{Score: 0, Index: 0}, true
	}

	var positions []int
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
		return items
	}

	type scored struct {
		item  string
		score int
	}

	var matches []scored
	for _, item := range items {
		if m, ok := Match(pattern, item); ok {
			matches = append(matches, scored{item, m.Score})
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

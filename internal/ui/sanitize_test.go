package ui

import (
	"strings"
	"testing"
)

func TestSanitizeNameReplacesControlBytes(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
	}{
		"escape sequence": {in: "a\x1b[31mred", want: "a\uFFFD[31mred"},
		"carriage return": {in: "overwrite\r", want: "overwrite\uFFFD"},
		"null byte":       {in: "a\x00b", want: "a\uFFFDb"},
		"bell":            {in: "ding\x07", want: "ding\uFFFD"},
		"delete":          {in: "x\x7fy", want: "x\uFFFDy"},
		"c1 control":      {in: "x\u009by", want: "x\uFFFDy"},
		"invalid utf8":    {in: "a\xffb", want: "a\uFFFDb"},
		"tab":             {in: "a\tb", want: "a b"},
		"keeps text":      {in: "plain ascii", want: "plain ascii"},
		"keeps cjk":       {in: "\u5fbd\u9677\u5b50", want: "\u5fbd\u9677\u5b50"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := SanitizeName(tc.in); got != tc.want {
				t.Errorf("SanitizeName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A tab is one cell in a rendered row, not a cursor instruction, so the strict
// path replaces it with exactly one space.
func TestSanitizeNameReplacesTabWithSingleSpace(t *testing.T) {
	got := SanitizeName("ab\tcd.txt")
	if got != "ab cd.txt" {
		t.Errorf("SanitizeName(%q) = %q, want %q", "ab\tcd.txt", got, "ab cd.txt")
	}
	if strings.ContainsRune(got, '\t') {
		t.Error("a tab survived name sanitisation")
	}
}

// Sanitized names must contain nothing the terminal could act on.
func TestSanitizeNameLeavesNoDangerousRunes(t *testing.T) {
	in := "d8:announce\x1b[2J\x00\r\xff\tend\x1b]0;title\x07"

	for _, r := range SanitizeName(in) {
		if r < 0x20 {
			t.Errorf("control rune %U survived sanitization", r)
		}
		if r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Errorf("dangerous rune %U survived sanitization", r)
		}
	}
}

func TestSanitizeContentLinesLeavesSafeInputUnchanged(t *testing.T) {
	lines := []string{"package main", "func f() {}", ""}

	got := SanitizeContentLines(lines)
	if len(got) != len(lines) {
		t.Fatalf("length changed: %d, want %d", len(got), len(lines))
	}
	for i := range lines {
		if got[i] != lines[i] {
			t.Errorf("line %d changed to %q, want %q", i, got[i], lines[i])
		}
	}
}

// Content is the file's own text, so a tab there is indentation and stays.
// Only the name and path sanitizer turns a tab into a space.
func TestSanitizeContentLinesKeepsTab(t *testing.T) {
	lines := []string{"all:", "\tgo build ./..."}

	got := SanitizeContentLines(lines)
	if got[1] != "\tgo build ./..." {
		t.Errorf("content tab was altered: got %q, want %q", got[1], "\tgo build ./...")
	}
}

func TestSanitizeContentLinesDoesNotMutateInput(t *testing.T) {
	lines := []string{"safe", "bad\x1b[2J"}

	_ = SanitizeContentLines(lines)

	if lines[1] != "bad\x1b[2J" {
		t.Errorf("input was mutated: %q", lines[1])
	}
}

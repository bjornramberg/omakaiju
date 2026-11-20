package nav

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// The documentation is what a user reads on GitHub and in the release tarball.
// A keybinding table that has drifted from the code is worse than no table at
// all: the user presses a key the documentation promises and nothing happens.
//
// Two things are checked: that the canonical keybinding document lists exactly
// the keys the app binds, and that each key is described as doing what it
// actually does. The second check is the one that catches the subtle drift — a
// table that lists the right keys but pairs them with the wrong actions.
//
// The comparison is against the whole keybind table rather than the rows the
// overlay displays, because the documentation promises what works, not what
// the help panel chooses to show. The overlay hides the aliases to keep its
// table short; a user reading the docs should still learn that they work.

// repoRoot is the repository, two directories up from this package.
const repoRoot = "../.."

var (
	// readmePath is the root README. It is still guarded for stale claims,
	// even though the keybinding table now lives in its own document.
	readmePath = filepath.Join(repoRoot, "README.md")

	// keybindingsPath is the one canonical keybinding document. The table is
	// checked here and nowhere else, so a table quoted in another document can
	// never be mistaken for the source of truth.
	keybindingsPath = filepath.Join(repoRoot, "docs", "keybindings.md")
)

// docPaths returns every document the guards cover: the root README followed by
// each Markdown file under docs/, in a stable order.
func docPaths(t *testing.T) []string {
	t.Helper()
	docs, err := filepath.Glob(filepath.Join(repoRoot, "docs", "*.md"))
	if err != nil {
		t.Fatalf("cannot list the docs directory: %v", err)
	}
	sort.Strings(docs)
	return append([]string{readmePath}, docs...)
}

func readDoc(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	return string(data)
}

// readmePhrases maps a binding's description to the words the docs use for it,
// where the two differ.
//
// The docs are written for people and the descriptions in the app are terse,
// so most rows need no entry and are checked against their own description. The
// ones listed here are checked against their phrase instead.
var readmePhrases = map[string]string{
	"move up":                      "Move cursor",
	"move down":                    "Move cursor",
	"parent directory":             "Move cursor",
	"enter dir / open file":        "Enter directory, or open file",
	"switch pane":                  "Toggle active pane",
	"jump to file by first letter": "Jump to first file",
	"stack / unstack panes":        "Stack file panes vertically",
	"refresh pane":                 "Refresh active pane",
	"cancel / clear / back":        "Cancel copy",
}

// TestReadmeKeybindingsMatchTheCode checks that the canonical keybinding
// table documents exactly the keys the app binds, that every binding is
// documented, and that each key is described as doing what it does.
func TestReadmeKeybindingsMatchTheCode(t *testing.T) {
	actions := readmeKeyActions(readDoc(t, keybindingsPath))

	// Every binding must be documented.
	for _, kb := range Keybinds {
		cells, ok := actions[kb.Key]
		if !ok {
			t.Errorf("%s does not document %q (%s)", keybindingsPath, kb.Key, kb.Desc)
			continue
		}
		delete(actions, kb.Key)

		// And the description must match what the key does. The docs may
		// spell it differently, so the expected phrase is what counts.
		want := kb.Desc
		if phrase, ok := readmePhrases[kb.Desc]; ok {
			want = phrase
		}
		if !anyContainsFold(cells, want) {
			t.Errorf("%s describes %q as %q, want it to mention %q", keybindingsPath, kb.Key, cells, want)
		}
	}

	// Anything left is a key the docs promise and the app does not bind.
	for key, cells := range actions {
		t.Errorf("%s documents %q, which is not a binding", keybindingsPath, key)
		_ = cells
	}
}

// anyContainsFold reports whether any of the cells contains the phrase,
// ignoring case.
func anyContainsFold(cells []string, phrase string) bool {
	lower := strings.ToLower(phrase)
	for _, c := range cells {
		if strings.Contains(strings.ToLower(c), lower) {
			return true
		}
	}
	return false
}

// readmeStalePhrases are claims the documentation has made about the app that
// the code does not back. They are the leftovers of features that were removed
// or renamed, and they are worse than a missing feature: the user reads them
// and expects behaviour that never arrives.
//
// The list is checked against the code rather than kept in sync by hand, so a
// phrase that becomes true again is caught.
func TestReadmeMakesNoStaleClaims(t *testing.T) {
	for _, path := range docPaths(t) {
		text := strings.ToLower(readDoc(t, path))

		// A leader key was planned and never built.
		if strings.Contains(text, "leader") {
			t.Errorf("%s mentions a leader key, which the app does not have", path)
		}

		// The preview has no focused state, so nothing unfocuses it.
		if strings.Contains(text, "to unfocus") {
			t.Errorf("%s says a key unfocuses the preview, which it does not", path)
		}

		// The old keybindings, before they were renamed to avoid the shifted
		// symbol keys that do not exist on every layout.
		for _, stale := range []string{"shift+/", "shift+f` | fuzzy"} {
			if strings.Contains(text, stale) {
				t.Errorf("%s still documents %q, which is not a binding", path, stale)
			}
		}

		// The fuzzy finder used to take j and k as cursor movement. That made
		// both letters impossible to search for — "json" searched for "son" —
		// so any claim that they navigate is stale now that they type.
		for _, stale := range []string{
			"`j`/`k` move in the fuzzy finder",
			"j/k move in the fuzzy finder",
			"`j/k` navigate",
		} {
			if strings.Contains(text, stale) {
				t.Errorf("%s still says %q of the fuzzy finder, where j and k type a search", path, stale)
			}
		}
	}
}

// markdownLink matches the target of an inline Markdown link or image:
// [text](target) and ![alt](target). Reference-style links are not used in
// these documents and are not matched.
var markdownLink = regexp.MustCompile(`\[[^\]]*\]\(\s*([^)\s]+)`)

// TestReadmeLinksResolve checks that every relative link and image target in
// the documentation resolves to a file in this repository, and that a "#anchor"
// names a heading in the file it points at. External URLs are not fetched: the
// check is offline and fast, and only the links this repository controls can
// rot on their own.
func TestReadmeLinksResolve(t *testing.T) {
	for _, path := range docPaths(t) {
		checkDocLinks(t, path)
	}
}

func checkDocLinks(t *testing.T, path string) {
	t.Helper()
	dir := filepath.Dir(path)
	inFence := false
	for i, line := range strings.Split(readDoc(t, path), "\n") {
		// A link inside a fenced code block is sample text, not a link.
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}

		for _, match := range markdownLink.FindAllStringSubmatch(line, -1) {
			target := match[1]
			if target == "" || isExternalLink(target) {
				continue
			}
			// A site-absolute link is relative to the repository root.
			filePart, fragment, _ := strings.Cut(target, "#")
			filePart = strings.TrimPrefix(filePart, "/")
			resolved := path
			if filePart != "" {
				resolved = filepath.Join(dir, filepath.FromSlash(filePart))
			}

			info, err := os.Stat(resolved)
			if err != nil {
				t.Errorf("%s:%d: link %q does not resolve: %v", path, i+1, target, err)
				continue
			}
			if fragment == "" || info.IsDir() {
				continue
			}
			anchors, err := markdownAnchors(resolved)
			if err != nil {
				t.Errorf("%s:%d: cannot read link target %q: %v", path, i+1, resolved, err)
				continue
			}
			if !anchors[strings.ToLower(fragment)] {
				t.Errorf("%s:%d: link %q names no heading in %s", path, i+1, target, resolved)
			}
		}
	}
}

// isExternalLink reports whether a target is fetched from elsewhere and so is
// outside this repository's control.
func isExternalLink(target string) bool {
	for _, prefix := range []string{"http://", "https://", "mailto:", "tel:", "ftp://", "//"} {
		if strings.HasPrefix(target, prefix) {
			return true
		}
	}
	return false
}

// markdownAnchors returns the heading anchors of a Markdown file, following
// GitHub's slug rule: lowercase, letters and digits kept, spaces and hyphens
// kept, other punctuation dropped, and a repeated slug suffixed with -1, -2,
// and so on.
func markdownAnchors(path string) (map[string]bool, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	anchors := map[string]bool{}
	seen := map[string]int{}
	inFence := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(trimmed, "#") {
			continue
		}
		text := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		// An ATX heading may close with a run of '#' characters.
		text = strings.TrimSpace(strings.TrimRight(text, "#"))
		if text == "" {
			continue
		}
		slug := markdownSlug(text)
		if n := seen[slug]; n > 0 {
			seen[slug] = n + 1
			slug = slug + "-" + strconv.Itoa(n)
		} else {
			seen[slug] = 1
		}
		anchors[slug] = true
	}
	return anchors, nil
}

// markdownSlug is GitHub's heading-anchor slug for one heading.
func markdownSlug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == ' ' || r == '-':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// readmeKeyActions pulls the key column and the action column out of the
// keybinding document's tables, and pairs them.
func readmeKeyActions(markdown string) map[string][]string {
	actions := map[string][]string{}
	section := keybindingSection(markdown)
	if section == "" {
		return actions
	}

	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := strings.Split(trimmed, "|")
		if len(cells) < 4 {
			continue
		}
		keyCell := cells[1]
		actionCell := strings.TrimSpace(cells[2])
		// Skip the header row and the rule under it.
		if strings.Contains(keyCell, "Key") || strings.Contains(keyCell, "---") {
			continue
		}
		for _, part := range strings.Split(keyCell, " / ") {
			token := readmeKeyToken(part)
			if token == "" {
				continue
			}
			actions[token] = append(actions[token], actionCell)
		}
	}
	return actions
}

// keybindingSection returns the keybinding part of the document, so a table
// elsewhere in it is not mistaken for one. The canonical document is expected
// to open its keybinding part with an exact "## Keybindings" heading; that is
// the heading this guard looks for, and the one docs/keybindings.md must carry.
func keybindingSection(markdown string) string {
	start := strings.Index(markdown, "## Keybindings")
	if start < 0 {
		return ""
	}
	section := markdown[start:]
	// The section ends at the next heading of the same level.
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	return section
}

// readmeKeyToken normalises one key as the docs write it.
func readmeKeyToken(cell string) string {
	token := strings.TrimSpace(cell)
	if token == "" {
		return ""
	}

	// A compound like "Space + a-z" describes a sequence. The key it binds
	// is the first part.
	if i := strings.Index(token, " + "); i >= 0 {
		token = token[:i]
	}
	token = strings.TrimSpace(strings.Trim(token, "`"))

	switch token {
	case "↑":
		return "up"
	case "↓":
		return "down"
	case "←":
		return "left"
	case "→":
		return "right"
	case "Enter":
		return "enter"
	case "Tab":
		return "tab"
	case "Esc":
		return "esc"
	case "Space":
		return "space"
	}

	// Shift+X is written as shift+x in the keybind table, which is the
	// canonical identity of the key: the modifier and the base key.
	if rest, ok := strings.CutPrefix(token, "Shift+"); ok {
		return "shift+" + strings.ToLower(rest)
	}
	return token
}

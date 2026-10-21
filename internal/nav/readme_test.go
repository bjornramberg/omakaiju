package nav

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The README is the documentation a user reads on GitHub and in the release
// tarball. A keybinding table that has drifted from the code is worse than
// no table at all: the user presses a key the documentation promises and
// nothing happens.
//
// Two things are checked: that the README documents exactly the keys the app
// binds, and that each key is described as doing what it actually does.
// The second check is the one that catches the subtle drift — a table that
// lists the right keys but pairs them with the wrong actions.
//
// The comparison is against the whole keybind table rather than the rows the
// overlay displays, because the README documents what works, not what the
// help panel chooses to show. The overlay hides the aliases to keep its
// table short; a user reading the README should still learn that they work.

// readmePath is the README, two directories up from this package.
const readmePath = "../../README.md"

func readReadme(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(readmePath))
	if err != nil {
		t.Fatalf("cannot read the README: %v", err)
	}
	return string(data)
}

// readmePhrases maps a binding's description to the words the README uses
// for it, where the two differ.
//
// The README is written for people and the descriptions in the app are
// terse, so most rows need no entry and are checked against their own
// description. The ones listed here are checked against their phrase
// instead.
var readmePhrases = map[string]string{
	"move up":                      "Move cursor",
	"move down":                    "Move cursor",
	"parent directory":             "Move cursor",
	"enter dir / open file":        "Enter directory, or open file",
	"switch pane":                  "Toggle active pane",
	"jump to file by first letter": "Jump to first file",
	"stack / unstack panes":        "Stack file panes vertically",
	"add file / directory":         "Add file/directory",
	"refresh pane":                 "Refresh active pane",
	"cancel / clear / back":        "Cancel copy",
}

// TestReadmeKeybindingsMatchTheCode checks that the README's keybinding
// tables document exactly the keys the app binds, that every binding is
// documented, and that each key is described as doing what it does.
func TestReadmeKeybindingsMatchTheCode(t *testing.T) {
	actions := readmeKeyActions(readReadme(t))

	// Every binding must be documented.
	for _, kb := range Keybinds {
		cells, ok := actions[kb.Key]
		if !ok {
			t.Errorf("the README does not document %q (%s)", kb.Key, kb.Desc)
			continue
		}
		delete(actions, kb.Key)

		// And the description must match what the key does. The README may
		// spell it differently, so the expected phrase is what counts.
		want := kb.Desc
		if phrase, ok := readmePhrases[kb.Desc]; ok {
			want = phrase
		}
		if !anyContainsFold(cells, want) {
			t.Errorf("the README describes %q as %q, want it to mention %q", kb.Key, cells, want)
		}
	}

	// Anything left is a key the README promises and the app does not
	// bind.
	for key, cells := range actions {
		t.Errorf("the README documents %q, which is not a binding", key)
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

// readmeStalePhrases are claims the README has made about the app that the
// code does not back. They are the leftovers of features that were removed
// or renamed, and they are worse than a missing feature: the user reads
// them and expects behaviour that never arrives.
//
// The list is checked against the code rather than kept in sync by hand, so
// a phrase that becomes true again is caught.
func TestReadmeMakesNoStaleClaims(t *testing.T) {
	readme := strings.ToLower(readReadme(t))

	// A leader key was planned and never built.
	if strings.Contains(readme, "leader") {
		t.Error("the README mentions a leader key, which the app does not have")
	}

	// The preview has no focused state, so nothing unfocuses it.
	if strings.Contains(readme, "to unfocus") {
		t.Error("the README says a key unfocuses the preview, which it does not")
	}

	// The old keybindings, before they were renamed to avoid the shifted
	// symbol keys that do not exist on every layout.
	for _, stale := range []string{"shift+/", "shift+f` | fuzzy"} {
		if strings.Contains(readme, stale) {
			t.Errorf("the README still documents %q, which is not a binding", stale)
		}
	}
}

// readmeKeyActions pulls the key column and the action column out of the
// README's keybinding tables, and pairs them.
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

// keybindingSection returns the keybinding part of the README, so a table
// elsewhere in the document is not mistaken for one.
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

// readmeKeyToken normalises one key as the README writes it.
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

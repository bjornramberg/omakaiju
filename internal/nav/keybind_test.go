package nav

import (
	"strings"
	"testing"
)

// Every binding must carry a description, otherwise the key-list overlay
// silently renders an unexplained key.
func TestKeybindsAllHaveDescriptions(t *testing.T) {
	for _, kb := range Keybinds {
		if strings.TrimSpace(kb.Desc) == "" {
			t.Errorf("key %q (action %d) has no description", kb.Key, kb.Action)
		}
	}
}

func TestLookup(t *testing.T) {
	if got := Lookup("shift+k"); got != ActionKeyList {
		t.Errorf("Lookup(shift+k) = %d, want ActionKeyList", got)
	}
	if got := Lookup("shift+f"); got != ActionFilter {
		t.Errorf("Lookup(shift+f) = %d, want ActionFilter", got)
	}
	if got := Lookup("shift+p"); got != ActionFuzzyFind {
		t.Errorf("Lookup(shift+p) = %d, want ActionFuzzyFind", got)
	}
	if got := Lookup("P"); got != ActionFuzzyFind {
		t.Errorf("Lookup(P) = %d, want ActionFuzzyFind (uppercase alias)", got)
	}
	if got := Lookup("shift+g"); got != ActionGoto {
		t.Errorf("Lookup(shift+g) = %d, want ActionGoto", got)
	}
	if got := Lookup("G"); got != ActionGoto {
		t.Errorf("Lookup(G) = %d, want ActionGoto (uppercase alias)", got)
	}
	if got := Lookup("shift+c"); got != ActionCopy {
		t.Errorf("Lookup(shift+c) = %d, want ActionCopy", got)
	}
	if got := Lookup("shift+f"); got != ActionFilter {
		t.Errorf("Lookup(shift+f) = %d, want ActionFilter", got)
	}
	if got := Lookup("K"); got != ActionKeyList {
		t.Errorf("Lookup(K) = %d, want ActionKeyList (uppercase alias)", got)
	}
	if got := Lookup("definitely-not-a-key"); got != ActionNone {
		t.Errorf("unknown key should fall back to ActionNone, got %d", got)
	}
}

// Equivalent keys for the same action collapse into one row with the aliases
// merged, so the overlay reads as a table rather than a duplicate list.
func TestKeyRowsMergeAliases(t *testing.T) {
	rows := KeyRows()

	byDesc := map[string]string{}
	for _, row := range rows {
		byDesc[row.Desc] = row.Keys
	}
	for desc, keys := range map[string]string{
		"move up":                      "up",
		"move down":                    "down",
		"parent directory":             "left",
		"enter directory":              "right",
		"enter dir / open file":        "enter",
		"switch pane":                  "tab",
		"jump to file by first letter": "space+key",
		"copy (yank)":                  "shift+c",
		"paste / move marked":          "shift+v",
		"delete":                       "shift+d",
		"mark / unmark":                "shift+m",
		"add file / directory":         "shift+a",
		"rename":                       "shift+r",
		"stack / unstack panes":        "shift+s",
		"cycle sort order":             "shift+o",
		"toggle preview":               "shift+i",
		"fuzzy find":                   "shift+p",
		"filter pane":                  "shift+f",
		"goto path":                    "shift+g",
		"refresh pane":                 "shift+e",
		"key list overlay":             "shift+k",
		"quit":                         "shift+q",
		"cancel / clear / back":        "esc",
	} {
		if byDesc[desc] != keys {
			t.Errorf("desc %q should merge into %q, got %q", desc, keys, byDesc[desc])
		}
	}

	// First-seen order: navigation first, then jump, file ops, pane view, system
	if len(rows) > 0 && rows[0].Keys != "up" {
		t.Errorf("unexpected opening row %q", rows[0].Keys)
	}
	if last := rows[len(rows)-1]; !strings.Contains(last.Keys, "esc") {
		t.Errorf("esc binding should be last, got %q", last.Keys)
	}
}

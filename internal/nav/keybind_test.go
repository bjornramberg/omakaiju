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
	if got := Lookup("ctrl+k"); got != ActionKeyList {
		t.Errorf("Lookup(ctrl+k) = %d, want ActionKeyList", got)
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
		"up":           "k/up",
		"down":         "j/down",
		"out / back":   "h/left",
		"in / forward": "l/right",
	} {
		if byDesc[desc] != keys {
			t.Errorf("desc %q should merge into %q, got %q", desc, keys, byDesc[desc])
		}
	}

	// First-seen order: k comes before j, and the list itself is last.
	if rows[0].Keys != "k/up" || rows[1].Keys != "j/down" {
		t.Errorf("unexpected opening rows %q, %q", rows[0].Keys, rows[1].Keys)
	}
	if last := rows[len(rows)-1]; last.Keys != "ctrl+k" {
		t.Errorf("key-list binding should be last, got %q", last.Keys)
	}
}

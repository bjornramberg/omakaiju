package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

// spaceModel builds a model over a directory whose entries contain spaces, so
// the text paths have something realistic to filter and type.
func spaceModel(t *testing.T) (Model, string) {
	t.Helper()

	dir := t.TempDir()
	for _, name := range []string{"my file.txt", "other.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "nonexistent.toml")})
	m.width, m.height = 120, 40
	m.leftPath, m.leftFiles = dir, files
	m.rightPath, m.rightFiles = dir, files
	return m, dir
}

// The space bar is a printable character, not a named key: every text prompt
// has to receive it. Bubble Tea delivers it as Text " " with a "space"
// keystroke, and the prompt used to test only the keystroke string.
func TestSpaceKeyTypesIntoPrompt(t *testing.T) {
	modes := []struct {
		name string
		mode inputMode
	}{
		{"rename", inputRename},
		{"filter", inputFilter},
		{"add", inputAdd},
		{"goto", inputGoto},
	}
	for _, tc := range modes {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := spaceModel(t)
			m.mode = tc.mode
			m.inputBuf = ""

			updated, _ := m.Update(space())
			m = updated.(Model)

			if m.inputBuf != " " {
				t.Errorf("inputBuf = %q, want a single space", m.inputBuf)
			}
		})
	}
}

// The fuzzy finder takes free text too, so the space must reach its query.
func TestSpaceKeyTypesIntoFuzzyFinder(t *testing.T) {
	m, _ := spaceModel(t)
	m.fuzzyActive = true
	m.fuzzyInput = ""

	updated, _ := m.Update(space())
	m = updated.(Model)

	if m.fuzzyInput != " " {
		t.Errorf("fuzzyInput = %q, want a single space", m.fuzzyInput)
	}
}

// Same for the key-list search box.
func TestSpaceKeyTypesIntoKeyList(t *testing.T) {
	m, _ := spaceModel(t)
	m.keyListActive = true
	m.keyListQuery = ""

	updated, _ := m.Update(space())
	m = updated.(Model)

	if m.keyListQuery != " " {
		t.Errorf("keyListQuery = %q, want a single space", m.keyListQuery)
	}
}

// A filter with a space in it has to actually narrow the listing: typing
// "my file" must match "my file.txt" and nothing else.
func TestFilterPromptMatchesNameWithSpace(t *testing.T) {
	m, _ := spaceModel(t)

	updated, _ := m.Update(shiftKey('f'))
	m = updated.(Model)
	if m.mode != inputFilter {
		t.Fatalf("mode = %v, want the filter prompt open", m.mode)
	}

	for _, r := range "my file" {
		if r == ' ' {
			updated, _ = m.Update(space())
		} else {
			updated, _ = m.Update(key(r))
		}
		m = updated.(Model)
	}

	if m.inputBuf != "my file" {
		t.Fatalf("inputBuf = %q, want %q", m.inputBuf, "my file")
	}
	if m.leftFilter != "my file" {
		t.Fatalf("leftFilter = %q, want %q", m.leftFilter, "my file")
	}

	files := m.visibleFiles(0)
	if len(files) != 1 || files[0].Name != "my file.txt" {
		names := make([]string, len(files))
		for i, f := range files {
			names[i] = f.Name
		}
		t.Fatalf("filter matched %v, want just [my file.txt]", names)
	}
}

// Backspace must take the space back off without touching the rest of the
// buffer.
func TestBackspaceAfterSpaceRemovesOnlyTheSpace(t *testing.T) {
	m, _ := spaceModel(t)
	m.mode = inputRename
	m.inputBuf = "my file"

	updated, _ := m.Update(space())
	m = updated.(Model)
	if m.inputBuf != "my file " {
		t.Fatalf("inputBuf = %q, want %q", m.inputBuf, "my file ")
	}

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	m = updated.(Model)

	if m.inputBuf != "my file" {
		t.Errorf("inputBuf = %q after backspace, want %q", m.inputBuf, "my file")
	}
}

// Keys that carry no text must keep inserting nothing, in every text context.
func TestNonTextKeysDoNotInsertSpace(t *testing.T) {
	keys := []struct {
		name string
		msg  tea.KeyMsg
	}{
		{"enter", tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})},
		{"backspace", tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace})},
		{"escape", tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})},
		{"up", arrow(tea.KeyUp)},
		{"down", arrow(tea.KeyDown)},
		{"left", arrow(tea.KeyLeft)},
		{"right", arrow(tea.KeyRight)},
	}

	contexts := []struct {
		name  string
		setup func(*Model)
		get   func(Model) string
	}{
		{
			name:  "prompt",
			setup: func(m *Model) { m.mode = inputRename; m.inputBuf = "x" },
			get:   func(m Model) string { return m.inputBuf },
		},
		{
			name:  "fuzzy",
			setup: func(m *Model) { m.fuzzyActive = true; m.fuzzyInput = "x" },
			get:   func(m Model) string { return m.fuzzyInput },
		},
		{
			name:  "key list",
			setup: func(m *Model) { m.keyListActive = true; m.keyListQuery = "x" },
			get:   func(m Model) string { return m.keyListQuery },
		},
	}

	for _, ctx := range contexts {
		for _, k := range keys {
			t.Run(ctx.name+"/"+k.name, func(t *testing.T) {
				m, _ := spaceModel(t)
				ctx.setup(&m)

				updated, _ := m.Update(k.msg)
				m = updated.(Model)

				got := ctx.get(m)
				if strings.Contains(got, " ") {
					t.Errorf("query = %q, key %s inserted a space", got, k.name)
				}
				if len(got) > 1 {
					t.Errorf("query = %q, key %s inserted text", got, k.name)
				}
			})
		}
	}
}

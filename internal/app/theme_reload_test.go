package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"omakaiju/internal/config"

	tea "charm.land/bubbletea/v2"
)

func themeFixture(t *testing.T) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	colors := filepath.Join(dir, "colors.toml")
	if err := os.WriteFile(colors, []byte("background = \"#101010\"\naccent = \"#222222\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		ThemePath:       colors,
		ThemeWatchDirs:  []string{dir},
		OmarchyStateDir: filepath.Join(dir, "state"),
	}
	m := NewModel(cfg)
	if m.themeWatcher == nil {
		t.Fatal("model should own a theme watcher")
	}
	t.Cleanup(func() { _ = m.themeWatcher.Close() })
	return m, colors
}

// awaitReload runs the waiting command the way the runtime would: it blocks
// until the watcher reports an event.
func awaitReload(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command waiting on the theme watcher")
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()

	select {
	case msg := <-ch:
		tr, ok := msg.(themeReloadedMsg)
		if !ok {
			t.Fatalf("expected themeReloadedMsg, got %T", msg)
		}
		if tr.err != nil {
			t.Fatalf("reload reported an error: %v", tr.err)
		}
		return tr
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a theme reload")
		return nil
	}
}

// Regression: the watcher used to return from inside its event loop, so only
// the first theme change ever took effect and the second switch did nothing.
func TestThemeHotReloadSurvivesRepeatedChanges(t *testing.T) {
	m, colors := themeFixture(t)

	cmd := m.waitThemeCmd()
	for i, want := range []string{"#0a0a0a", "#0b0b0b", "#0c0c0c", "#0d0d0d"} {
		body := "background = \"" + want + "\"\naccent = \"#222222\"\n"
		if err := os.WriteFile(colors, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}

		msg := awaitReload(t, cmd)
		updated, next := m.Update(msg)
		m = updated.(Model)
		if m.theme.BGDark != want {
			t.Fatalf("round %d: BGDark = %q, want %q", i, m.theme.BGDark, want)
		}

		// The runtime must receive a fresh command or listening stops here.
		cmd = next
		if cmd == nil {
			t.Fatalf("round %d: Update did not re-arm the theme watcher", i)
		}
	}
}

// The model has to own the watcher, otherwise closing it on quit is a no-op and
// the inotify descriptor leaks.
func TestThemeWatcherIsOwnedAndClosable(t *testing.T) {
	m, _ := themeFixture(t)
	if m.themeWatcher == nil {
		t.Fatal("watcher not stored on the model")
	}
	if err := m.themeWatcher.Close(); err != nil {
		t.Errorf("closing the watcher failed: %v", err)
	}
	// A closed watcher must report rather than hang forever.
	cmd := m.waitThemeCmd()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Error("waiting on a closed watcher never returned")
	}
}

func TestRelevantThemeEvent(t *testing.T) {
	state := "/home/u/.local/state/omarchy"
	cases := []struct {
		name string
		want bool
	}{
		{"/home/u/.local/state/omarchy/current/theme/colors.toml", true},
		{"/tmp/whatever/colors.toml", true},
		{state + "/current", true},
		{"/tmp/some-dir/", false},
		{"", false},
	}
	for _, c := range cases {
		if got := relevantThemeEvent(c.name, "/home/u/.local/state/omarchy/current/theme/colors.toml", state); got != c.want {
			t.Errorf("relevantThemeEvent(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

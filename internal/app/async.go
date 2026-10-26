package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

func loadFuzzyFilesCmd(root string) tea.Cmd {
	return safeCmd("fuzzy-walk", func() tea.Msg {
		result, _ := fs.WalkDir(root)
		return fuzzyFilesLoadedMsg{files: result.Files, truncated: result.Truncated, root: root}
	})
}

// fuzzyTickCmd advances the loading spinner. The loop stops on its own: a tick
// that arrives once the walk has landed, the finder has been closed, or the
// root has moved on is simply dropped rather than reissued.
func fuzzyTickCmd() tea.Cmd {
	return safeCmd("fuzzy-tick", tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return fuzzyTickMsg{}
	}))
}

// splashTickCmd advances the version splash one frame. Update re-issues it
// until the splash has fully faded out, then lets the loop end so no ticker
// outlives the animation.
func splashTickCmd() tea.Cmd {
	return safeCmd("splash-tick", tea.Tick(ui.SplashTick, func(time.Time) tea.Msg {
		return splashTickMsg{}
	}))
}

// waitThemeCmd blocks until the next palette-relevant fsnotify event and
// reports it. It is deliberately one-shot: Update re-issues it after handling
// each message, so the watcher survives every reload instead of stopping after
// the first one.
func (m Model) waitThemeCmd() tea.Cmd {
	watcher := m.themeWatcher
	if watcher == nil {
		return nil
	}
	path := m.cfg.ThemePath
	omarchyPath := m.cfg.OmarchyStateDir

	return safeCmd("theme-watch", func() tea.Msg {
		for {
			select {
			case event, ok := <-watcher.Events():
				if !ok {
					return themeReloadedMsg{err: fmt.Errorf("theme watcher closed"), closed: true}
				}
				if !relevantThemeEvent(event.Name, path, omarchyPath) {
					continue
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Chmod) {
					// Editors and the theme engine write in bursts, so let the
					// write settle before reading.
					time.Sleep(100 * time.Millisecond)
					theme, err := config.LoadTheme(path)
					if err != nil {
						// A theme switch briefly swaps the symlink; the next
						// event will carry the final file.
						continue
					}
					return themeReloadedMsg{theme: theme}
				}

			case err, ok := <-watcher.Errors():
				if !ok {
					// The watcher was closed, which also closes the events
					// channel. Returning here ends the wait cleanly instead
					// of spinning on a closed channel.
					return themeReloadedMsg{err: fmt.Errorf("theme watcher closed"), closed: true}
				}
				if err != nil {
					// The watcher is still alive, so this is a transient
					// failure. Update reports it and arms the wait again;
					// returning here without re-arming is what used to stop
					// theme switches from ever being noticed again.
					return themeReloadedMsg{err: err}
				}
			}
		}
	})
}

// relevantThemeEvent reports whether an fsnotify event concerns the palette.
// A theme switch re-points the omarchy symlink rather than rewriting the file,
// so events under the state directory count too.
func relevantThemeEvent(name, themePath, omarchyStateDir string) bool {
	if name == "" {
		return false
	}
	if name == themePath {
		return true
	}
	if strings.HasSuffix(name, "colors.toml") {
		return true
	}
	return omarchyStateDir != "" && strings.HasPrefix(name, omarchyStateDir)
}

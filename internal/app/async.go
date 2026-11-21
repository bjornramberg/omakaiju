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

// debugTickCmd schedules the next debug-pane sample. It is one-shot on
// purpose: Update re-issues it only while the pane is open, so closing the pane
// leaves no ticker, goroutine or sampler behind.
func debugTickCmd() tea.Cmd {
	return safeCmd("debug-tick", tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return debugTickMsg{}
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

// dirCoalesceWindow is how long the directory wait lingers after the first
// event before reporting it. Filesystem bursts — an untar, a download
// landing, a checkout — raise one event per file, and reporting each one
// would dispatch a pane reload per file: hundreds of concurrent listings,
// each pinning its own goroutine and, on a slow mount, its own OS thread,
// while the event loop starves behind them. Lingering collapses the whole
// burst into the single reload that actually matters, at the cost of
// delaying an isolated external change by this window.
const dirCoalesceWindow = 200 * time.Millisecond

// waitDirCmd blocks until something changes under a watched pane directory
// and reports it. It is one-shot like the theme wait: Update re-arms it after
// every message, so there is exactly one wait reading the watcher at a time.
//
// Bursts are coalesced: after the first meaningful event the wait lingers for
// dirCoalesceWindow, absorbing whatever else arrives, and reports only the
// last path. A sustained torrent therefore yields one message per window —
// a bounded handful of concurrent listings — no matter how many files changed.
//
// The path is reported rather than a pane or a listing: a directory can change
// while the pane showing it is still loading somewhere else, and the decision
// of whether the pane still cares belongs to the model that receives it, not
// to a command that captured the model when it started.
func (m Model) waitDirCmd() tea.Cmd {
	watcher := m.dirWatcher
	if watcher == nil {
		return nil
	}
	return safeCmd("dir-watch", func() tea.Msg {
		var path string
		var deadline <-chan time.Time
		for {
			select {
			case event, ok := <-watcher.Events():
				if !ok {
					return dirChangedMsg{closed: true}
				}
				if event.Name == "" {
					continue
				}
				// A large copy writes its destination through a scratch
				// file, one chunk per step, and every chunk raises a
				// filesystem event. Reporting those would dispatch a
				// pane reload and a preview refresh per chunk — thousands
				// of concurrent listings for a file that is not even
				// finished — while the final rename raises the one event
				// that actually matters: the destination appearing under
				// its real name. Scratch writes are therefore skipped
				// here, and the wait continues for a meaningful change.
				if fs.IsScratchName(event.Name) {
					continue
				}
				path = event.Name
				if deadline == nil {
					deadline = time.After(dirCoalesceWindow)
				}
			case err, ok := <-watcher.Errors():
				if !ok {
					return dirChangedMsg{closed: true}
				}
				if err != nil {
					return dirChangedMsg{err: err}
				}
			case <-deadline:
				return dirChangedMsg{path: path}
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

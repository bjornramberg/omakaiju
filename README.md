# omakaiju

![CI](https://github.com/bjornramberg/omakaiju/actions/workflows/ci.yml/badge.svg)
![Release](https://img.shields.io/github/v/release/bjornramberg/omakaiju)

A modern, ultra-lightweight, dual-pane TUI file manager for Unix-like systems.
Built in Go with Bubble Tea + Lipgloss. Both panes start in the directory where you launch it.

## Features

- Dual-pane navigation, dirs-first sorting, Vim-style (`h/j/k/l`) and arrow keys, per-pane filtering
- Dynamic Omarchy theme integration with hot-reloading
- Nerd Font icons in panes and archive listings
- Text previews with Chroma syntax highlighting, hex dump for binaries, entry listing for zip/tar/tgz (images show metadata only, no pixel rendering)
- Fuzzy finder (recursive, ranked) plus live `/` filter
- Yank/paste with progress bar and cancel, mark/move across panes, trash (XDG) or force delete, add/rename
- Asynchronous I/O so the UI never blocks on filesystem work

## Requirements

- Go 1.27+
- A Nerd Font (optional, for icons)

## Install

```bash
go install github.com/bjornramberg/omakaiju/cmd/omakaiju@latest
```

Or download a prebuilt Linux (amd64/arm64) tarball from
[GitHub Releases](https://github.com/bjornramberg/omakaiju/releases), then:

```bash
tar xzf omakaiju_Linux_x86_64.tar.gz
./omakaiju
```

## Use as the Omarchy file manager

omakaiju is a drop-in replacement for Omarchy's default file manager binding.
These steps target **Omarchy Quattro** (Hyprland, Lua-based config).

**1. Make sure `omakaiju` is on your `PATH`.** If you installed with
`go install`, the binary lands in `~/go/bin`; symlink it into `~/.local/bin`
so both your shell and Hyprland can find it:

```bash
mkdir -p ~/.local/bin
ln -sf "$(go env GOPATH)/bin/omakaiju" ~/.local/bin/omakaiju
```

**2. Rebind `SUPER + SHIFT + F` in `~/.config/hypr/bindings.lua`.** The default
launches nautilus, so unbind it first:

```lua
hl.unbind("SUPER + SHIFT + F")
o.bind("SUPER + SHIFT + F", "File manager", "omarchy launch tui omakaiju")
```

`omarchy launch tui` starts omakaiju in a new terminal with Omarchy styling.
Hyprland auto-reloads the config on save; validate with `hyprctl reload`
followed by `hyprctl configerrors`.

**3. Keep the Omarchy window bindings intact.** If you previously unbound
`SUPER + CTRL + L` (Lock) or `SUPER + CTRL + F` (Tiled full screen) — a common
workaround when TUIs steal `Ctrl+F` — delete those `hl.unbind` lines so both
keys fall back to the Omarchy defaults:

```lua
-- Remove these; Omarchy's defaults are enough:
-- hl.unbind("SUPER + CTRL + L")
-- hl.unbind("SUPER + CTRL + F")
```

Inside omakaiju, `Ctrl+f` is the fuzzy finder and only applies while omakaiju
has focus, so it no longer conflicts with the Hyprland binding.

Check the result any time with:

```bash
omarchy menu keybindings --print
```

To revert everything, remove the two lines from step 2 and
`omarchy refresh hyprland`.

## Building

```bash
make build    # go build -o omakaiju ./cmd/omakaiju
make run      # build + ./omakaiju
make install  # install to /usr/local/bin (override with PREFIX=/path)
make test     # go test ./...
make lint     # go vet ./...
make clean    # rm -f omakaiju
```

## Keybindings

| Key | Action |
|-----|--------|
| h/j/k/l or arrows | Navigate (cursor restores to exited dirname on `h`) |
| Enter | Enter directory, or open file with the desktop handler (`gio open`, `xdg-open` fallback) |
| Right / l | Enter directory only (never opens files) |
| Tab | Toggle active pane |
| s | Stack file panes vertically / restore side by side |
| v | Hide / show the file preview pane (width goes back to the file panes) |
| i | Focus preview (press `i`/`Esc` to unfocus; `i` also restores a hidden preview) |
| y | Yank (copy) entry under cursor |
| m | Mark / unmark entry (repeat to select several) |
| p | Move marked entries to the other pane, else paste yanked entry |
| d | Delete prompt: `t`rash (default, also `y`/`Enter`), `f`orce delete, `n`/`Esc` cancel |
| a | Add file/directory (trailing `/` creates a directory) |
| r | Rename |
| / | Filter (per-pane, arrows keep the prompt open) |
| o | Cycle sort (per-pane): name → size↓ largest first → size↑ smallest first → name (dirs stay first) |
| Ctrl+f | Fuzzy find (`j/k` navigate, type/backspace refines, `Enter` jumps, `Esc` exits) |
| Ctrl+r | Refresh active pane |
| Esc | Context: cancel copy / dismiss prompt / clear filter |
| q | Quit |

Long copies show a progress bar; `Esc` cancels and removes only the partial file.

## Configuration

omakaiju reads its palette from Omarchy's active theme at
`~/.local/state/omarchy/current/theme/colors.toml` — the same file Omarchy's own
`omarchy-theme-color` helper resolves, so colours match the rest of the desktop.
Switching themes is hot-reloaded; you can also point `OMARCHY_COLORS` at a
different `colors.toml` to override it.

Older setups that render a per-application `~/.config/omarchy/fm.toml` are used
only if the primary file is missing. If neither is readable, built-in defaults are used.

## License

MIT

# omakaiju

A modern, ultra-lightweight, dual-pane TUI file manager for Unix-like systems.

## Features

- Dual-pane file navigation with Vim-style keybindings
- Dynamic Omarchy theme integration with hot-reloading
- Nerd Font icon support
- File previews (text, binary hex, archives, images)
- Fuzzy finding
- Asynchronous I/O for non-blocking operation

## Building

```bash
make build
```

## Running

```bash
./omakaiju
```

## Keybindings

| Key | Action |
|-----|--------|
| h/j/k/l or arrows | Navigate |
| Enter | Open directory / file |
| Tab | Toggle active pane |
| y | Yank (copy) |
| m | Mark / unmark entry (press repeatedly to select several) |
| p | Move marked entries to the other pane, or paste the yanked file |
| Esc | Cancel an in-flight copy (removes the partial file) |
| d | Delete: `t`rash (default), `f`orce delete, `n`o |
| a | Add file/directory |
| r | Rename |
| / | Filter |
| Ctrl+f | Fuzzy find |
| Ctrl+r | Refresh |
| Esc | Clear/dismiss |
| q | Quit |

## Configuration

omakaiju reads its palette from Omarchy's active theme at
`~/.local/state/omarchy/current/theme/colors.toml` — the same file Omarchy's own
`omarchy-theme-color` helper resolves, so colours match the rest of the desktop.
Switching themes is hot-reloaded; you can also point `OMARCHY_COLORS` at a
different `colors.toml` to override it.

Older setups that render a per-application `~/.config/omarchy/fm.toml` are still
supported as a fallback. If neither is readable, built-in defaults are used.

## License

MIT

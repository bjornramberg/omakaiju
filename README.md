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

omakaiju reads its theme from `~/.config/omarchy/fm.toml`. Changes to this file are hot-reloaded automatically.

## License

MIT

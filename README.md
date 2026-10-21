# omakaiju

![CI](https://github.com/bjornramberg/omakaiju/actions/workflows/ci.yml/badge.svg)
![Release](https://img.shields.io/github/v/release/bjornramberg/omakaiju)

A modern, ultra-lightweight, dual-pane TUI file manager for Unix-like systems.
Built in Go with Bubble Tea + Lipgloss. Both panes start in the directory where you launch it.

## Features

- Dual-pane navigation, dirs-first sorting, arrow keys, per-pane filtering
- Dynamic Omarchy theme integration with hot-reloading
- Nerd Font icons in panes and archive listings
- Text previews with Chroma syntax highlighting, hex dump for binaries, entry listing for zip/tar/tgz (images show metadata only, no pixel rendering)
- Fuzzy finder (recursive, ranked) plus a live per-pane filter
- Yank/paste with progress bar and cancel, mark/move across panes, trash (XDG) or force delete, add/rename
- Asynchronous I/O so the UI never blocks on filesystem work: previews, the fuzzy matcher and the walk all run in the background
- Recovers from internal errors instead of dropping the session: a panic in the render or update path is caught, reported in the bottom bar and the app carries on
- Version splash on the bottom-right: fades in on launch, fades out after three seconds

### Footprint

One static binary, no runtime dependencies and no daemon.

| | |
|---|---|
| Binary | ~9.4 MB (stripped) |
| Resident | ~23 MB |
| Startup | ~35 ms |
| Config | one theme file; everything else is a default |

The bulk of the binary is Chroma's syntax highlighting and the Go runtime.
Dropping the highlighting would save a couple of megabytes and the previews
with it, which is not a trade this app makes.

## Requirements

- Linux (for prebuilt binaries; the source also builds fine on Unix-likes)
- A Nerd Font (optional, for icons)

## Install

Install the latest prebuilt release with one command:

```bash
curl -fsSL https://raw.githubusercontent.com/bjornramberg/omakaiju/main/install.sh | sh
```

The installer detects your architecture (amd64/arm64), downloads the Linux
binary from GitHub Releases, verifies it against the published
`checksums.txt`, and installs it into `~/.local/bin`.

The install is atomic: the new binary is staged beside the old one and
renamed into place, so the binary is never missing and a failed install
leaves the working one in place.

## Use as the Omarchy file manager

omakaiju is a drop-in replacement for Omarchy's default file manager binding.
These steps target **Omarchy Quattro** (Hyprland, Lua-based config).

**1. Make sure `omakaiju` is on your `PATH`.** If you picked a custom
prefix with the installer (`OMAKAIJU_PREFIX=...`), make sure
`$PREFIX/bin` is on your `PATH`. The default `~/.local/bin` is already
on the path for most distros, but verify with `command -v omakaiju`.

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

Inside omakaiju, `Shift+P` is the fuzzy finder and only applies while omakaiju
has focus, so it no longer conflicts with the Hyprland binding.

Check the result any time with:

```bash
omarchy menu keybindings --print
```

To revert everything, remove the two lines from step 2 and
`omarchy refresh hyprland`.

## Development

This README is the design document. It is what users read on GitHub and in
the release tarball, and the tests check it against the code: the keybinding
table is compared with the keybind table, and the claims it makes about the
installer and the release are checked against those files. A change to the
app that makes the README wrong fails the build.

These paths are for hacking on the source or doing installs by hand; the
[Install](#install) section is all most users need.

Requires Go 1.27+ (Linux or any Unix-like).

```bash
make build    # go build -o omakaiju ./cmd/omakaiju
make run      # build + ./omakaiju
make install  # install to /usr/local/bin (override with PREFIX=/path)
make test     # go test ./...
make race     # go test -race ./...   (catches data races; CI runs it too)
make lint     # go vet ./...
make clean    # rm -f omakaiju
```

The text-handling code is fuzzed. The fuzzers cover the key-list matcher, the
sanitiser, the truncation helpers and the fuzzy finder — the functions that
take text from the user or from the filesystem. They have found real bugs: a
matcher that sliced past the end of a string, and a name that came out wider
than its budget.

```bash
go test -run=XXX -fuzz=FuzzEmphasizeText -fuzztime=60s ./internal/ui/...
```

A crash is written to `testdata/fuzz/` and replayed on every later run.

Or the classic one-liner, if you'd rather not clone:

```bash
go install github.com/bjornramberg/omakaiju/cmd/omakaiju@latest
```

### Manual asset downloads

Every release on [GitHub Releases](https://github.com/bjornramberg/omakaiju/releases)
ships tarballs (`omakaiju_linux_*.tar.gz` with LICENSE and README) and bare
binaries (`omakaiju_linux_amd64` / `omakaiju_linux_arm64`), each accompanied by
a `checksums.txt`:

```bash
curl -fsSL https://github.com/bjornramberg/omakaiju/releases/latest/download/omakaiju_linux_amd64 \
  -o omakaiju && chmod +x omakaiju
```

## Keybindings

### Navigation
| Key | Action |
|-----|--------|
| `↑` / `↓` / `←` / `→` | Move cursor (cursor restores to exited dirname on `←`) |
| `Enter` | Enter directory, or open file with the desktop handler (`gio open`, `xdg-open` fallback) |
| `→` | Enter directory only (never opens files) |
| `Tab` | Toggle active pane |

### Jump (Space + letter)
| Key | Action |
|-----|--------|
| `Space` + `a`–`z` | Jump to first file starting with letter (sorting-aware, wraps) |

The shifted key is the whole binding: `Shift+G` opens the goto prompt and
the unshifted `g` does nothing. In the file view only `q`, `Esc`, `Tab`,
`Enter`, the arrows and `Space` work without a modifier — every other
binding needs the shift. The overlays have their own keys: `j`/`k` move in
the fuzzy finder, the arrows walk the key list, and typing searches in
both.

### File Operations (Shift + key)
| Key | Action |
|-----|--------|
| `Shift+C` | Copy (yank) entry under cursor |
| `Shift+V` | Paste / move marked entries |
| `Shift+D` | Delete prompt: `t`rash (default, also `y`/`Enter`), `f`orce delete, `n`/`Esc` cancel |
| `Shift+M` | Mark / unmark entry (repeat to select several for bulk move) |
| `Shift+A` | Add file/directory (trailing `/` creates a directory) |
| `Shift+R` | Rename entry under cursor |

### Pane / View (Shift + key)
| Key | Action |
|-----|--------|
| `Shift+S` | Stack file panes vertically / restore side by side |
| `Shift+O` | Cycle sort order: name → size↓ → size↑ → name (dirs stay first) |
| `Shift+I` | Toggle preview |
| `Shift+P` | Fuzzy find (`j/k` navigate, type/backspace refines, `Enter` jumps, `Esc` exits) |
| `Shift+F` | Filter pane (per-pane, arrows keep the prompt open) |
| `Shift+G` | Goto path |
| `Shift+E` | Refresh active pane |
| `Shift+K` | Key list overlay: search across keys and descriptions, arrow keys to browse, closes with `Esc`/`Shift+K` |

### System
| Key | Action |
|-----|--------|
| `q` / `Shift+Q` | Quit |
| `Esc` | Cancel copy / dismiss prompt / clear filter |

The bottom bar stays minimal (`Shift+K:keys  q:quit`); the full table lives
behind the `Shift+K` overlay so shortcuts don't clutter the normal view. The
overlay searches across keys and descriptions (type "pane" to see every
pane binding), and arrow keys walk the rows highlight-style like the fuzzy
finder. Nothing fires from the panel — close it with `Esc` or `Shift+K`.

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

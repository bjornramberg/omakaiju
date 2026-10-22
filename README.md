# omakaiju

![CI](https://github.com/bjornramberg/omakaiju/actions/workflows/ci.yml/badge.svg)
![Release](https://img.shields.io/github/v/release/bjornramberg/omakaiju)

A modern, ultra-lightweight, dual-pane TUI file manager for Unix-like systems. Built in Go with Bubble Tea + Lipgloss.

---

## Why omakaiju?

* **Blazing Fast & Ultra-Lightweight:** Launches in ~35ms with a footprint of ~23MB RAM as a single static binary with no daemons or runtime dependencies.
* **Dead Simple Keybindings:** Navigate with arrow keys, and press `Shift+K` for everything else. Keybindings are clean, sectioned, and fully filterable on the fly.
* **Non-Blocking Asynchronous I/O:** Syntax highlighting, previews, fuzzy matching, and directory walks run strictly in the background—the UI never lags or freezes.
* **Resilient & Crash-Proof:** Features built-in error recovery. Internal render or update panics are caught and reported in the status bar while your session stays active.
* **Seamless Omarchy Integration:** Dynamic theme integration with instant hot-reloading using Omarchy's native `colors.toml`.

---

## Install

Install the latest prebuilt release with one command:

```bash
curl -fsSL https://raw.githubusercontent.com/bjornramberg/omakaiju/main/install.sh | sh
```

The installer detects your architecture (`amd64`/`arm64`), verifies the download against published checksums, and atomically places the binary in `~/.local/bin`.

### System Requirements

* **OS:** Linux (prebuilt binaries) or Unix-like systems (built from source)
* **Font:** A [Nerd Font](https://www.nerdfonts.com/) (optional, for pane and archive icons)

---

## Keybindings

Navigation is as simple as using your **Arrow Keys** and **Tab** to switch active panes. 

For all other operations, the only shortcut you need to remember is **`Shift+K`**. This brings up the overlay—a searchable, sectioned list where you can filter shortcuts instantly (type to filter by key or action, e.g., typing *"pane"* or *"copy"*).

```
Shift+K:keys   q:quit
```

---

## Configuration & Integration

### Omarchy Desktop Integration

To use `omakaiju` as your default file manager in **Omarchy Quattro** (Hyprland, Lua-based config):

1. **Verify PATH:** Ensure `omakaiju` is on your `$PATH` (`command -v omakaiju`).

2. **Rebind Shortcut:** Update `~/.config/hypr/bindings.lua`:

   ```lua
   hl.unbind("SUPER + SHIFT + F")
   o.bind("SUPER + SHIFT + F", "File manager", "omarchy launch tui omakaiju")
   ```

3. **Restore Window Bindings:** If you previously unbound `SUPER + CTRL + L` or `SUPER + CTRL + F` to resolve TUI `Ctrl+F` conflicts, remove those unbind lines—`omakaiju` uses `Shift+P` for fuzzy finding and will not conflict.

### Theming

omakaiju automatically reads colors from `~/.local/state/omarchy/current/theme/colors.toml`. Theme switches are hot-reloaded instantly. Override the path anytime by setting the `OMARCHY_COLORS` environment variable.

---

## Under the Hood

### Footprint & Performance Benchmarks (v0.9.4)

| Metric              | Measurement                       |
| ------------------- | --------------------------------- |
| **Binary Size**     | ~9.4 MB (stripped)                |
| **Resident Memory** | ~23 MB                            |
| **Startup Time**    | ~35 ms                            |
| **Dependencies**    | 0 runtime dependencies, 0 daemons |

> **Note on Binary Size:** The majority of the binary weight comes from Chroma's syntax highlighting engine and the Go runtime.

### Previews & Asset Preloading

* **Text files:** Chroma syntax highlighting with line numbers.
* **Binaries:** Automatic hex dump view.
* **Archives:** Full table-of-contents listings for `.zip`, `.tar`, and `.tgz`.
* **Images:** Metadata-only preview to keep memory usage minimal.

---

### Local Development & Hacking

Requires Go 1.27+ on a Unix-like system.

```bash
make build    # Build binary to ./omakaiju
make run      # Build and launch
make test     # Run test suite
make race     # Run test suite with race detector
make lint     # Run go vet
make clean    # Remove build artifacts
```

Alternatively, install directly via Go:

```bash
go install github.com/bjornramberg/omakaiju/cmd/omakaiju@latest
```

### Automated Fuzz Testing

Crucial text-parsing and UI rendering paths (key matching, input sanitization, text truncation, fuzzy finding) are fuzzed against edge cases:

```bash
go test -run=XXX -fuzz=FuzzEmphasizeText -fuzztime=60s ./internal/ui/...
```

Fuzzing crashers are automatically written to `testdata/fuzz/` and replayed on subsequent test runs.

### Manual Asset Downloads

Pre-built binaries (`omakaiju_linux_amd64` / `omakaiju_linux_arm64`) and release tarballs are hosted on [GitHub Releases](https://github.com/bjornramberg/omakaiju/releases) along with `checksums.txt`:

```bash
curl -fsSL https://github.com/bjornramberg/omakaiju/releases/latest/download/omakaiju_linux_amd64 \
  -o omakaiju && chmod +x omakaiju
```

---

### Which pane has the focus

The focused pane is marked three ways at once, so it stays obvious on a dim
screen and where two colours are too close to tell apart:

- a marker glyph in its header, the shape cue that needs no colour at all
- a band across its header, the theme background blended toward the accent
- a dimmed listing in the other pane, whose cursor row drops the selection bar

All three are derived from the same palette the rest of the UI draws from, so
they follow a theme switch along with everything else.

## License

[MIT](LICENSE)

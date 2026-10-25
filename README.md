# omakaiju

![CI](https://github.com/bjornramberg/omakaiju/actions/workflows/ci.yml/badge.svg)
![Release](https://img.shields.io/github/v/release/bjornramberg/omakaiju)

A modern, ultra-lightweight, dual-pane TUI file manager for Unix-like systems. Built in Go with Bubble Tea + Lipgloss.

---

## Why omakaiju?

* **Blazing Fast & Ultra-Lightweight:** Launches in ~35ms with a footprint of ~19MB RAM as a single static binary with no daemons or runtime dependencies.
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

The installer detects your architecture (`amd64`/`arm64`), verifies the download against published checksums, and atomically places the binary in `~/.local/bin`. It prints the version it installed, so you can see at a glance what you got.

To pin a version instead of taking the latest:

```bash
curl -fsSL https://raw.githubusercontent.com/bjornramberg/omakaiju/main/install.sh | sh -s -- v0.10.0
```

The version can also be given as `OMAKAIJU_VERSION=v0.10.0`, and `OMAKAIJU_PREFIX` chooses where it is installed (`~/.local` by default).

### System Requirements

* **OS:** Linux (prebuilt binaries) or Unix-like systems (built from source)
* **Font:** A [Nerd Font](https://www.nerdfonts.com/) (optional, for pane and archive icons)

---

## Using omakaiju

omakaiju is built around three things you already know:

* **Arrow keys** move the cursor, and `←` walks back up to the parent directory.
* **`Tab`** switches between the two panes.
* **`Shift+K`** opens the full key list: a searchable, sectioned overlay where you can filter by key or action — type *"pane"* or *"copy"* to narrow it down.

Everything else is a **Shift + letter** combination: `Shift+C` copies, `Shift+V` pastes, `Shift+D` deletes, `Shift+F` filters the listing. The complete table lives in the [Keybindings](#keybindings) section further down.

```
Shift+K:keys   q:quit
```

### Which pane has the focus

The focused pane is marked three ways at once, so it stays obvious on a dim
screen and where two colours are too close to tell apart:

* a marker glyph in its header, the shape cue that needs no colour at all
* a band across its header, the theme background blended toward the accent
* a dimmed listing in the other pane, whose cursor row drops the selection bar

All three are derived from the same palette the rest of the UI draws from, so
they follow a theme switch along with everything else.

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

### Footprint & Performance

Measured on the v0.10.0 release build (stripped, static) on an idle Linux desktop:

| Metric              | Measurement                       |
| ------------------- | --------------------------------- |
| **Binary Size**     | ~9.4 MiB (stripped)               |
| **Resident Memory** | ~19 MB                            |
| **Startup Time**    | ~35 ms                            |
| **Dependencies**    | 0 runtime dependencies, 0 daemons |

> **Note on Binary Size:** The majority of the binary weight comes from Chroma's syntax highlighting engine and the Go runtime.

The size is the released file, the memory is `VmRSS` from `/proc/<pid>/status`
while a directory is listed, and the startup time is how long the process
takes to reach its first frame. The memory figure moves with the size of the
directory being listed and with the theme in use.

### Previews & Asset Preloading

* **Text files:** Chroma syntax highlighting with line numbers.
* **Binaries:** Automatic hex dump view.
* **Archives:** Full table-of-contents listings for `.zip`, `.tar`, and `.tgz`.
* **Images:** Metadata-only preview to keep memory usage minimal.

---

### Local Development & Hacking

This README is the design document. It is what users read on GitHub and in the release tarball, and the tests check it against the code: the keybinding table is compared with the keybind table, so a change to the app that makes the README wrong fails the build.

Requires Go 1.27+ on a Unix-like system.

```bash
make build    # Build binary to ./omakaiju (static, like the release)
make run      # Build and launch
make test     # Run test suite
make race     # Run test suite with race detector
make lint     # Run go vet
make install  # Install to $(PREFIX)/bin, honouring DESTDIR
make clean    # Remove build artifacts
```

`make build` produces the same static binary the release publishes, and stamps
the version it was built from: the release version on a tagged checkout, and
the distance from that tag otherwise.

Alternatively, install directly via Go:

```bash
go install github.com/bjornramberg/omakaiju/cmd/omakaiju@latest
```

A binary installed that way has no version stamped into it and says `dev`.

### Automated Fuzz Testing

Crucial text-parsing and UI rendering paths (key matching, input sanitization, text truncation, fuzzy finding) are fuzzed against edge cases:

```bash
go test -run=XXX -fuzz=FuzzEmphasizeText -fuzztime=60s ./internal/ui/...
```

Fuzzing crashers are automatically written to `testdata/fuzz/` and replayed on subsequent test runs.

### Manual Asset Downloads

Pre-built binaries (`omakaiju_linux_amd64` / `omakaiju_linux_arm64`) and release tarballs are hosted on [GitHub Releases](https://github.com/bjornramberg/omakaiju/releases) along with `checksums.txt`. Name the version you want, so the download is the one you meant:

```bash
TAG=v0.10.0
curl -fsSL "https://github.com/bjornramberg/omakaiju/releases/download/$TAG/omakaiju_linux_amd64" -o omakaiju
curl -fsSL "https://github.com/bjornramberg/omakaiju/releases/download/$TAG/checksums.txt" -o checksums.txt
grep 'omakaiju_linux_amd64$' checksums.txt | sha256sum -c -
chmod +x omakaiju
```

`releases/latest/download/<file>` works too, and serves whatever GitHub currently marks as the latest release.

---

## Keybindings

The complete list of what the app binds. It is compared with the code on
every build, so it cannot drift away from what the keys actually do.

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

### File Operations (Shift + key)
| Key | Action |
|-----|--------|
| `Shift+C` | Copy (yank) entry under cursor |
| `Shift+V` | Paste into this pane (y-staged copy, or move marked entries there) |
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
| `Shift+P` | Fuzzy find (type to search, arrows or ctrl+j/k move, enter jumps). `Enter` selects the result; `Esc` exits |
| `Shift+F` | Filter pane (per-pane, arrows keep the prompt open) |
| `Shift+G` | Goto path |
| `Shift+E` | Refresh active pane |
| `Shift+K` | Key list overlay: search across keys and descriptions, arrow keys to browse, closes with `Esc`/`Shift+K` |

### System
| Key | Action |
|-----|--------|
| `q` / `Shift+Q` | Quit |
| `Esc` | Cancel copy / dismiss prompt / clear filter |

The shifted key is the whole binding: `Shift+G` opens the goto prompt and the
unshifted `g` does nothing. In the file view only `q`, `Esc`, `Tab`, `Enter`,
the arrows and `Space` work without a modifier — every other binding needs the
shift. The overlays have their own keys: typing searches in both, and the
arrows walk both lists. In the fuzzy finder `Ctrl+J`/`Ctrl+K` also move (a
terminal may send `Ctrl+J` as a newline), and `Enter` jumps to the result and
leaves the cursor on it.

Long copies show a progress bar; `Esc` cancels and removes only the partial
file. A move that has to cross a filesystem — onto a share, say — is a copy
followed by the removal of the original, so it gets a bar and a cancel of its
own.

### File operations are safe by construction

Copies and moves never expose a half-written file, and never lose one.

* A copy is written to a scratch file beside the destination and renamed into
  place only once it is whole, flushed and checked against the source. The
  destination you already had is replaced atomically, or not at all — and it
  is held aside until the replacement is confirmed, so a replacement that
  does not land puts the old file back.
* A failed or cancelled copy does not put a half-written file in place, and
  nothing it made for itself is left lying around. In a folder copy the files
  it had already finished stay: they are whole, and they are the ones the
  copy was asked for.
* A copy keeps the file's mode — including the setuid, setgid and sticky
  bits — and its modification and access times. A copied directory gets the
  source's mode and times once its contents are in place, so the umask cannot
  mask them and writing the tree does not make the directory look newer.
  Ownership, ACLs, extended attributes and sparseness are not carried over:
  a sparse file arrives with all its blocks.
* A move between filesystems — onto an SMB share, say — is a rename the kernel
  refuses, so omakaiju copies the file across and removes the original. The
  original is only removed once the copy has landed and been verified.
* A move never replaces a file that is already there. Renaming onto a name
  that is taken is refused, and the file under that name is left alone. The
  one case the kernel cannot make safe is another program writing to that
  exact name in the same instant.
* A batch of moves that would put two entries under the same name is refused
  before anything moves, so one entry can never quietly overwrite another.
* Each entry of a move is moved on its own: it lands only once its
  destination is safely in place, and it keeps its source until then. A batch
  can therefore finish partly — if one entry fails, the entries that already
  landed stay moved, and the report names what failed and what did not. What
  did not move stays selected, so it can be tried again.
* A source whose size or timestamp changed while it was on its way is left
  alone: it is not the file that arrived.
* Only regular files are copied. A pipe, a socket or a device is reported by
  name and left where it is, rather than copied.
* A file reached by two paths (a symlinked directory, a share mounted in two
  places) is recognised as one file, so copying or moving it onto itself is
  refused instead of emptying it.
* Creating a file never empties one that is already there.
* Every operation that fails says so, names the file, and stays on screen
  until you do something else. What did not move stays selected, so you can
  fix the cause and try again.

## License

[MIT](LICENSE)

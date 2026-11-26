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
| `Shift+V` | Paste into this pane (y-staged copy, or move marked entries there). A taken destination name asks: overwrite, skip, or keep both |
| `Shift+D` | Delete prompt: `t`rash (also `y`), `p`ermanently delete (a second `y` confirms), `n`/`Esc`/`Enter` cancel |
| `Shift+M` | Mark / unmark entry (repeat to select several for bulk move) |
| `Shift+A` | Add file |
| `Shift+N` | Add directory |
| `Shift+R` | Rename entry under cursor |
| `Shift+T` | Open the trash in the active pane |

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
| `q` / `Shift+Q` | Quit (asks whether to empty a non-empty home trash first; `Enter` means no) |
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

A paste onto a name that is already taken does not replace it silently. It
asks whether to overwrite, skip, or keep both; the result then says how many
entries were replaced. Overwriting a folder merges into it, as before.

Quitting with items in the trash asks whether to empty it. Answering yes
empties the home trash and quits; answering no — or pressing `Enter` or `Esc`
— quits and leaves the trash alone. Only the home trash is covered, because
the per-filesystem trashes are not walked.

Deleting permanently is always two steps. The first key opens a question that
names what will go — a file, or a folder and everything in it — and only a
second confirmation deletes it. `Enter` is the safe answer on every
confirmation.

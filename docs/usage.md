# Using omakaiju

omakaiju is built around three things you already know:

* **Arrow keys** move the cursor — up from the first row reaches the `..`
  row — and `←` walks back up to the parent directory.
* **`Tab`** switches between the two panes.
* **`Shift+K`** opens the full key list: a searchable, sectioned overlay where you can filter by key or action — type *"pane"* or *"copy"* to narrow it down.

Everything else is a **Shift + letter** combination: `Shift+C` copies, `Shift+V` pastes, `Shift+D` deletes, `Shift+F` filters the listing. The complete table lives in the [Keybindings](keybindings.md) page.

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

### Mouse

The mouse works, and it is on by default:

* **Left click** a file row to focus its pane and select that row. Clicking a
  pane's header or empty space focuses the pane without moving the cursor.
  The listing stays where it is: clicking a row that is already on screen
  selects it where it is, without scrolling the list.
* **Double click** opens what is under the pointer: a directory is entered, a
  file is opened with the desktop handler, and the `..` row goes to the
  directory above.
* **Wheel** over a pane scrolls it three rows a notch, and never moves the
  focus away from the pane you are working in.
* **Drag** a row onto the other pane to move that entry into the directory the
  other pane shows. Hold **Ctrl** (or **Alt**) while dropping to copy it
  instead. The target pane tints its header while the pointer is over it, and
  says whether the drop would move or copy. A press and release that never
  leaves the pane is just a click, and the `..` row cannot be dragged.
* **Right click** a row for the menu of what can be done with it: open,
  rename, copy, mark and delete. The menu opens under the pointer, follows it
  as it moves, and answers to the arrow keys as well — `Enter` runs the
  highlighted action and `Esc` closes it.

Each pane shows a `..` row above its listing — the way back to the directory
above — except at the filesystem root, where there is none. It is a real
entry: the cursor can be on it and Enter goes up a level, and a double click
does the same. It answers to nothing else: copying, marking, renaming or
deleting it is refused.

Clicks outside the two file panes — the top and bottom bars, the preview
column — do nothing. While a prompt or overlay is open the mouse is released
again, so its text can be selected.

A terminal in mouse-reporting mode stops doing its own drag-to-select, and
the preview pane is exactly the text people want to copy. `OMAKAIJU_NO_MOUSE=1`
gives the pointer back to the terminal; `OMAKAIJU_MOUSE=0` does the same for
shells carrying the older name.

---

## Configuration & Integration

### Omarchy Desktop Integration

To use `omakaiju` as your default file manager in **Omarchy Quattro** (Hyprland, Lua-based config):

1. **Verify PATH:** Ensure `omakaiju` is on your `$PATH` (`command -v omakaiju`).

2. **Make it the default:** Run `omakaiju --default` and confirm. It unbinds the stock `SUPER + SHIFT + F` file-manager shortcut and binds it to omakaiju in `~/.config/hypr/bindings.lua`. The installer offers the same choice after it installs.

   To edit the file yourself instead, add:

   ```lua
   hl.unbind("SUPER + SHIFT + F")
   o.bind("SUPER + SHIFT + F", "File manager", "omarchy launch tui omakaiju")
   ```

3. **Restore Window Bindings:** If you previously unbound `SUPER + CTRL + L` or `SUPER + CTRL + F` to resolve TUI `Ctrl+F` conflicts, remove those unbind lines—`omakaiju` uses `Shift+P` for fuzzy finding and will not conflict.

### Theming

omakaiju automatically reads colors from `~/.local/state/omarchy/current/theme/colors.toml`. Theme switches are hot-reloaded instantly. Override the path anytime by setting the `OMARCHY_COLORS` environment variable.

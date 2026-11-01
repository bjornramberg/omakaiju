# TODO

The backlog lives in the [GitHub issues](https://github.com/bjornramberg/omakaiju/issues) —
#15 to #36, each with a label saying what kind of work it is (`correctness`,
`ui`, `housekeeping`) and the detail below. This file is the longer version
of the same list; an item is removed from here when its issue is closed.

What is still open, ordered by how much it can cost the user rather than by
how easy it is to fix. Each item says where it is, what breaks, and what
fixing it would take.

Two things are deliberately not on this list, because they are decisions
rather than defects: whether a batch move should be all-or-nothing (the
README now says plainly what it does instead), and whether the file
operations should be signed (#34).

## Correctness and robustness

## UI polish

### 27. A 40-column terminal is too narrow to use

`internal/ui/layout.go:14` allows a 40-column terminal, which gives the
panes thirteen columns each, the content seven, and a filename about three
cells wide.

Fix: raise the minimum width, or hide the preview automatically when the
terminal is narrow.

### 28. Icons are matched case-sensitively and have no fallback

`internal/icons/nerdfont.go:31` matches the extension as written, so
`FOO.MD` gets no icon although the file type is detected in lower case, and
`Dockerfile` is only matched by name (`internal/icons/nerdfont.go:72`). A
terminal without a Nerd Font shows a tofu box in every row and in the top
bar.

Fix: lowercase the extension, recognise the common names (Dockerfile and
friends), and offer a no-icon fallback.

### 29. The old preview path has no byte cap

`internal/ui/preview.go` (`RenderTextFromFile`) still reads a thousand lines
with no limit on their size. The app uses the new bounded path, but the old
one is still reachable from the tests.

Fix: route it through the bounded reader, or delete it.

### 30. A preview load can be started twice for the same file

`internal/app/model.go` (`updatePreview`) starts a load whenever the cached
path is empty, which it is while a load is in flight — so moving the cursor
over a slow file starts another load of the same file, and a result that
arrives after a theme change is not checked against the current theme
either.

Fix: one load in flight per path, and drop a result whose theme is not the
one on screen.

### 31. `clearFuzzy` clears shared arrays in place

`internal/app/model.go` empties the fuzzy result slices with `clear`, which
mutates the backing array a copy of the model still holds — the same
aliasing the crash-recovery contract says must not happen.

Fix: replace the slices instead of clearing them.

## Structure and housekeeping

* **#32** `internal/app/model.go` is 2,600 lines and holds the model, the
  key handling, the overlays, the commands and the prompts. Splitting it
  would make the next change cheaper.
* **#33** ~~Coverage~~ — done: `internal/version` and `internal/icons` are
  at 100%, and `cmd/omakaiju` has a smoke test that runs the real binary's
  `--version` path. The rest of that package is the terminal entry point,
  which needs a terminal to exercise.
* **#34** The releases are not signed: the checksum file comes from the
  same release as the binary, so it proves the download arrived intact but
  not that the release is authentic.
* **#35** Dependencies and workflows are updated by hand.
* **#36** The local design document ("omakaiju - TUI File Manager Design
  Document.md") describes Tview, Kitty image previews and `h/j/k/l` — none
  of which the app does. It is gitignored, so it is scratch: delete it, or
  fold whatever is still wanted into the README.

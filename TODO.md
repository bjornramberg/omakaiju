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

### 16. The copy-into-itself guard compares paths as written

`internal/fs/fs.go:409` (`isInside`) compares the source and the destination
lexically. A destination directory that is a symlink into the source tree
passes the check, and the copy then writes into the source tree, recursing
until the depth bound stops it.

Fix: resolve both paths before comparing, or compare the device and inode of
the destination's parent with the source tree.

Verify: a destination that is a symlink into the source must be refused.

### 17. Archive listing has no decompression budget

`internal/fs/archive.go:52` and `internal/fs/archive.go:93` read a gzip
stream and walk tar headers; `tar.Next` decompresses each member's payload
to reach the next header, so a small archive that declares a huge member
keeps the preview worker busy for as long as it takes to decompress it.
`internal/fs/archive.go:75` reads a zip's central directory with no cap at
all, while the tar path has one (`maxArchiveScan`, `internal/fs/archive.go:20`).

Fix: wrap the gzip reader in an `io.LimitReader`, cap the zip entries like
the tar ones, and report the cut as truncation rather than as an archive
with fewer members.

Verify: a `.tgz` holding a gigabyte of zeros must list in bounded time and
say it was truncated.

### 18. Archive entry names come back raw

The fs layer hands out the member names as the archive stores them: control
bytes, absolute paths and `..` among them. The UI sanitises them when
drawing now, but the fs layer is what a future "extract" would consume, and
that is where the check belongs.

Fix: sanitise at the fs boundary and flag entries that are absolute or that
climb out of the archive.

### 19. Opening a file can pass an option to the handler

`internal/fs/open.go:25` runs the desktop handler with the path as a bare
argument. There is no shell, so this is not command injection — but a file
named `-a` or `--help` is parsed by `gio open` as an option rather than as
the thing to open.

Fix: put `--` before the path where the handler supports it.

Verify: a file named `--help` opens the file.

### 20. The handler process is never reaped

`internal/fs/open.go:36` calls `Process.Release()`, which drops the handle
without waiting. Every file opened leaves a zombie for the rest of the
session.

Fix: wait for the child in a goroutine instead of releasing it.

### 21. The trash is the home trash only

`internal/fs/trash.go:20` always uses the XDG home trash, and
`internal/fs/trash.go:66` refuses anything on another filesystem. The
freedesktop spec wants a per-filesystem trash (`$topdir/.Trash-$uid`), so
deleting something on an external drive, on tmpfs or on a share always falls
back to permanent deletion — which is the one thing the trash is for.

There is also no restore and no empty (issue #2): the `.trashinfo` files
are written well enough for `gio trash --restore` to read them, but the app
cannot restore what it trashed.

Fix: implement the topdir trash — created 0700, refusing a symlinked or
foreign-owned one — and add `Restore` and `EmptyTrash`.

### 22. Two trashes of the same name can collide

`internal/fs/trash.go:82` chooses a free name by looking, and then renames
into it. A second trash of the same basename — from another instance, or
from a desktop file manager sharing the trash — can replace the first
payload and its info file.

Fix: reserve the name by creating the info file with `O_CREATE|O_EXCL`
before the rename, or link and unlink and retry on a collision.

Verify: trashing two files with the same name from different directories
must leave two payloads in the trash.

## UI polish

### 24. Archive rows overflow the preview

`internal/ui/archive.go:16` budgets the member's name, appends the size, and
never clips the row. Most long member paths wrap and push the footer out of
the panel.

Fix: reserve the size column in the name's budget, or clip the finished row.

### 25. The progress bar is two rows where the layout expects three

`internal/ui/bottombar.go:50` draws the progress bar without the height the
other branches pin, so the frame is a row short and the panes jump when a
copy starts or stops.

Fix: draw the progress through the same helper the other branches use.

### 26. Stacked panes are not reconsidered when the terminal shrinks

`internal/ui/layout.go:110` (`CanStack`) is consulted when the user toggles
the layout and never again. Shrink the terminal afterwards and each stacked
pane is two rows high, with no filename visible.

Fix: fall back to side by side at render time when the layout no longer
fits.

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
* **#33** Coverage: `cmd/omakaiju` 0%, `internal/version` 0%,
  `internal/icons` 0%. The version formatter and the icon map are trivial to
  test; `main` is worth a smoke test.
* **#34** The releases are not signed: the checksum file comes from the
  same release as the binary, so it proves the download arrived intact but
  not that the release is authentic.
* **#35** Dependencies and workflows are updated by hand.
* **#36** The local design document ("omakaiju - TUI File Manager Design
  Document.md") describes Tview, Kitty image previews and `h/j/k/l` — none
  of which the app does. It is gitignored, so it is scratch: delete it, or
  fold whatever is still wanted into the README.

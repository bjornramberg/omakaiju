# TODO

The backlog is in two places. **Issues** are the work with a number, an owner
and a definition of done: the ones already closed (#2, #3, #15–#38), and the
security findings from the release review (see the list below). **This file**
holds the rest: the findings that are real but not urgent, the things the
repository is missing, and the features that are wanted but not planned.

Nothing here is private: no keys, no secrets, no paths from anyone's machine.

## Security and robustness — filed as issues

These are the findings of the release review, and they are issues rather than
lines here, because each one is a hole someone could walk through:

* **#44** a hostile filename must not reach the disk or the terminal — a
  symlink in the destination path turns a paste into an arbitrary write, and
  the rename and delete prompts put raw filename bytes into the terminal
* **#46** the theme file is read without a limit and can hang or exhaust
  memory at startup
* **#47** the trash must be checked the same way everywhere — the home trash
  is not checked the way the per-filesystem trash is, and restore trusts the
  trash's own records
* **#49** the installer must only install what it verified — the unsigned
  opt-out is a full bypass, a tarball member can be a symlink, and the
  release's own toolchain is fetched at run time
* **#51** the interface must say what a destructive action will do — pasting
  over an existing file replaces it without asking, the question on exit is
  unreadable at small widths while Enter still means yes, and "force" is the
  label for deleting something permanently
* **#54** the release pipeline still fetches its tooling at run time

## Security and robustness — still to fix

### The repository itself

* `main` is not protected: no force pushes, no deletions. Nothing requires a
  review, which is deliberate for a one-person project, but the history can
  still be rewritten.
* No environments beyond the `release` one, no CODEOWNERS enforcement, and
  no tag signing. Tags are annotated but unsigned, and most commits are
  unsigned too.
* Vulnerability alerts are off, code scanning is not enabled, non-provider
  secret scanning is off, and so are the validity checks that confirm a
  leaked credential is live. All of these are free for a public repository.
* No `SECURITY.md`: nobody has said where to report a vulnerability.
* The Go toolchain is pinned to the exact patch `1.27.0`, so a release
  binary does not get the fixes in 1.27.1 and later.
* `ci.yml` fetches `govulncheck` and `staticcheck` by `@latest` — code from a
  moving reference, in the pipeline. The `make check` target now pins them;
  the workflow should call it.
* The actions in `ci.yml` are pinned to tags (`checkout@v7`, `setup-go@v7`,
  `upload-artifact@v7`) while the other workflows pin commit SHAs. Tags can
  be moved; commits cannot.
* No `actionlint` in CI, which is why a Dependabot bump could point the
  agent workflow at an action that no longer exists and nothing noticed.
* The agent workflow's action installs its own binary with `curl … | bash`
  when it runs, so that tool is fetched from a moving reference too. That is
  the action's own behaviour and cannot be pinned from here.
* The repository's `opencode.json` allows the agent to work outside the
  repository (`external_directory: allow`). It is left alone on purpose: it
  is what lets local sessions work, and on a CI runner there is nothing
  secret on disk for it to read.
* No pull request or issue templates, and no changelog beyond what
  GoReleaser writes.

### In the app

* **Readers can block on a FIFO.** The preview, the listing and the archive
  readers stat a path and then open it; a file swapped for a FIFO in between
  blocks the open forever. The copy path already has the pattern to copy:
  open with `O_NONBLOCK`, then check the opened file.
* **The published binary is not built with `-trimpath`.** Every crash report
  a user pastes into an issue carries the builder's filesystem layout. The
  Makefile and CI use it; GoReleaser does not.
* **A crash report is printed unsanitised**, so a panic whose message
  contains a filename can put escape sequences into the terminal.
* **The terminal restore is escape-only.** It does not put the terminal back
  from raw mode, and it is written to stderr — so a redirected stderr leaves
  the terminal in raw mode with the alternate screen up.
* **Blocking filesystem calls happen on the event loop**: a stat, a trash
  listing, emptying a trash. On a stalled mount the whole interface freezes
  and quitting looks broken.
* **Preview and fuzzy work cannot be cancelled.** Holding the cursor key over
  a slow directory starts a read per movement, none of them cancellable.
* **The file listing is copied on every frame**, so a directory with a
  hundred thousand entries costs a copy per frame and per key press.
* **The preview reads a whole directory to count its entries**, duplicating
  what the pane already knows.
* **A theme change is read on every event**, with no debounce, so a program
  writing to the watched directory keeps the app busy.
* **A name with a bidi control in it renders as a different name**, so
  `report.pdf` can appear as `report.txt`. The format characters are not
  neutralised.
* **A file whose name is 246 to 255 bytes cannot be trashed**: the name of
  its record file is longer than the filesystem allows.
* **There is no limit on the number of files a copy will queue**, and a
  source that cannot be read aborts the whole batch rather than that one
  entry.
* **A move has a window between checking the destination and renaming** in
  which another process can put a file there, and a source that changes in a
  way that keeps its size and timestamp is treated as unchanged.
* **Nothing sweeps up after a crash.** A killed copy leaves its scratch files,
  its staged links and its hard-link backups behind, and a crash between
  moving a file to the trash and writing its record leaves a payload that
  the trash listing does not mention and cannot be emptied.
* **A setuid bit is copied verbatim.** It is harmless as an ordinary user,
  because the copy belongs to the user; it matters only if the app is ever
  run as root.

### The interface

* The error surface clips a message in the middle of a word, with no
  ellipsis, so a failure across several files loses most of what it said.
* The advertised minimum terminal size shows no files at all: a pane needs
  more rows than the minimum allows, so the app looks like an empty
  directory.
* `Shift+E` gives no acknowledgement, so on a slow disk it looks like a
  hang.
* A refresh, a listing and an operation can disagree: after moving into a
  slow directory, the old directory's rows are still on screen and an
  operation acts on it.
* Messages are cleared by the next key press, so a failure being read
  disappears.
* The README promises line numbers in the preview that are not there, and
  its first-minute snippet does not match what the bar draws.
* The README calls the key list "the complete list of what the app binds"
  while excluding the debug pane from it.
* The README's arrow row contradicts itself, and it says the space-jump
  "wraps" when it does not.
* The marked rows use an ASCII `*` while the focused pane uses `▸`; an
  executable looks like any other file; the hostname in the top bar reads as
  a stray word.
* Nothing remembers where you were: both panes start in the working
  directory, and the sort and the flags are forgotten.
* The design document and this file are both scratch: the design document
  describes a program that does not exist, and this file has been the
  backlog for a while.

## Features that are wanted

Ordered by how much they cost the user, with the rough size of each.

* **Bulk delete and copy honour the marked set** (medium). The marks are
  there, the status line counts them, and the operations ignore them: mark
  fifty files, press `Shift+D`, and one file is deleted.
* **Restore from the trash, and a trash that shows where things came from**
  (small — the functions are written and tested; the interface does not call
  them).
* **Open a file in an editor or a pager** (medium). On a machine with no
  desktop handler there is currently no way to read or edit a file.
* **Select all, select a range, invert, clear the marks** (small).
* **Page up, page down, home, end** (small).
* **Directory history: back and forward** (medium).
* **The fuzzy finder should find directories** (small).
* **Extracting an archive** (medium to large).
* **Sorting by date and by kind** (small to medium).
* **A preview that scrolls** (small to medium).
* **The paste should say which source it is using** — marks or the yank
  clipboard (small).
* **Emptying the trash from the interface, for every filesystem** (small
  once the trash view exists).
* **The panes refresh when something changes on disk** (small to medium).
* **Bookmarks and places** (small).
* **A properties overlay** — owner, group, inode, link target (small to
  medium).
* **Changing permissions, and creating a symlink** (small).
* **Mounts, and unmounting a removable disk** (medium).
* **A way to hide dotfiles** (small).
* **A configuration file, and a way to restore the last session** (small to
  medium; the project prefers environment variables for small settings, so a
  configuration file is a decision rather than an oversight).
* **A starting directory, and returning to the shell's directory on exit**
  (small).
* **A command line** (medium; the project has decided against a general
  command line, so this is a decision to revisit).
* **Searching by content, date, size or kind** (medium).
* **The copy could ask what to do about a name that is taken** (small to
  medium).
* **Decided against, and not to be scheduled:** the mouse, tabs and
  workspaces, resizable panes, git status badges, image previews through
  Kitty or Sixel graphics, and a bare-letter or `h/j/k/l` keymap.

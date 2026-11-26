# Destructive actions

This is a project rule, not a suggestion. Any change that removes, replaces, or
empties something on disk has to say so before the user finds out too late.

The rule exists because a file manager is where people go to move the one copy
of something they care about. A successful operation message is not enough if
the operation quietly replaced a file, and a prompt is not a warning if the
terminal is too narrow to show its beginning.

## The rules

1. **Name the action.** Use the word for what actually happens: "Permanently
   delete", "Replace", "Empty the trash". Do not borrow a softer term such as
   "force" for a permanent delete.
2. **Say what is affected.** A file gets its name. A folder says that the whole
   folder goes, not just its name. A batch says how many entries are involved.
3. **Enter is the safe answer.** `Enter` dismisses a question, cancels, or
   keeps the current state. It never confirms a destructive action.
4. **Permanent deletion is two steps.** The first key opens a question that
   describes the consequence; the second confirms it. One keypress alone must
   never delete anything permanently.
5. **Replacing a destination asks first, or reports it afterwards.** A paste
   onto an existing name offers overwrite, skip, and keep both. If an
   operation replaces something with the user's consent, its result says how
   many entries were replaced.
6. **Fit the advertised minimum width.** At the minimum terminal width the
   question *and* its answers must both be readable. Questions keep their
   beginning, because that is where the verb and the target are; typed input
   keeps its end. Put the answers on their own row rather than letting them
   push the question off screen.
7. **Test the safe path.** Every destructive question has a test that pressing
   `Enter` leaves the data alone, and a test that the question and answers fit
   at the minimum width.

## Where it lives

`internal/app/trash.go` and `internal/app/operations.go` hold the quit and
delete confirmations. `internal/app/paste.go` holds the destination-name
question and the overwrite/skip/keep-both choices. The two-line rendering is
`BottomBar.confirmContent` in `internal/ui/bottombar.go`.

The minimum terminal size is `ui.MinWidth` / `ui.MinHeight` (currently 44 by
10). Tests use those constants or the literal width so a layout change that
breaks the promise fails in CI.

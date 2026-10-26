package app

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/ui"
)

type dirLoadedMsg struct {
	pane  int
	path  string
	files []fs.Entry
	err   error
}

// opFailure is one entry a file operation could not finish, with the reason
// reduced to something a person can read.
type opFailure struct {
	name string
	// path is the entry's source path, used to put a failed move back into
	// the marked set so it can be retried.
	path string
	err  error
}

// fileOpMsg reports the outcome of a file operation, or of a whole batch.
//
// A batch runs to completion inside a single command and reports back as one
// message. That is what makes its outcome independent of completion order:
// there is no race between per-file messages, so one failure cannot be erased
// by a success that happens to arrive after it.
type fileOpMsg struct {
	// verb is the past tense shown for the successful entries: "moved",
	// "copied", "pasted", "deleted", "trashed", "renamed", "created".
	verb string
	// name is set for a single-entry operation, which reads better as
	// "<verb> <name>" than as a count.
	name      string
	succeeded int
	skipped   int
	failures  []opFailure
	// text, when set, replaces the generated result. It is for outcomes that
	// are more than a count, such as a trash that moved the payload but could
	// not write its metadata.
	text string
	// marks holds the marked entries a move batch attempted, keyed by path.
	// A failed entry is put back so the user can fix the cause and retry
	// instead of marking everything again.
	marks map[string]fs.Entry
	// clipboard is the paste selection the batch consumed. A failed paste
	// re-arms it for the same reason.
	clipboard *Clipboard
}

// result renders the line shown in the bottom bar. Failures are sorted by
// name, so the same batch always reads the same way, whatever order its work
// happened to finish in.
func (msg fileOpMsg) result() string {
	if msg.text != "" {
		return msg.text
	}
	if len(msg.failures) == 0 && msg.skipped == 0 && msg.succeeded == 1 && msg.name != "" {
		return msg.verb + " " + msg.name
	}

	var parts []string
	if msg.succeeded > 0 {
		parts = append(parts, fmt.Sprintf("%s %d file(s)", msg.verb, msg.succeeded))
	}
	if msg.skipped > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d", msg.skipped))
	}
	if len(msg.failures) > 0 {
		failures := append([]opFailure(nil), msg.failures...)
		sort.Slice(failures, func(i, j int) bool { return failures[i].name < failures[j].name })
		reasons := make([]string, 0, len(failures))
		for _, f := range failures {
			reasons = append(reasons, fmt.Sprintf("%s (%s)", f.name, readableOpError(f.err)))
		}
		parts = append(parts, fmt.Sprintf("failed %d: %s", len(failures), strings.Join(reasons, ", ")))
	}
	return strings.Join(parts, ", ")
}

// readableOpError strips the noisy prefix os.LinkError and os.PathError add
// ("rename /a /b: ", "open /a: ") and keeps the reason the user can act on,
// such as "permission denied". The file name is already shown beside it, so
// repeating the path adds nothing.
//
// A refused replacement is the exception: fs reports it as a
// *fs.RefusedReplacement, which names the destination that could not be
// replaced and why. Unwrapping that would reduce the whole outcome to the bare
// reason the backup failed, leaving the user unable to tell a replacement that
// was refused because the old file could not be kept from a copy that failed
// some other way. The type is recognised rather than its wording, so fs may
// reword the message without the context silently disappearing.
func readableOpError(err error) string {
	if err == nil {
		return ""
	}
	var refused *fs.RefusedReplacement
	if errors.As(err, &refused) {
		return refused.Error()
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) && linkErr.Err != nil {
		err = linkErr.Err
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) && pathErr.Err != nil {
		err = pathErr.Err
	}
	return err.Error()
}

// opItem is one entry in a batch: its display name, source, and destination.
type opItem struct {
	name string
	src  string
	dst  string
}

// singularResult builds the one-entry result of an operation that finished
// inline, such as creating or renaming an entry.
func singularResult(verb, name string, err error) fileOpMsg {
	if err != nil {
		return fileOpMsg{verb: verb, failures: []opFailure{{name: name, err: err}}}
	}
	return fileOpMsg{verb: verb, name: name, succeeded: 1}
}

// copyProgressMsg reports one chunk of an in-flight copy and asks for the next
// step, or signals completion.
type copyProgressMsg struct {
	copier *fs.Copier
	done   bool
	err    error
	// name is the file the step was working on when it failed, captured
	// before Step ran because a failed Step drops the copier's pointer to
	// the current entry.
	name string
	// path is where that file was read from, so a failed transfer can put
	// the entry back for another try.
	path string
	// gen identifies the copy this message belongs to.
	//
	// A copy that was cancelled still has a step in flight, and its message
	// arrives after the user has moved on. Without a generation to check it
	// against, the message would adopt its dead copier, block the keys
	// again and report a copy that never finished.
	gen int
	// move marks the transfer as a move rather than a copy. A move removes
	// the source once the copy has landed, so it is reported with its own
	// wording.
	move bool
}

// openMsg reports the result of opening a file with the desktop handler. It is
// deliberately not a fileOpMsg: that handler reloads both panes, which would
// reset the cursor every time a file was opened.
type openMsg struct {
	name string
	err  error
}

type themeReloadedMsg struct {
	theme config.Theme
	err   error
	// closed reports that the watcher was shut down and cannot be waited on
	// again. Any other error is transient, and the wait is re-armed so one
	// inotify hiccup does not end hot-reload for the rest of the session.
	closed bool
}

// fuzzyTickMsg drives the spinner while a walk is in progress. The animation
// state lives on the model because the finder widget is rebuilt on every frame
// and cannot hold it itself.
type fuzzyTickMsg struct{}

// splashTickMsg advances the version splash animation in the bottom bar.
type splashTickMsg struct{}

// previewLoadedMsg carries a preview that was gathered in the background.
type previewLoadedMsg struct {
	// path is the file the preview belongs to. A preview for a file the
	// cursor has already left is stale and is dropped.
	path string
	// stamp identifies the file's contents at the time it was read: its size
	// and modification time. A preview keyed on the path alone would keep
	// showing a file that has since been edited, overwritten or replaced.
	stamp   string
	content ui.Content
}

// fuzzyFilesLoadedMsg reports the files a walk found.
type fuzzyFilesLoadedMsg struct {
	files []string
	// truncated reports that the walk stopped at its limit and the tree
	// holds more than was collected.
	truncated bool
	// root identifies which walk produced files. A walk from a directory the
	// user has since left can finish after the current one, and applying it
	// would replace the live results with a stale tree.
	root string
}

// fuzzyResultsMsg carries results that were filtered in the background.
type fuzzyResultsMsg struct {
	// query is the query the results belong to. Results for a query the
	// user has since changed are dropped.
	query string
	// results are the matching paths, best first.
	results []string
	// gen identifies the filter run. A slow run that finishes after a
	// faster one must not overwrite the faster one's results.
	gen int
}

type Clipboard struct {
	Action string
	Files  []fs.Entry
	Source string
}

// inputMode is the active single-line text prompt. Filter, add, rename, and goto all
// share one capture path: type to edit, enter commits, esc cancels.
type inputMode int

const (
	inputNone inputMode = iota
	inputFilter
	inputAdd
	inputRename
	inputGoto
)

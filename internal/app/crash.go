package app

import (
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// crashBudget is how many *distinct* crash sites the app tolerates in one
// session before it gives up and exits.
//
// Repeats of the same site are free. A panic that recurs on the next frame
// is exactly what the recovery exists for: the app redraws and carries on.
// Only a spread of unrelated failures — a sign the state is genuinely
// inconsistent — is worth ending the session over.
const crashBudget = 3

// crashHistory is how many recent reports are kept for the exit report.
const crashHistory = 8

// crashReport describes one recovered panic.
type crashReport struct {
	// Where names the code path that panicked, e.g. "update" or "load-dir".
	Where string
	// Err is the recovered value rendered as an error.
	Err error
	// Stack is the goroutine stack at the point of the panic.
	Stack []byte
	// When is the time the panic was recovered.
	When time.Time
}

// fingerprint identifies a crash site so the same failure is not counted
// against the budget twice.
func (r crashReport) fingerprint() string {
	return r.Where + "|" + fmt.Sprint(r.Err)
}

// crashState holds the session's crash history.
//
// Model is copied by value on every update, so this is reached through a
// pointer: a crash recorded during a render has to be visible to the render
// that follows it, and a crash recorded in a command goroutine has to be
// visible to the next Update.
type crashState struct {
	mu       sync.Mutex
	reports  []crashReport
	distinct map[string]bool
}

// ensureCrash gives the model a crash state if it does not have one yet.
//
// The constructor always sets one, but a model built by hand would otherwise
// silently lose every crash it recovered from — and with it the budget that
// decides when the session should end.
func (m *Model) ensureCrash() {
	if m.crash == nil {
		m.crash = &crashState{}
	}
}

// crashStateFor returns the model's crash state, creating one if needed.
//
// The assignment lands on the caller's copy, so a caller that returns the
// model (Update) keeps the state; a caller that does not (View) loses it.
// That is the price of Model being a value type, and it only bites for a
// model that was not built by the constructor.
func (m *Model) crashStateFor() *crashState {
	m.ensureCrash()
	return m.crash
}

// record files a crash and keeps the history bounded.
func (c *crashState) record(r crashReport) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.distinct == nil {
		c.distinct = make(map[string]bool)
	}
	c.distinct[r.fingerprint()] = true
	c.reports = append(c.reports, r)
	if len(c.reports) > crashHistory {
		c.reports = c.reports[len(c.reports)-crashHistory:]
	}
}

// count is the total number of crashes recorded this session.
func (c *crashState) count() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.reports)
}

// distinctCount is the number of different crash sites seen this session.
func (c *crashState) distinctCount() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.distinct)
}

// exhausted reports whether the crash budget has been spent.
func (c *crashState) exhausted() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.distinct) > crashBudget
}

// history returns a copy of the recorded reports, oldest first.
func (c *crashState) history() []crashReport {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]crashReport, len(c.reports))
	copy(out, c.reports)
	return out
}

// panicMsg carries a panic out of a command goroutine.
//
// Bubble Tea recovers panics in commands, but recovering there ends the
// program. Turning the panic into a message instead lets the session
// continue: the command's work is abandoned, the error is reported, and the
// user keeps their panes.
type panicMsg struct {
	report crashReport
}

// newCrashReport captures a recovered panic together with its stack.
func newCrashReport(where string, r any) crashReport {
	return crashReport{
		Where: where,
		Err:   fmt.Errorf("%v", r),
		Stack: debug.Stack(),
		When:  time.Now(),
	}
}

// safeCmd wraps a command so a panic inside it becomes a message rather than
// the end of the program.
//
// The command's own work is lost, which is the correct trade: a half-finished
// filesystem operation is abandoned rather than allowed to corrupt state, and
// the user is told what happened.
func safeCmd(where string, fn tea.Cmd) tea.Cmd {
	if fn == nil {
		return nil
	}
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				msg = panicMsg{report: newCrashReport(where, r)}
			}
		}()
		return fn()
	}
}

// crashError is the one-line summary shown in the bottom bar.
func crashError(r crashReport, count int) error {
	msg := fmt.Sprintf("internal error in %s: %v", r.Where, r.Err)
	if count > 1 {
		msg += fmt.Sprintf(" (%d crashes this session)", count)
	}
	return errors.New(msg)
}

// crashView is the fallback frame drawn when the normal render path panics.
//
// It builds nothing but plain strings, so it cannot fail the same way the
// real view did. The session stays alive and the user can quit at their
// leisure rather than being dropped back into a broken shell.
func (m Model) crashView(r crashReport) (v tea.View) {
	// A panic while reporting a panic must not take the process down. This
	// defer is the outermost guard: it catches anything the body below throws.
	defer func() {
		if rec := recover(); rec != nil {
			v = tea.NewView("omakaiju: internal error (recovered)")
		}
	}()
	return tea.NewView(m.crashViewText(r))
}

// crashViewText is the body of the fallback frame.
func (m Model) crashViewText(r crashReport) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(m.theme.PathText().Render("omakaiju hit an internal error and recovered"))
	b.WriteString("\n\n")
	b.WriteString("Your files were not touched. Press q to quit.\n\n")
	b.WriteString(m.theme.ErrorText().Render("crash: " + r.Where + ": " + fmt.Sprint(r.Err)))
	b.WriteString("\n")
	if n := m.crash.count(); n > 1 {
		b.WriteString(m.theme.StatusText().Render(fmt.Sprintf("%d crashes so far this session", n)))
		b.WriteString("\n")
	}
	return b.String()
}

// CrashReport is the exported view of one recovered panic, used by the exit
// report and by tests.
type CrashReport struct {
	// Where names the code path that panicked.
	Where string
	// Err is the recovered value rendered as a string.
	Err string
	// Stack is the goroutine stack at the point of the panic.
	Stack string
	// When is the time the panic was recovered.
	When time.Time
}

// CrashReports returns the crashes recovered this session, oldest first.
func (m Model) CrashReports() []CrashReport {
	history := m.crash.history()
	out := make([]CrashReport, 0, len(history))
	for _, r := range history {
		out = append(out, CrashReport{
			Where: r.Where,
			Err:   fmt.Sprint(r.Err),
			Stack: string(r.Stack),
			When:  r.When,
		})
	}
	return out
}

// Crashed reports whether the app recovered from a panic this session.
func (m Model) Crashed() bool {
	return m.crash.count() > 0
}

// FormatCrashReports renders the crash history for the terminal, so a user
// who hits a bug can report exactly what went wrong.
func FormatCrashReports(reports []CrashReport) string {
	if len(reports) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("omakaiju recovered from %d internal error(s):\n", len(reports)))
	for i, r := range reports {
		fmt.Fprintf(&b, "\n%d) %s: %s\n", i+1, r.Where, r.Err)
		if r.Stack != "" {
			b.WriteString(r.Stack)
			b.WriteString("\n")
		}
	}
	return b.String()
}

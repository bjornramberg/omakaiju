package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/bjornramberg/omakaiju/internal/app"
	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/version"

	tea "charm.land/bubbletea/v2"
)

func main() {
	// The moment the program was entered, which is the fallback for when the
	// process started. The kernel's own record is better and is asked for
	// below, but this is always available.
	entered := time.Now()

	// A panic that escapes the program would leave the terminal in raw mode
	// with no echo, which reads as a dead shell to the user. Restoring it
	// here is the last line of defence; the app's own recovery handles the
	// panics it can see.
	defer func() {
		if r := recover(); r != nil {
			restoreTerminal()
			fmt.Fprintf(os.Stderr, "omakaiju: internal error: %v\n\n%s\n", r, debug.Stack())
			os.Exit(1)
		}
	}()

	// The version is stamped at build time. Printing it is how a user
	// tells a bug report which release they are running.
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-v", "--version":
			printVersion(os.Stdout)
			return
		}
	}

	// --default is a setup command, not a session: handle it before the
	// theme and the terminal are touched, and leave without starting the TUI.
	if handled, err := maybeMakeDefault(os.Args[1:], os.Stdin, os.Stdout); handled {
		if err != nil {
			fmt.Fprintf(os.Stderr, "omakaiju: %v\n", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "omakaiju: %v\n", err)
		os.Exit(1)
	}

	// The tracker is the program's output as well as its stopwatch: the
	// moment the first frame reaches the terminal is the moment the startup
	// is over.
	tracker := app.NewStartupTracker(os.Stdout, app.ProcessStart(entered))

	m := app.NewModel(cfg, app.WithStartup(tracker))
	p := tea.NewProgram(m, tea.WithOutput(tracker))

	// SIGHUP is what a terminal sends when the window closes. Bubble Tea
	// handles SIGINT and SIGTERM but not SIGHUP, so without this the shell
	// would be left without its line discipline.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		// A goroutine the app owns sits outside Bubble Tea's panic net, so
		// it carries its own recover: a panic here would kill the process
		// with the terminal still in raw mode.
		defer func() {
			_ = recover()
		}()
		<-hup
		restoreTerminal()
		os.Exit(1)
	}()

	final, err := p.Run()
	if err != nil {
		// Bubble Tea restores the terminal on the paths it owns, but an
		// error that escaped the program may not have gone through one.
		restoreTerminal()
		fmt.Fprintf(os.Stderr, "omakaiju: %v\n", err)
		os.Exit(1)
	}

	// A session that recovered from crashes still worked, but the user
	// deserves the details: the stack is what a bug report needs.
	if fm, ok := final.(app.Model); ok {
		if reports := fm.CrashReports(); len(reports) > 0 {
			restoreTerminal()
			fmt.Fprint(os.Stderr, app.FormatCrashReports(reports))
		}
	}
}

// restoreTerminal puts the terminal back into a usable state: cursor
// visible, alt screen left, attributes reset.
//
// It is written to stderr rather than stdout because stdout may have been
// redirected, and the reset has to reach the terminal itself. It is safe to
// call more than once and safe to call when the terminal was never put into
// raw mode.
func restoreTerminal() {
	restoreTerminalTo(os.Stderr)
}

// restoreTerminalTo writes the reset to w. It is the same reset, with the
// writer chosen by the caller, so the behaviour can be tested without
// touching the real terminal.
func restoreTerminalTo(w io.Writer) {
	fmt.Fprint(w, "\x1b[?25h\x1b[?1049l\x1b[0m\r\n")
}

// printVersion writes the release the binary was built from. It is what a
// bug report quotes, so it says the version and nothing else.
func printVersion(w io.Writer) {
	fmt.Fprintln(w, "omakaiju "+version.Display())
}

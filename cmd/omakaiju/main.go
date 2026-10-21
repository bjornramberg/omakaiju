package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/bjornramberg/omakaiju/internal/app"
	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/version"

	tea "charm.land/bubbletea/v2"
)

func main() {
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
			fmt.Println("omakaiju " + version.Display())
			return
		}
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "omakaiju: %v\n", err)
		os.Exit(1)
	}

	m := app.NewModel(cfg)
	p := tea.NewProgram(m)

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
	fmt.Fprint(os.Stderr, "\x1b[?25h\x1b[?1049l\x1b[0m\r\n")
}

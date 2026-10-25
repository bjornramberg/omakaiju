package fs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// openers are tried in order.
//
// gio resolves the file's content type from its name as well as its contents, so
// a .md file is handed to its markdown handler. xdg-open sniffs the contents
// alone and can settle on the generic text/plain handler, which here is nvim: a
// terminal application that has no terminal to attach to once detached, so it
// exits immediately and nothing visible happens. That is why opening markdown
// worked through a desktop file manager but not from here.
var openers = [][]string{
	{"gio", "open"},
	{"xdg-open"},
}

// openRunner launches a command. It is a variable so tests can replace the real
// launcher rather than spawning programs.
var openRunner = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	// The handler outlives the TUI, so it must not inherit the terminal or hold
	// the process open waiting for it.
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return err
	}
	// Wait in the background. A handler may stay up for as long as the user
	// keeps its window open, so waiting inline would freeze the TUI. Releasing
	// the child instead, as this used to, leaves it behind as a zombie for the
	// rest of the session. A goroutine blocks neither the caller nor exit: Go
	// does not wait for goroutines when main returns. The exit status is of no
	// interest—Start succeeding is all that launching the opener promises.
	go func() { _ = cmd.Wait() }()
	return nil
}

// handlerArgument turns path into an argument the desktop handler cannot read
// as an option. Such a name is only reachable as a relative path, since
// absolute paths begin with "/"; prefixing it with "./" makes it unambiguously
// a path.
//
// Neither opener can be relied on to accept a "--" separator instead:
//
//   - gio parses its arguments with GOption. A "--" is normally stripped, but
//     GOption keeps it in the argument list when the following argument still
//     begins with a dash, so "gio open -- --help" tries to open a file named
//     "--" as well as the file "--help".
//   - xdg-open rejects any argument beginning with a dash, including "--"
//     ("unexpected option"), unless XDG_UTILS_ENABLE_DOUBLE_HYPEN is set. Its
//     manual recommends an absolute path or a "./" prefix for relative ones.
//
// The "./" prefix is accepted by both and leaves ordinary names untouched.
func handlerArgument(path string) string {
	if strings.HasPrefix(path, "-") {
		return "./" + path
	}
	return path
}

// OpenFile asks the desktop to open path with its default handler.
//
// A successful return means the handler was launched, not that it stayed up:
// desktop launchers background their child and exit immediately, so anything
// that fails afterwards is invisible here.
func OpenFile(path string) error {
	if _, err := os.Lstat(path); err != nil {
		return err
	}

	var lastErr error
	for _, opener := range openers {
		if _, err := exec.LookPath(opener[0]); err != nil {
			lastErr = fmt.Errorf("%s: %w", opener[0], err)
			continue
		}

		args := append(append([]string{}, opener[1:]...), handlerArgument(path))
		if err := openRunner(opener[0], args...); err != nil {
			lastErr = fmt.Errorf("%s: %w", opener[0], err)
			continue
		}
		return nil
	}

	return fmt.Errorf("no desktop handler available: %w", lastErr)
}

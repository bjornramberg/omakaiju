package fs

import (
	"fmt"
	"os"
	"os/exec"
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
	// Release the child so it becomes a zombie reaped by init rather than
	// lingering; the TUI has no interest in its exit status.
	return cmd.Process.Release()
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

		args := append(append([]string{}, opener[1:]...), path)
		if err := openRunner(opener[0], args...); err != nil {
			lastErr = fmt.Errorf("%s: %w", opener[0], err)
			continue
		}
		return nil
	}

	return fmt.Errorf("no desktop handler available: %w", lastErr)
}

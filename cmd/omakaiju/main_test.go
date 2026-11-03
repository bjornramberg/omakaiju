package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/version"
)

func TestPrintVersionSaysTheRelease(t *testing.T) {
	var buf bytes.Buffer
	printVersion(&buf)

	got := buf.String()
	want := "omakaiju " + version.Display() + "\n"
	if got != want {
		t.Errorf("printVersion wrote %q, want %q", got, want)
	}
}

func TestRestoreTerminalResetsTheTerminal(t *testing.T) {
	var buf bytes.Buffer
	restoreTerminalTo(&buf)

	// The cursor is shown, the alternate screen is left, and the attributes
	// are reset: a terminal left in raw mode with the alt screen up reads as
	// a dead shell to the user.
	got := buf.String()
	for _, want := range []string{"\x1b[?25h", "\x1b[?1049l", "\x1b[0m"} {
		if !strings.Contains(got, want) {
			t.Errorf("the reset does not contain %q: %q", want, got)
		}
	}
}

// The version flag is how a bug report says which release it is about, so
// the real binary is run: the argument parsing, the output and the exit
// code all have to be right, not just the function behind them.
func TestVersionFlagRunsAndExitsCleanly(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "omakaiju")

	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the binary failed: %v\n%s", err, out)
	}

	for _, flag := range []string{"-v", "--version"} {
		cmd := exec.Command(bin, flag)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()

		if err != nil {
			t.Errorf("%s exited with %v (stderr: %q)", flag, err, stderr.String())
			continue
		}
		got := strings.TrimSpace(stdout.String())
		if !strings.HasPrefix(got, "omakaiju ") {
			t.Errorf("%s printed %q, want the version", flag, got)
		}
		if stderr.Len() != 0 {
			t.Errorf("%s wrote to stderr: %q", flag, stderr.String())
		}
	}
}

// --default is a setup command: it must not load the theme or start the TUI,
// and it has to write the binding and exit cleanly.
func TestDefaultFlagRunsAndExitsCleanly(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "omakaiju")

	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the binary failed: %v\n%s", err, out)
	}

	env := append(os.Environ(), "XDG_CONFIG_HOME="+dir)
	cmd := exec.Command(bin, "--default", "--yes")
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("--default --yes exited with %v (stderr: %q)", err, stderr.String())
	}

	data, err := os.ReadFile(filepath.Join(dir, "hypr", "bindings.lua"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"omarchy launch tui omakaiju"`) {
		t.Errorf("--default did not write the binding:\n%s", data)
	}

	cmd = exec.Command(bin, "--default")
	cmd.Env = env
	cmd.Stdin = strings.NewReader("n\n")
	stdout.Reset()
	stderr.Reset()
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("--default exited with %v (stderr: %q)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "run omakaiju --default or edit the ~/.config/hypr/bindings.lua") {
		t.Errorf("a no answer did not give the way to change it later: %q", stdout.String())
	}
}

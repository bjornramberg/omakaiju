package fs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withStubbedRunner swaps the launcher for the duration of a test.
func withStubbedRunner(t *testing.T, fn func(name string, args ...string) error) *[]string {
	t.Helper()
	var captured []string
	orig := openRunner
	openRunner = func(name string, args ...string) error {
		captured = append(captured, name)
		captured = append(captured, args...)
		return fn(name, args...)
	}
	t.Cleanup(func() { openRunner = orig })
	return &captured
}

func TestOpenFileInvokesHandlerWithPath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := withStubbedRunner(t, func(string, ...string) error { return nil })

	if err := OpenFile(file); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 3 {
		t.Fatalf("expected gio, open, and the path; got %v", *got)
	}
	if !strings.HasSuffix((*got)[0], "gio") {
		t.Errorf("handler = %q, want gio first so content types resolve by name", (*got)[0])
	}
	if (*got)[1] != "open" {
		t.Errorf("subcommand = %q, want open", (*got)[1])
	}
	if (*got)[2] != file {
		t.Errorf("argument = %q, want %q", (*got)[2], file)
	}
}

// gio resolves a file's content type from its name as well as its contents.
// Falling back to xdg-open first would send a .md to whatever handles generic
// text, which on a desktop configured for a terminal editor means nothing
// visible happens.
func TestOpenFilePrefersGioOverXdgOpen(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(file, []byte("# hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	var names []string
	orig := openRunner
	openRunner = func(name string, args ...string) error {
		names = append(names, name)
		return nil
	}
	t.Cleanup(func() { openRunner = orig })

	if err := OpenFile(file); err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 {
		t.Fatalf("expected one launch, got %v", names)
	}
	if strings.HasSuffix(names[0], "xdg-open") {
		t.Error("xdg-open was used even though gio is available")
	}
}

// If the preferred opener is missing, the next one is tried rather than giving
// up, and a launch failure falls through too.
func TestOpenFileFallsBackToNextOpener(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var attempts []string
	orig := openRunner
	openRunner = func(name string, args ...string) error {
		attempts = append(attempts, name)
		if strings.HasSuffix(name, "gio") {
			return errors.New("delegate failed")
		}
		return nil
	}
	t.Cleanup(func() { openRunner = orig })

	if err := OpenFile(file); err != nil {
		t.Fatalf("should fall through to the next opener, got %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("expected two attempts, got %v", attempts)
	}
	if !strings.HasSuffix(attempts[1], "xdg-open") {
		t.Errorf("fallback = %q, want xdg-open", attempts[1])
	}
}

func TestOpenFilePropagatesLaunchFailure(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	withStubbedRunner(t, func(string, ...string) error { return errors.New("boom") })

	if err := OpenFile(file); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected the launch error, got %v", err)
	}
}

func TestOpenFileMissingTargetErrors(t *testing.T) {
	withStubbedRunner(t, func(string, ...string) error { return nil })

	err := OpenFile(filepath.Join(t.TempDir(), "nope.txt"))
	if err == nil {
		t.Fatal("opening a missing file should error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected a not-exist error, got %v", err)
	}
}

// With no handler on PATH the failure should say so plainly, rather than
// surfacing an obscure exec failure.
func TestOpenFileWithoutHandlerReportsClearly(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", "")
	withStubbedRunner(t, func(string, ...string) error {
		t.Fatal("runner should not be reached without a handler")
		return nil
	})

	err := OpenFile(file)
	if err == nil {
		t.Fatal("expected an error with no handler available")
	}
	if !strings.Contains(err.Error(), "handler") {
		t.Errorf("error should mention the missing handler, got %v", err)
	}
}

// A path with spaces and quotes must reach the handler as a single argument,
// since it is never passed through a shell.
func TestOpenFilePassesPathVerbatim(t *testing.T) {
	dir := t.TempDir()
	weird := filepath.Join(dir, "a file with 'quotes' & $vars.txt")
	if err := os.WriteFile(weird, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := withStubbedRunner(t, func(string, ...string) error { return nil })

	if err := OpenFile(weird); err != nil {
		t.Fatal(err)
	}
	if (*got)[len(*got)-1] != weird {
		t.Errorf("argument = %q, want the path verbatim", (*got)[len(*got)-1])
	}
}

func TestOpenFileHandlesDirectory(t *testing.T) {
	dir := t.TempDir()
	got := withStubbedRunner(t, func(string, ...string) error { return nil })

	if err := OpenFile(dir); err != nil {
		t.Errorf("opening a directory should be allowed, got %v", err)
	}
	if (*got)[len(*got)-1] != dir {
		t.Errorf("argument = %q, want %q", (*got)[len(*got)-1], dir)
	}
}

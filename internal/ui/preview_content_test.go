package ui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bjornramberg/omakaiju/internal/config"
)

// A large text file must not be read or highlighted in full. The preview keeps
// at most previewTextBytes of source, so its own Content stays small and it
// returns in bounded time even when the file is many megabytes.
func TestLargeFilePreviewIsBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dense.go")

	// Dense tokens maximise the highlighter's output relative to the source,
	// which is the case that used to explode. The lines are long enough that
	// a thousand of them would far exceed the byte cap, so the test exercises
	// the byte bound rather than the line bound.
	line := strings.Repeat("func f(x int) string { return \"abcdefghij\" } // dense tokens ", 16) + "\n"
	data := strings.Repeat(line, (4<<20)/len(line)+1)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	start := time.Now()
	c := Load(path, config.DefaultTheme())
	elapsed := time.Since(start)

	// Generous enough for a slow machine, finite enough to catch a preview
	// that reads and highlights the whole file.
	if elapsed > 30*time.Second {
		t.Fatalf("loading a %d-byte file took %v, want a bounded load", len(data), elapsed)
	}
	if c.Kind != KindText {
		t.Fatalf("Kind = %v, want KindText", c.Kind)
	}
	if !c.TextTruncated {
		t.Error("TextTruncated = false, want true for a file over the cap")
	}
	if len(c.Text) >= previewTextLines {
		t.Errorf("retained %d lines, want the byte cap to stop the read before the line cap of %d", len(c.Text), previewTextLines)
	}

	var sourceBytes int
	for _, l := range c.Text {
		sourceBytes += len(l)
	}
	if sourceBytes > previewTextBytes {
		t.Errorf("retained %d bytes of source, want at most %d", sourceBytes, previewTextBytes)
	}
	if joined := strings.Join(c.Text, "\n"); len(joined) > previewTextBytes {
		t.Errorf("retained %d joined bytes of source, want at most %d", len(joined), previewTextBytes)
	}

	// Highlighting legitimately adds ANSI, but it must be a multiple of the
	// bounded source, not of the original file.
	if len(c.Highlighted) == 0 {
		t.Fatal("expected highlighted output for Go source")
	}
	var highlightedBytes int
	for _, l := range c.Highlighted {
		highlightedBytes += len(l)
	}
	if highlightedBytes > 20*previewTextBytes {
		t.Errorf("retained %d bytes of highlighted text, want at most %d", highlightedBytes, 20*previewTextBytes)
	}

	p := NewPreview(80, 24, config.DefaultTheme()).SetPath(path)
	out := stripANSI(p.RenderContent(c))
	if !strings.Contains(out, "cut short") {
		t.Errorf("preview footer does not say the file was cut short:\n%s", out)
	}
}

// An unreadable file is an error, not an empty text file. The reader's error
// must survive into the Content so the panel can report it.
func TestUnreadableFileBecomesErrorPreview(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file permission bits do not prevent reads")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "locked.txt")
	if err := os.WriteFile(path, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatalf("chmod test file: %v", err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o600) })

	c := Load(path, config.DefaultTheme())
	if c.Kind != KindError {
		t.Fatalf("Kind = %v, want KindError (Err = %v)", c.Kind, c.Err)
	}
	if c.Err == nil {
		t.Fatal("Err = nil, want the read failure")
	}

	p := NewPreview(80, 16, config.DefaultTheme()).SetPath(path)
	if out := stripANSI(p.RenderContent(c)); !strings.Contains(out, "Error:") {
		t.Errorf("preview does not report the error:\n%s", out)
	}
}

// A FIFO opened as a stream blocks forever waiting for a writer. The preview
// must refuse it by name before opening it.
func TestHexDumpRefusesFIFO(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.bin")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo is unavailable: %v", err)
	}

	done := make(chan Content, 1)
	go func() { done <- Load(path, config.DefaultTheme()) }()

	select {
	case c := <-done:
		if c.Kind != KindError {
			t.Fatalf("Kind = %v, want KindError", c.Kind)
		}
		if c.Err == nil || !strings.Contains(c.Err.Error(), "regular file") {
			t.Fatalf("Err = %v, want an error naming the non-regular file", c.Err)
		}
		if !strings.Contains(c.Err.Error(), path) {
			t.Errorf("error %q does not name the path %q", c.Err, path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("preview blocked on a FIFO instead of refusing it")
	}
}

// The text reader has the same guard, so a FIFO with a text extension is
// refused as well.
func TestTextPreviewRefusesFIFO(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo is unavailable: %v", err)
	}

	done := make(chan Content, 1)
	go func() { done <- Load(path, config.DefaultTheme()) }()

	select {
	case c := <-done:
		if c.Kind != KindError || c.Err == nil {
			t.Fatalf("Kind = %v, Err = %v, want an error preview", c.Kind, c.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("text preview blocked on a FIFO instead of refusing it")
	}
}

// A normal file previews its contents and says how many lines it has; a file
// longer than the panel says how many it left out. That is the live path's
// contract, and it must not change when the duplicate reader goes away.
func TestTextPreviewShowsContentAndLineCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	c := Load(path, config.DefaultTheme())
	if c.Kind != KindText {
		t.Fatalf("Kind = %v, want KindText (Err = %v)", c.Kind, c.Err)
	}
	if c.TextTruncated {
		t.Error("TextTruncated = true for a file under the cap")
	}
	if got := strings.Join(c.Text, "\n"); got != "alpha\nbeta\ngamma" {
		t.Errorf("Text = %q, want the file's three lines", got)
	}

	p := NewPreview(80, 24, config.DefaultTheme()).SetPath(path)
	out := stripANSI(p.RenderContent(c))
	for _, want := range []string{"small.txt", "alpha", "beta", "gamma", "3 lines"} {
		if !strings.Contains(out, want) {
			t.Errorf("preview is missing %q:\n%s", want, out)
		}
	}

	// More lines than the panel can show: the body is capped and the footer
	// counts what was left out.
	var many []string
	for i := 0; i < 60; i++ {
		many = append(many, fmt.Sprintf("line %02d", i))
	}
	maxVisible := 24 - 8
	out = stripANSI(p.RenderContent(Content{Kind: KindText, Text: many}))
	if want := fmt.Sprintf("... (%d more lines)", len(many)-maxVisible); !strings.Contains(out, want) {
		t.Errorf("preview footer is missing %q:\n%s", want, out)
	}
	if !strings.Contains(out, "line 00") {
		t.Errorf("preview does not show the first lines:\n%s", out)
	}
	if strings.Contains(out, fmt.Sprintf("line %02d", maxVisible)) {
		t.Errorf("preview drew line %d, past the panel's visible rows:\n%s", maxVisible, out)
	}
}

// internal/ui must have one way to read a text preview, and it must be the
// bounded reader. A second, unbounded reader is how the safe one rots: a byte
// cap was added to one path while the other kept reading a thousand lines of
// any size. This scans the package's non-test source for the unbounded
// fs.ReadFileHead call, so reintroducing it fails here.
func TestPreviewHasOnlyTheBoundedReader(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fset := token.NewFileSet()
	parsed := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed++
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if ok && ident.Name == "fs" && sel.Sel.Name == "ReadFileHead" {
				t.Errorf("%s calls fs.ReadFileHead, an unbounded preview reader; use Load/readTextHead instead", name)
			}
			return true
		})
	}
	// Guard the guard: if the scan saw no source files it proves nothing.
	if parsed == 0 {
		t.Fatal("scanned no package source files")
	}
}

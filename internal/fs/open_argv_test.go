package fs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// openerCase describes one of the real openers and the arguments it takes
// before the path.
var openerCases = []struct {
	name string
	argv []string
}{
	{"gio", []string{"gio", "open"}},
	{"xdg-open", []string{"xdg-open"}},
}

// installFakeHandler writes an executable shell script called name into a
// directory of its own and points PATH at that directory, so OpenFile's real
// opener table and argument construction are exercised without a desktop
// session.
func installFakeHandler(t *testing.T, name, body string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

// recordHandlerArgs installs a fake handler that writes its own name and every
// argument it received to $RECORD, one per line, and returns the file it
// writes.
func recordHandlerArgs(t *testing.T, name string) string {
	t.Helper()
	record := filepath.Join(t.TempDir(), "args")
	t.Setenv("RECORD", record)
	installFakeHandler(t, name, "printf '%s\\n' \"${0##*/}\" \"$@\" > \"$RECORD\"\n")
	return record
}

// readHandlerArgs waits for the fake handler to finish recording and returns
// what it received.
func readHandlerArgs(t *testing.T, record string) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(record)
		if err == nil && len(data) > 0 {
			return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		}
		if time.Now().After(deadline) {
			t.Fatalf("handler recorded no arguments: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A file whose name begins with a dash must reach the handler as a path. Passed
// verbatim, gio and xdg-open both read it as an option: "--help" prints help,
// "-a" is rejected outright.
func TestOpenFilePassesDashNameAsPath(t *testing.T) {
	for _, opener := range openerCases {
		for _, name := range []string{"-a", "--help"} {
			t.Run(opener.name+"/"+name, func(t *testing.T) {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Chdir(dir)

				record := recordHandlerArgs(t, opener.name)
				if err := OpenFile(name); err != nil {
					t.Fatalf("OpenFile(%q): %v", name, err)
				}

				got := readHandlerArgs(t, record)
				if len(got) != len(opener.argv)+1 {
					t.Fatalf("handler arguments = %q, want %q plus the path", got, opener.argv)
				}
				for i, want := range opener.argv {
					if got[i] != want {
						t.Errorf("handler argument %d = %q, want %q", i, got[i], want)
					}
				}

				arg := got[len(got)-1]
				if strings.HasPrefix(arg, "-") {
					t.Fatalf("handler received %q, which it reads as an option", arg)
				}
				opened, err := os.Stat(arg)
				if err != nil {
					t.Fatalf("argument %q does not name a file: %v", arg, err)
				}
				want, err := os.Stat(name)
				if err != nil {
					t.Fatal(err)
				}
				if !os.SameFile(opened, want) {
					t.Errorf("argument %q does not name %q", arg, name)
				}
			})
		}
	}
}

// The remedy for dash names must not disturb ordinary ones.
func TestOpenFilePassesOrdinaryNameUnchanged(t *testing.T) {
	for _, opener := range openerCases {
		t.Run(opener.name, func(t *testing.T) {
			dir := t.TempDir()
			const name = "doc.txt"
			if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Chdir(dir)

			record := recordHandlerArgs(t, opener.name)
			if err := OpenFile(name); err != nil {
				t.Fatalf("OpenFile(%q): %v", name, err)
			}

			got := readHandlerArgs(t, record)
			if len(got) != len(opener.argv)+1 {
				t.Fatalf("handler arguments = %q, want %q plus the path", got, opener.argv)
			}
			if arg := got[len(got)-1]; arg != name {
				t.Errorf("argument = %q, want %q unchanged", arg, name)
			}
		})
	}
}

// A handler that exits immediately must not be left behind as a zombie.
// Process.Release dropped the handle without waiting, so the child stayed in
// the process table for the rest of the session.
func TestOpenFileReapsHandlerProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the child's state is read from /proc, which only Linux provides")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	pidFile := filepath.Join(dir, "handler.pid")
	t.Setenv("PIDFILE", pidFile)
	installFakeHandler(t, "gio", "printf '%s\\n' \"$$\" > \"$PIDFILE\"\n")

	if err := OpenFile(target); err != nil {
		t.Fatalf("OpenFile(%q): %v", target, err)
	}

	pid := waitForHandlerPID(t, pidFile)
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, err := procState(pid)
		if err != nil {
			return // no /proc entry: the child was reaped
		}
		if time.Now().After(deadline) {
			t.Fatalf("handler process %d still exists in state %q after OpenFile returned; it was not reaped", pid, state)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForHandlerPID(t *testing.T, pidFile string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil {
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("handler never recorded its pid: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// procState returns the state character from /proc/<pid>/stat: "Z" for a
// zombie, "R"/"S"/... while it is still running.
func procState(pid int) (byte, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	end := bytes.LastIndexByte(data, ')')
	if end < 0 || end+2 >= len(data) {
		return 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	return data[end+2], nil
}

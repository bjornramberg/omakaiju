package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaybeMakeDefaultIgnoresOtherArguments(t *testing.T) {
	handled, err := maybeMakeDefault([]string{"some-directory"}, strings.NewReader(""), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Error("--default was not given, so the TUI should start")
	}
}

func TestMaybeMakeDefaultDeclinedSaysHowToChangeItLater(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	for _, answer := range []string{"", "n", "no"} {
		t.Run(answer, func(t *testing.T) {
			var out bytes.Buffer
			handled, err := maybeMakeDefault([]string{"--default"}, strings.NewReader(answer+"\n"), &out)
			if err != nil {
				t.Fatal(err)
			}
			if !handled {
				t.Fatal("--default should be handled")
			}
			if !strings.Contains(out.String(), "Would you like to make omakaiju the default file manager?") {
				t.Errorf("the question was not asked: %q", out.String())
			}
			if !strings.Contains(out.String(), "run omakaiju --default or edit the ~/.config/hypr/bindings.lua") {
				t.Errorf("the way to change it later was not given: %q", out.String())
			}
			if _, err := os.Stat(filepath.Join(dir, "hypr", "bindings.lua")); !os.IsNotExist(err) {
				t.Error("a declined question changed the bindings file")
			}
		})
	}
}

func TestMaybeMakeDefaultAcceptedWritesTheBinding(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	var out bytes.Buffer
	handled, err := maybeMakeDefault([]string{"--default"}, strings.NewReader("y\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("--default should be handled")
	}
	if !strings.Contains(out.String(), "now the default file manager") {
		t.Errorf("the outcome was not reported: %q", out.String())
	}

	data, err := os.ReadFile(filepath.Join(dir, "hypr", "bindings.lua"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"omarchy launch tui omakaiju"`) {
		t.Errorf("the binding was not written:\n%s", data)
	}
}

func TestMaybeMakeDefaultYesSkipsTheQuestion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	var out bytes.Buffer
	handled, err := maybeMakeDefault([]string{"--default", "--yes"}, strings.NewReader(""), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("--default should be handled")
	}
	if strings.Contains(out.String(), "Would you like") {
		t.Errorf("--yes should not ask: %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "hypr", "bindings.lua")); err != nil {
		t.Errorf("--yes did not write the binding: %v", err)
	}
}

package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/bjornramberg/omakaiju/internal/omarchy"
)

// defaultHint is what a user who declines is left with: two ways to change
// their mind later, without running the installer again.
const defaultHint = "If you change your mind, either run omakaiju --default or edit the ~/.config/hypr/bindings.lua file directly."

// maybeMakeDefault handles the --default flag, which is not part of the TUI.
// It reports whether the flag was present, so main can exit instead of
// starting the file manager.
func maybeMakeDefault(args []string, in io.Reader, out io.Writer) (bool, error) {
	makeDefault := false
	assumeYes := false
	for _, arg := range args {
		switch arg {
		case "--default":
			makeDefault = true
		case "-y", "--yes":
			assumeYes = true
		}
	}
	if !makeDefault {
		return false, nil
	}

	if !assumeYes {
		fmt.Fprint(out, "Would you like to make omakaiju the default file manager? [y/n] (enter = no) ")
		answer, err := readAnswer(in)
		if err != nil {
			return true, err
		}
		if !isYes(answer) {
			fmt.Fprintln(out, defaultHint)
			return true, nil
		}
	}

	path, err := omarchy.BindingsPath()
	if err != nil {
		return true, err
	}
	changed, err := omarchy.MakeDefault(path)
	if err != nil {
		return true, fmt.Errorf("could not update %s: %w", path, err)
	}
	if changed {
		fmt.Fprintln(out, "omakaiju is now the default file manager.")
	} else {
		fmt.Fprintln(out, "omakaiju is already the default file manager.")
	}
	return true, nil
}

func readAnswer(in io.Reader) (string, error) {
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func isYes(answer string) bool {
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")
}

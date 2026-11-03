// Package omarchy integrates omakaiju with the Omarchy desktop.
//
// Omarchy Quattro reads its keybindings from ~/.config/hypr/bindings.lua and
// loads the user's file after its own defaults. Making omakaiju the file
// manager is therefore a matter of unbinding the stock shortcut and binding
// it to omakaiju: later declarations win.
package omarchy

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// BindingKey is the Omarchy shortcut that opens the file manager.
const BindingKey = "SUPER + SHIFT + F"

// BindingsFile is the user's Omarchy Quattro keybinding file, relative to the
// configuration directory.
var BindingsFile = filepath.Join("hypr", "bindings.lua")

// binding is the block appended to the user's bindings file. It is marked so
// it is obvious where the lines came from, and it unbinds the stock shortcut
// first because the user's file is loaded after Omarchy's defaults.
const binding = `-- omakaiju: use it as the default file manager.
hl.unbind("SUPER + SHIFT + F")
o.bind("SUPER + SHIFT + F", "File manager", "omarchy launch tui omakaiju")
`

// BindingsPath returns the path of the user's Omarchy keybinding file. It
// follows XDG_CONFIG_HOME when it is set, as Omarchy does.
func BindingsPath() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, BindingsFile), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", BindingsFile), nil
}

// MakeDefault appends the omakaiju file-manager binding to the bindings file
// at path, creating the file and its directory when they do not exist. It
// reports whether the file changed: a file that already binds omakaiju is
// left alone.
func MakeDefault(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if hasBinding(data) {
		return false, nil
	}

	section := binding
	if len(data) > 0 {
		section = "\n" + section
		if data[len(data)-1] != '\n' {
			section = "\n" + section
		}
	}

	mode := fs.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := writeAtomic(path, append(append([]byte{}, data...), section...), mode); err != nil {
		return false, err
	}
	return true, nil
}

// hasBinding reports whether the file already opens the file manager with
// omakaiju. It accepts any form of the binding, including one written by hand
// before this command existed, rather than only the block appended above.
func hasBinding(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "omakaiju") {
			continue
		}
		if strings.Contains(line, "o.bind") && strings.Contains(line, BindingKey) {
			return true
		}
	}
	return false
}

// writeAtomic writes data to path through a temporary file in the same
// directory, so a failure cannot leave a half-written bindings file.
func writeAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

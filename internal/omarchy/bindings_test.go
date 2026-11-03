package omarchy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMakeDefaultCreatesTheBindingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hypr", "bindings.lua")

	changed, err := MakeDefault(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("MakeDefault should report a change for a new file")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`hl.unbind("SUPER + SHIFT + F")`,
		`o.bind("SUPER + SHIFT + F", "File manager", "omarchy launch tui omakaiju")`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("the bindings file does not contain %q:\n%s", want, got)
		}
	}
}

func TestMakeDefaultLeavesAnExistingBindingAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bindings.lua")
	original := `o.bind("SUPER + SHIFT + F", "File manager", "omarchy launch tui omakaiju")` + "\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := MakeDefault(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("MakeDefault should not change a file that already binds omakaiju")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("the bindings file was changed:\n%s", got)
	}
}

func TestMakeDefaultIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bindings.lua")
	if _, err := MakeDefault(path); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	changed, err := MakeDefault(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("a second MakeDefault should not change the file")
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("a second MakeDefault changed the file:\n%s", second)
	}
}

func TestMakeDefaultKeepsExistingBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bindings.lua")
	original := "-- my bindings\no.bind(\"SUPER + B\", \"Browser\", { launch = \"chromium\" })"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := MakeDefault(path); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), original+"\n\n") {
		t.Errorf("the existing bindings were not kept as they were:\n%s", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("the file mode changed to %o, want 600", info.Mode().Perm())
	}
}

// A comment that mentions omakaiju is not a binding, so the command still has
// work to do.
func TestMakeDefaultIgnoresAMentionOutsideABinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bindings.lua")
	if err := os.WriteFile(path, []byte("-- omakaiju is installed here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := MakeDefault(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("a mere mention of omakaiju should not count as a binding")
	}
}

func TestBindingsPathFollowsXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	got, err := BindingsPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "hypr", "bindings.lua")
	if got != want {
		t.Errorf("BindingsPath() = %q, want %q", got, want)
	}
}

func TestBindingsPathFallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)

	got, err := BindingsPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "hypr", "bindings.lua")
	if got != want {
		t.Errorf("BindingsPath() = %q, want %q", got, want)
	}
}

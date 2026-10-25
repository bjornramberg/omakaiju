package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeSymlinkGuardTree builds the source tree the guard tests copy: one file
// at the top, one directory, and one file inside it.
func makeSymlinkGuardTree(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// symlinkOrSkip makes a symlink, skipping the test where the filesystem does
// not support one.
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
}

// assertSymlinkGuardTreeUntouched checks that a refused copy wrote nothing
// into the source: the files still hold what they held, and no copy of the
// tree appeared inside it.
func assertSymlinkGuardTreeUntouched(t *testing.T, src string) {
	t.Helper()
	for rel, want := range map[string]string{
		"a.txt":     "payload",
		"sub/b.txt": "nested",
	} {
		got, err := os.ReadFile(filepath.Join(src, rel))
		if err != nil {
			t.Errorf("%s is no longer readable: %v", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
	for _, dir := range []string{src, filepath.Join(src, "sub")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Errorf("cannot read %s: %v", dir, err)
			continue
		}
		for _, e := range entries {
			if e.Name() == "docs" {
				t.Errorf("the copy wrote %s into the source", filepath.Join(dir, e.Name()))
			}
		}
	}
}

// A destination directory that is a symlink into the source tree is the same
// directory as one inside it, however innocent its name looks. Comparing the
// paths as written let the copy through, and it wrote into the source and
// recursed into its own output until the depth bound stopped it.
func TestCopierRefusesSymlinkedDestinationInsideSource(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "docs")
	makeSymlinkGuardTree(t, src)

	link := filepath.Join(base, "innocent")
	symlinkOrSkip(t, filepath.Join(src, "sub"), link)

	_, err := NewCopier([]string{src}, link)
	if err == nil {
		t.Fatal("copying a directory into a symlink that points inside it should be refused")
	}
	if !strings.Contains(err.Error(), "into itself") {
		t.Errorf("error = %q, want it to say the copy is into itself", err)
	}
	assertSymlinkGuardTreeUntouched(t, src)
}

// The tree-copy path must refuse the same spelling. The link names the source
// root itself, so before the guard resolved it the copy tried to write the
// tree over itself and failed for the wrong reason.
func TestCopyRecursiveRefusesSymlinkedDestinationInsideSource(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "docs")
	makeSymlinkGuardTree(t, src)

	link := filepath.Join(base, "innocent")
	symlinkOrSkip(t, src, link)

	err := CopyRecursive(src, link)
	if err == nil {
		t.Fatal("copying a directory into a symlink that points at it should be refused")
	}
	if !strings.Contains(err.Error(), "into itself") {
		t.Errorf("error = %q, want it to say the copy is into itself", err)
	}
	assertSymlinkGuardTreeUntouched(t, src)
}

// A destination that does not exist yet is the normal case. Its deepest
// existing ancestor is still the symlink, and that ancestor resolves inside
// the source: the names the copy is about to create go inside too, so a check
// that only resolves paths that already exist in full would miss this.
func TestCopierRefusesUnexistingDestinationUnderSymlinkInsideSource(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "docs")
	makeSymlinkGuardTree(t, src)

	link := filepath.Join(base, "innocent")
	symlinkOrSkip(t, filepath.Join(src, "sub"), link)

	dstDir := filepath.Join(link, "newdir")
	if _, err := os.Lstat(dstDir); !os.IsNotExist(err) {
		t.Fatalf("the destination was supposed to be missing: %v", err)
	}

	_, err := NewCopier([]string{src}, dstDir)
	if err == nil {
		t.Fatal("copying into a new directory under a symlink into the source should be refused")
	}
	if !strings.Contains(err.Error(), "into itself") {
		t.Errorf("error = %q, want it to say the copy is into itself", err)
	}
	assertSymlinkGuardTreeUntouched(t, src)
}

// A ".." after a symlink names the target's parent, not the link's. Cleaning
// it before resolving the link would put the destination outside the source
// when it really lands inside.
func TestCopierRefusesDotDotAfterSymlinkIntoSource(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "docs")
	makeSymlinkGuardTree(t, src)

	link := filepath.Join(base, "innocent")
	symlinkOrSkip(t, filepath.Join(src, "sub"), link)

	// Written deliberately without filepath.Join, which would clean the
	// ".." away and hide the case from the guard. The filesystem reads this
	// as <src>/newdir: the link resolves to <src>/sub, and its parent is
	// <src>.
	dstDir := link + string(filepath.Separator) + ".." + string(filepath.Separator) + "newdir"

	_, err := NewCopier([]string{src}, dstDir)
	if err == nil {
		t.Fatal("a destination reached by a \"..\" after a symlink into the source should be refused")
	}
	if !strings.Contains(err.Error(), "into itself") {
		t.Errorf("error = %q, want it to say the copy is into itself", err)
	}
	assertSymlinkGuardTreeUntouched(t, src)
}

// A destination that is a symlink pointing outside the source is an ordinary
// place to copy to. Resolving the paths must not turn that into a refusal.
func TestCopyAllowsSymlinkedDestinationOutsideSource(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "docs")
	makeSymlinkGuardTree(t, src)

	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "away")
	symlinkOrSkip(t, outside, link)

	t.Run("Copier", func(t *testing.T) {
		c, err := NewCopier([]string{src}, link)
		if err != nil {
			t.Fatalf("copying into a symlink that points outside the source was refused: %v", err)
		}
		drain(t, c)

		got, err := os.ReadFile(filepath.Join(outside, "docs", "a.txt"))
		if err != nil {
			t.Fatalf("the copy did not land in the link's target: %v", err)
		}
		if string(got) != "payload" {
			t.Errorf("a.txt = %q, want %q", got, "payload")
		}
	})

	t.Run("CopyRecursive", func(t *testing.T) {
		dst := filepath.Join(link, "again")
		if err := CopyRecursive(src, dst); err != nil {
			t.Fatalf("copying into a symlink that points outside the source failed: %v", err)
		}

		got, err := os.ReadFile(filepath.Join(outside, "again", "sub", "b.txt"))
		if err != nil {
			t.Fatalf("the copy did not land in the link's target: %v", err)
		}
		if string(got) != "nested" {
			t.Errorf("sub/b.txt = %q, want %q", got, "nested")
		}
	})
}

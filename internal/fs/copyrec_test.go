package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A symlink to an ancestor used to send the copy into its own output: the
// old implementation stat'ed through the link, so the copy recursed into
// the tree it was writing. Sixty levels of nesting were produced before the
// path length gave out.
func TestCopyRecursiveDoesNotFollowSymlinkToAncestor(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, filepath.Join(src, "loop")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	dst := filepath.Join(base, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- CopyRecursive(src, filepath.Join(dst, "src")) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("CopyRecursive failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("CopyRecursive did not finish in 10s — it is following the symlink")
	}

	// The link must be copied as a link, not descended into.
	link := filepath.Join(dst, "src", "loop")
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("the symlink was not copied: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was copied as a real directory")
	}

	// And there must be no runaway nesting.
	assertNoDeepNesting(t, filepath.Join(dst, "src"))
}

// Copying a directory into itself used to nest forever: the destination was
// created before the source was re-read, so the copy kept finding its own
// output.
func TestCopyRecursiveRefusesToCopyIntoItself(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "docs")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(src, "docs")
	err := CopyRecursive(src, dst)
	if err == nil {
		t.Fatal("copying a directory into itself should be refused")
	}
	if !strings.Contains(err.Error(), "into itself") {
		t.Errorf("error = %q, want it to say the copy is into itself", err)
	}
}

// A symlink to a directory must be copied as a link, not descended into.
func TestCopyRecursiveCopiesSymlinkToDirectoryAsALink(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	target := filepath.Join(base, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "inside.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(src, "link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	dst := filepath.Join(base, "dst")
	if err := CopyRecursive(src, dst); err != nil {
		t.Fatalf("CopyRecursive failed: %v", err)
	}

	link := filepath.Join(dst, "link")
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("the symlink was not copied: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink to a directory was copied as a real directory")
	}
	// The link must not have been replaced by a real directory holding the
	// target's contents. The link still resolves to the target, so the
	// file is reachable through it — that is the point of a symlink.
	if info.IsDir() {
		t.Error("the copy created a real directory where the link belongs")
	}

	// And the destination must hold only the link, not a copy of the
	// target's tree.
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the destination holds %v, want just the link", names)
	}
}

// A regular tree still copies, and keeps its contents.
func TestCopyRecursiveCopiesTree(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "top.txt"), []byte("top"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "nested.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(base, "dst")
	if err := CopyRecursive(src, dst); err != nil {
		t.Fatalf("CopyRecursive failed: %v", err)
	}

	for _, rel := range []string{"top.txt", "sub/nested.txt"} {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("%s was not copied: %v", rel, err)
		}
		if len(got) == 0 {
			t.Errorf("%s is empty", rel)
		}
	}
}

// The executable bit must survive: a copied script that loses it is a
// surprise no file manager should spring.
func TestCopyRecursiveKeepsExecutableBit(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "script.sh")
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(base, "dst", "script.sh")
	if err := CopyRecursive(src, dst); err != nil {
		t.Fatalf("CopyRecursive failed: %v", err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("mode = %v, want the executable bit kept", info.Mode().Perm())
	}
}

// A dangling symlink is still a link, and must be copied as one.
func TestCopyRecursiveCopiesDanglingSymlink(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nowhere/at/all", filepath.Join(src, "broken")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	dst := filepath.Join(base, "dst")
	if err := CopyRecursive(src, dst); err != nil {
		t.Fatalf("CopyRecursive failed: %v", err)
	}

	info, err := os.Lstat(filepath.Join(dst, "broken"))
	if err != nil {
		t.Fatalf("the dangling symlink was not copied: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the dangling symlink was not copied as a link")
	}
}

// The large-copy path must agree with the small one: a symlink to a
// directory used to be queued as a file and fail with "is a directory".
func TestCopierCopiesSymlinkToDirectoryAsALink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "inside.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	dst := filepath.Join(base, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	c, err := NewCopier([]string{link}, dst)
	if err != nil {
		t.Fatalf("NewCopier failed: %v", err)
	}
	for {
		done, err := c.Step()
		if err != nil {
			t.Fatalf("the copy failed: %v", err)
		}
		if done {
			break
		}
	}

	info, err := os.Lstat(filepath.Join(dst, "link"))
	if err != nil {
		t.Fatalf("the symlink was not copied: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink to a directory was copied as a real directory")
	}
}

// The large-copy path must refuse to copy a directory into itself too.
func TestCopierRefusesToCopyIntoItself(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "docs")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := NewCopier([]string{src}, filepath.Join(src, "docs")); err == nil {
		t.Fatal("copying a directory into itself should be refused")
	}
}

// assertNoDeepNesting walks the tree and fails if any directory is nested
// absurdly deep, which is the signature of a copy that followed a link.
func assertNoDeepNesting(t *testing.T, root string) {
	t.Helper()
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > 8 {
			t.Errorf("the copy nested %d levels deep under %s", depth, root)
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				walk(filepath.Join(dir, e.Name()), depth+1)
			}
		}
	}
	walk(root, 0)
}

// Copying a file onto itself used to open the destination with O_TRUNC while
// the source was still open, truncating the inode before anything was read
// back — the file became empty. The refusal must happen before any write.
func TestCopyRefusesSamePath(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "keep.txt")
	if err := os.WriteFile(src, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, op := range []struct {
		name string
		run  func() error
	}{
		{"CopyRecursive", func() error { return CopyRecursive(src, src) }},
		{"Copy", func() error { return Copy(src, filepath.Clean(src)) }},
		{"Copier", func() error {
			c, err := NewCopier([]string{src}, base)
			if err != nil {
				return err
			}
			return c.Abort()
		}},
	} {
		t.Run(op.name, func(t *testing.T) {
			if err := op.run(); err == nil {
				t.Error("copying a file onto itself should be refused")
			}
			got, rerr := os.ReadFile(src)
			if rerr != nil {
				t.Fatalf("source unreadable: %v", rerr)
			}
			if string(got) != "precious" {
				t.Error("the source file must be untouched after the refusal")
			}
		})
	}
}

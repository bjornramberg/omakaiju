package ui

import (
	"strings"
	"testing"
)

// A shortened path must never claim to be the filesystem root, must keep the
// final component, and must mark that leading components were dropped.
func TestTrimPathLeftNeverCollapsesToRoot(t *testing.T) {
	cases := []struct {
		path string
		max  int
	}{
		// The final component alone is wider than the budget.
		{path: "/home/user/projects/averyverylongfilename.go", max: 20},
		// The original implementation returned "/" for this path and budget.
		{path: "/home/user/projects/omakaiju", max: 12},
	}

	for _, tc := range cases {
		got := TrimPathLeft(tc.path, tc.max)
		if got == "/" {
			t.Errorf("TrimPathLeft(%q, %d) = %q, want the path not the root", tc.path, tc.max, got)
		}
		if w := DisplayWidth(got); w > tc.max {
			t.Errorf("TrimPathLeft(%q, %d) is %d cells: %q", tc.path, tc.max, w, got)
		}
		if !strings.Contains(got, ellipsis) {
			t.Errorf("TrimPathLeft(%q, %d) = %q, want an elision marker", tc.path, tc.max, got)
		}
	}
}

func TestTrimPathLeftKeepsLastComponentAndMarksElision(t *testing.T) {
	long := "/home/user/projects/omakaiju/internal/ui"
	got := TrimPathLeft(long, 20)

	if got == "/" {
		t.Fatal("a deep path collapsed to the root")
	}
	if !strings.HasSuffix(got, "internal/ui") {
		t.Errorf("TrimPathLeft(%q, 20) = %q, want the final components preserved", long, got)
	}
	if !strings.Contains(got, "/"+ellipsis+"/") {
		t.Errorf("TrimPathLeft(%q, 20) = %q, want an explicit elision marker", long, got)
	}
	if w := DisplayWidth(got); w > 20 {
		t.Errorf("TrimPathLeft is %d cells, want at most 20: %q", w, got)
	}

	// An over-long final component is shortened, but its extension still
	// identifies the file and the result is not the root.
	file := TrimPathLeft("/home/user/projects/averyverylongfilename.go", 20)
	if !strings.HasSuffix(file, ".go") {
		t.Errorf("TrimPathLeft lost the extension: %q", file)
	}
	if file == "/" {
		t.Error("a long filename collapsed to the root")
	}
}

func TestTrimPathLeftLeavesRootAlone(t *testing.T) {
	if got := TrimPathLeft("/", 10); got != "/" {
		t.Errorf("TrimPathLeft(%q, 10) = %q, want %q", "/", got, "/")
	}
	if got := TrimPathLeft("/tmp/x", 40); got != "/tmp/x" {
		t.Errorf("a path within budget was altered: %q", got)
	}
}

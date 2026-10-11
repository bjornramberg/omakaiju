package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/icons"
)

func fileEntry(name string, size int64, mode os.FileMode) fs.Entry {
	return fs.Entry{Name: name, Path: "/tmp/" + name, Size: size, Mode: mode}
}

func dirEntry(name string) fs.Entry {
	return fs.Entry{
		Name:  name,
		Path:  "/tmp/" + name,
		IsDir: true,
		Mode:  os.ModeDir | 0o755,
	}
}

// Wide rows carry both detail columns.
func TestFileItemShowsSizeAndPerms(t *testing.T) {
	theme := config.DefaultTheme()
	row := NewFileItem(fileEntry("movie.mkv", 45088768, 0o644), false, false, theme, 80)
	out := stripANSI(row.Render())

	if !strings.Contains(out, "43MB") {
		t.Errorf("expected the size, got %q", out)
	}
	if !strings.Contains(out, "-rw-r--r--") {
		t.Errorf("expected permissions, got %q", out)
	}
}

// Directories have no meaningful byte count, so the size cell stays blank while
// the permissions still show.
func TestFileItemDirectoryHasNoSize(t *testing.T) {
	theme := config.DefaultTheme()
	row := NewFileItem(dirEntry("subdir"), false, false, theme, 80)
	out := stripANSI(row.Render())

	if !strings.Contains(out, "drwxr-xr-x") {
		t.Errorf("expected directory permissions, got %q", out)
	}
	// The size cell sits at a fixed offset left of the permissions, so check
	// that span directly: it must be blank regardless of what the name holds.
	i := strings.Index(out, "drwxr-xr-x")
	if i < 0 {
		t.Fatalf("expected directory permissions, got %q", out)
	}
	cell := out[max(0, i-sizeColumnWidth-gapWidth):max(0, i-gapWidth)]
	if strings.TrimSpace(cell) != "" {
		t.Errorf("a directory should not show a size, got %q in %q", cell, out)
	}
}

func TestFileItemShowsExecutableBits(t *testing.T) {
	theme := config.DefaultTheme()
	row := NewFileItem(fileEntry("script.sh", 2048, 0o755), false, false, theme, 80)
	if !strings.Contains(stripANSI(row.Render()), "-rwxr-xr-x") {
		t.Errorf("expected executable permissions, got %q", stripANSI(row.Render()))
	}
}

// Sizes are right aligned so the units line up down the column.
func TestFileItemSizeColumnIsAligned(t *testing.T) {
	theme := config.DefaultTheme()
	small := stripANSI(NewFileItem(fileEntry("a.txt", 999, 0o644), false, false, theme, 80).Render())
	large := stripANSI(NewFileItem(fileEntry("b.txt", 45088768, 0o644), false, false, theme, 80).Render())

	smallAt := strings.Index(small, "999")
	largeAt := strings.Index(large, "43MB")
	if smallAt < 0 || largeAt < 0 {
		t.Fatalf("expected both sizes, got %q and %q", small, large)
	}
	// Right aligned means the ends line up, so the shorter size starts further
	// right while occupying the same column width.
	if smallAt+len("999") != largeAt+len("43MB") {
		t.Errorf("sizes do not share a right edge: %d vs %d", smallAt, largeAt)
	}
}

// The columns are dropped in order as the pane narrows, and never squeeze the
// name below the minimum.
func TestFileItemDropsColumnsAsWidthShrinks(t *testing.T) {
	theme := config.DefaultTheme()
	entry := fileEntry("a-reasonably-long-name.txt", 45088768, 0o644)

	cases := []struct {
		width      int
		wantSize   bool
		wantPerms  bool
		nameMinLen int
	}{
		{width: 80, wantSize: true, wantPerms: true},
		{width: 45, wantSize: true, wantPerms: true},
		{width: 32, wantSize: false, wantPerms: true},
		{width: 22, wantSize: false, wantPerms: false},
	}

	for _, tc := range cases {
		row := NewFileItem(entry, false, false, theme, tc.width)
		out := stripANSI(row.Render())

		hasSize := strings.Contains(out, "43MB")
		hasPerms := strings.Contains(out, "-rw-r--r--")
		if hasSize != tc.wantSize {
			t.Errorf("width %d: size shown = %v, want %v (%q)", tc.width, hasSize, tc.wantSize, out)
		}
		if hasPerms != tc.wantPerms {
			t.Errorf("width %d: perms shown = %v, want %v (%q)", tc.width, hasPerms, tc.wantPerms, out)
		}
	}
}

// The name must keep a usable width in every regime. This asserts the budget
// itself, since middle truncation legitimately rewrites the filename text.
func TestFileItemNameKeepsMinimumWidth(t *testing.T) {
	theme := config.DefaultTheme()
	entry := fileEntry("an-extremely-long-filename-that-will-not-fit.txt", 45088768, 0o644)

	for _, w := range []int{60, 45, 32, 25, 22, 20, 15} {
		row := NewFileItem(entry, false, false, theme, w)

		used := 1 /*marker*/ + 2 + DisplayWidth(iconsIconForTest())
		budget := w - used
		size, perms := row.detailColumns(budget)
		nameBudget := budget - detailWidth(size, perms)

		if detailWidth(size, perms) > 0 && nameBudget < minNameWidth {
			t.Errorf("width %d: details kept but the name is down to %d cells",
				w, nameBudget)
		}
	}
}

// Adding columns must not push a row past the pane width, at any size.
func TestFileItemRowsNeverExceedWidth(t *testing.T) {
	theme := config.DefaultTheme()
	entries := []fs.Entry{
		fileEntry("movie.mkv", 45088768, 0o644),
		fileEntry("notes.md", 14336, 0o644),
		fileEntry("a-really-long-filename-here.txt", 999, 0o644),
		dirEntry("subdirectory"),
		{
			Name:  "a-much-longer-directory-name-here",
			Path:  "/tmp/a-much-longer-directory-name-here",
			IsDir: true,
			Mode:  os.ModeDir | 0o755,
		},
	}

	for _, w := range []int{1, 8, 20, 22, 25, 32, 45, 60, 80, 120, 200} {
		for i, e := range entries {
			for _, active := range []bool{false, true} {
				row := NewFileItem(e, active, i%2 == 0, theme, w)
				if !noLineExceeds(row.Render(), w) {
					t.Errorf("width %d: row overflowed: %q", w, stripANSI(row.Render()))
				}
			}
		}
	}
}

// Marked and unmarked rows must stay aligned with each other.
func TestFileItemMarkedRowsAlign(t *testing.T) {
	theme := config.DefaultTheme()
	entry := fileEntry("movie.mkv", 45088768, 0o644)

	plain := stripANSI(NewFileItem(entry, false, false, theme, 80).Render())
	marked := stripANSI(NewFileItem(entry, false, true, theme, 80).Render())

	sizeAt := strings.Index(plain, "43MB")
	markedAt := strings.Index(marked, "43MB")
	if sizeAt < 0 || markedAt < 0 {
		t.Fatalf("expected the size in both rows: %q / %q", plain, marked)
	}
	if sizeAt != markedAt {
		t.Errorf("marked row shifts the size column: %d vs %d", markedAt, sizeAt)
	}
}

// A hostile filename must still be neutralised before the budget is spent on it.
func TestFileItemSanitizesName(t *testing.T) {
	theme := config.DefaultTheme()
	entry := fileEntry("evil\x1b[2Jname.txt", 10, 0o644)

	out := NewFileItem(entry, false, false, theme, 80).Render()
	for _, r := range stripANSI(out) {
		if r < 0x20 && r != '\t' {
			t.Fatalf("control byte %U from a filename reached the row", r)
		}
	}
}

// ReadDir must carry permission bits through to the entry.
func TestReadDirCarriesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("echo hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		switch e.Name {
		case "script.sh":
			if got := e.Mode.String(); !strings.Contains(got, "rwxr-xr-x") {
				t.Errorf("script.sh mode = %q, want executable bits", got)
			}
		case "sub":
			if !e.IsDir || e.Mode.String() != "drwxr-xr-x" {
				t.Errorf("sub mode = %q, want drwxr-xr-x", e.Mode.String())
			}
		}
	}
}

// iconsIconForTest returns the icon the row would use for a plain file, so the
// width budget matches what Render actually spends.
func iconsIconForTest() string {
	return icons.ForFile("x.txt")
}

// detailStart is where the size column begins, which must be identical for every
// row regardless of how long the name is. The size column sits to the left of
// the permission string by a fixed amount.
func detailStart(t *testing.T, out, perms string) int {
	t.Helper()
	i := strings.Index(out, perms)
	if i < 0 {
		t.Fatalf("no %q in row: %q", perms, out)
	}
	start := i - (sizeColumnWidth + gapWidth)
	if start < 0 {
		t.Fatalf("details sit too close to the left edge: %q", out)
	}
	return start
}

// Short and long names must place the details in the same columns. Trailing
// padding cannot do this: a name shorter than its budget would drag its size
// and permissions left.
func TestFileItemDetailsAlignAcrossNameLengths(t *testing.T) {
	theme := config.DefaultTheme()
	names := []struct {
		name string
		size int64
	}{
		{"a.txt", 999},
		{"movie.mkv", 45088768},
		{"a-much-longer-filename-here.txt", 14336},
		{"z", 1},
	}

	const width = 80
	want := -1
	for _, n := range names {
		entry := fs.Entry{Name: n.name, Path: "/tmp/" + n.name, Size: n.size, Mode: 0o644}
		out := stripANSI(NewFileItem(entry, false, false, theme, width).Render())

		if got := DisplayWidth(out); got != width {
			t.Errorf("%s: row width %d, want %d", n.name, got, width)
		}
		at := detailStart(t, out, "-rw-r--r--")
		if want < 0 {
			want = at
		} else if at != want {
			t.Errorf("%s: details start at %d, want %d (%q)", n.name, at, want, out)
		}
	}
}

// A directory row has a blank size cell but must still line up with the rest.
func TestFileItemDirectoryRowAlignsWithFiles(t *testing.T) {
	theme := config.DefaultTheme()
	const width = 80

	file := fs.Entry{Name: "a.txt", Path: "/tmp/a.txt", Size: 999, Mode: 0o644}
	dir := dirEntry("subdir")

	fileOut := stripANSI(NewFileItem(file, false, false, theme, width).Render())
	dirOut := stripANSI(NewFileItem(dir, false, false, theme, width).Render())

	got := detailStart(t, dirOut, "drwxr-xr-x")
	want := detailStart(t, fileOut, "-rw-r--r--")
	if got != want {
		t.Errorf("directory details start at %d, want %d to match file rows (%q)", got, want, dirOut)
	}
}

// Marked rows are also compared against differently sized names, which the old
// test missed by rendering one entry twice.
func TestFileItemMarkedRowAlignsWithOthers(t *testing.T) {
	theme := config.DefaultTheme()
	const width = 80

	short := fs.Entry{Name: "a.txt", Path: "/tmp/a.txt", Size: 999, Mode: 0o644}
	long := fs.Entry{Name: "a-much-longer-name.txt", Path: "/tmp/x", Size: 2048, Mode: 0o644}

	base := detailStart(t, stripANSI(NewFileItem(short, false, false, theme, width).Render()), "-rw-r--r--")
	marked := detailStart(t, stripANSI(NewFileItem(long, false, true, theme, width).Render()), "-rw-r--r--")

	if marked != base {
		t.Errorf("marked row details at %d, want %d", marked, base)
	}
}

// When the columns are dropped the row keeps today's behaviour and exact width.
func TestFileItemNameOnlyRowIsExactWidth(t *testing.T) {
	theme := config.DefaultTheme()
	entry := fs.Entry{Name: "a.txt", Path: "/tmp/a.txt", Size: 999, Mode: 0o644}

	for _, w := range []int{20, 22, 25} {
		out := NewFileItem(entry, false, false, theme, w).Render()
		if got := DisplayWidth(out); got != w {
			t.Errorf("width %d: row is %d cells", w, got)
		}
	}
}

// A truncated directory name must keep its trailing slash without exceeding
// the row width. The slash used to be appended after truncation, pushing the
// row one cell past the pane and wrapping the permissions.
func TestFileItemLongDirectoryKeepsSlashWithinWidth(t *testing.T) {
	theme := config.DefaultTheme()
	entry := fs.Entry{
		Name:  "a-much-longer-directory-name-here",
		Path:  "/tmp/a-much-longer-directory-name-here",
		IsDir: true,
		Mode:  os.ModeDir | 0o755,
	}

	for _, w := range []int{32, 45, 60, 80} {
		out := stripANSI(NewFileItem(entry, false, false, theme, w).Render())

		if got := DisplayWidth(out); got != w {
			t.Errorf("width %d: row is %d cells", w, got)
		}
		if !strings.Contains(out, "drwxr-xr-x") {
			t.Errorf("width %d: permissions damaged: %q", w, out)
		}
		trimmed := strings.TrimRight(out, " ")
		if !strings.HasSuffix(strings.TrimSuffix(trimmed, "drwxr-xr-x"), "/") &&
			!strings.Contains(trimmed, "/") {
			t.Errorf("width %d: directory lost its trailing slash: %q", w, out)
		}
	}
}

// Short directory names render exactly as before.
func TestFileItemShortDirectoryUnchanged(t *testing.T) {
	theme := config.DefaultTheme()
	entry := dirEntry("subdir")

	out := stripANSI(NewFileItem(entry, false, false, theme, 80).Render())
	if !strings.Contains(out, "subdir/") {
		t.Errorf("expected subdir/ in the row, got %q", out)
	}
	if got := DisplayWidth(out); got != 80 {
		t.Errorf("row width %d, want 80", got)
	}
}

// Symlinks show a lowercase l like ls, not the capital L Go's String method
// emits, keeping the type characters consistent: -, d, l.
func TestFileItemSymlinkShowsLowercaseL(t *testing.T) {
	theme := config.DefaultTheme()
	entry := fs.Entry{
		Name: "link",
		Path: "/tmp/link",
		Mode: os.ModeSymlink | 0o777,
	}

	out := stripANSI(NewFileItem(entry, false, false, theme, 80).Render())
	if !strings.Contains(out, "lrwxrwxrwx") {
		t.Errorf("expected lrwxrwxrwx, got %q", out)
	}
	if strings.Contains(out, "Lrwxrwxrwx") {
		t.Errorf("capital L leaked through: %q", out)
	}
	if got := DisplayWidth(out); got != 80 {
		t.Errorf("row width %d, want 80", got)
	}
}

// A symlink row lines up with the rest of the listing.
func TestFileItemSymlinkRowAligns(t *testing.T) {
	theme := config.DefaultTheme()
	const width = 80

	file := fs.Entry{Name: "a.txt", Path: "/tmp/a.txt", Size: 999, Mode: 0o644}
	link := fs.Entry{Name: "link", Path: "/tmp/link", Mode: os.ModeSymlink | 0o777}

	fileOut := stripANSI(NewFileItem(file, false, false, theme, width).Render())
	linkOut := stripANSI(NewFileItem(link, false, false, theme, width).Render())

	got := detailStart(t, linkOut, "lrwxrwxrwx")
	want := detailStart(t, fileOut, "-rw-r--r--")
	if got != want {
		t.Errorf("symlink details at %d, want %d", got, want)
	}
}

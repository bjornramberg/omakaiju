package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
)

func TestArchiveLinesFormatsEntries(t *testing.T) {
	theme := config.DefaultTheme()
	entries := []fs.ArchiveEntry{
		{Name: "notes.txt", Size: 2048},
		{Name: "src", IsDir: true},
	}

	lines := ArchiveLines(entries, false, theme, 60)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}

	flat := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(flat, "notes.txt") {
		t.Error("file name missing")
	}
	if !strings.Contains(flat, "2KB") {
		t.Error("file size missing")
	}
	if !strings.Contains(flat, "src/") {
		t.Errorf("directory should render with a trailing slash, got %q", flat)
	}
}

func TestArchiveLinesMarksTruncation(t *testing.T) {
	lines := ArchiveLines([]fs.ArchiveEntry{{Name: "a.txt"}}, true, config.DefaultTheme(), 60)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2 with truncation note", len(lines))
	}
	if !strings.Contains(stripANSI(lines[1]), "truncated") {
		t.Error("expected a truncation note")
	}
}

// longMember is a member path far wider than any preview panel, so a row that
// is not bounded will wrap and steal a line from the panel's height.
var longMember = strings.Repeat("deeply/nested/", 12) + "some-very-long-member-name.tar.gz"

// TestArchiveLinesLongNameStaysOnOneLine checks the promise the preview relies
// on: one archive member is one rendered row, no wider than the panel.
func TestArchiveLinesLongNameStaysOnOneLine(t *testing.T) {
	const width = 34
	entries := []fs.ArchiveEntry{{Name: longMember, Size: 2048}}

	lines := ArchiveLines(entries, false, config.DefaultTheme(), width)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want one line per member", len(lines))
	}
	if got := DisplayWidth(lines[0]); got > width {
		t.Errorf("row is %d cells wide, want <= %d: %q", got, width, stripANSI(lines[0]))
	}
	if !strings.Contains(stripANSI(lines[0]), ellipsis) {
		t.Errorf("a name too long for the row should be truncated with an ellipsis: %q", stripANSI(lines[0]))
	}
}

// TestArchiveLinesLongNameKeepsSize checks that reserving the size up front
// leaves the size intact rather than letting the name crowd it out.
func TestArchiveLinesLongNameKeepsSize(t *testing.T) {
	const width = 34
	entries := []fs.ArchiveEntry{{Name: longMember, Size: 2048}}

	lines := ArchiveLines(entries, false, config.DefaultTheme(), width)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if got := DisplayWidth(lines[0]); got > width {
		t.Fatalf("row is %d cells wide, want <= %d", got, width)
	}
	if !strings.Contains(stripANSI(lines[0]), "2KB") {
		t.Errorf("size should survive a truncated name, got %q", stripANSI(lines[0]))
	}
}

// TestArchiveLinesShortNameAlignsSize checks that sizes sit in the same column
// regardless of name length, the way the file rows align their detail columns.
func TestArchiveLinesShortNameAlignsSize(t *testing.T) {
	const width = 40
	entries := []fs.ArchiveEntry{
		{Name: "a.txt", Size: 5},
		{Name: "notes.txt", Size: 2048},
	}

	lines := ArchiveLines(entries, false, config.DefaultTheme(), width)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	for i, want := range []string{"a.txt", "notes.txt"} {
		flat := stripANSI(lines[i])
		if !strings.Contains(flat, want) {
			t.Errorf("line %d missing name %q: %q", i, want, flat)
		}
		if got := DisplayWidth(lines[i]); got != width {
			t.Errorf("line %d is %d cells wide, want the full %d so the size aligns: %q", i, got, width, flat)
		}
	}
	if !strings.HasSuffix(stripANSI(lines[0]), " 5") {
		t.Errorf("size should be right-aligned, got %q", stripANSI(lines[0]))
	}
	if !strings.HasSuffix(stripANSI(lines[1]), " 2KB") {
		t.Errorf("size should be right-aligned, got %q", stripANSI(lines[1]))
	}
}

// TestArchiveLinesNarrowPanelDoesNotOverflow checks the degenerate case: when
// the panel cannot hold the icon and the size, the row is still bounded.
func TestArchiveLinesNarrowPanelDoesNotOverflow(t *testing.T) {
	const width = 6
	entries := []fs.ArchiveEntry{{Name: longMember, Size: 2048}}

	lines := ArchiveLines(entries, false, config.DefaultTheme(), width)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if got := DisplayWidth(lines[0]); got > width {
		t.Errorf("row is %d cells wide, want <= %d: %q", got, width, stripANSI(lines[0]))
	}
}

func TestRenderArchiveTruncatesToHeight(t *testing.T) {
	theme := config.DefaultTheme()
	entries := make([]fs.ArchiveEntry, 50)
	for i := range entries {
		entries[i] = fs.ArchiveEntry{Name: "file" + itoa(i) + ".txt", Size: 10}
	}
	lines := ArchiveLines(entries, false, theme, 26)

	p := NewPreview(30, 16, theme)
	p = p.SetPath("/tmp/big.zip")
	out := p.RenderArchive(lines)

	if strings.Contains(stripANSI(out), "file49.txt") {
		t.Error("entries beyond the visible height should be cut")
	}
	if !strings.Contains(stripANSI(out), "more entries") {
		t.Error("expected a more-entries footer")
	}
}

func TestRenderArchiveEmpty(t *testing.T) {
	theme := config.DefaultTheme()
	p := NewPreview(30, 16, theme)
	p = p.SetPath("/tmp/empty.zip")
	out := stripANSI(p.RenderArchive(nil))
	if !strings.Contains(out, "0 entries") {
		t.Errorf("expected 0 entries footer, got %q", out)
	}
}

func TestArchiveNoticeForUnsupportedFormat(t *testing.T) {
	p := NewPreview(30, 16, config.DefaultTheme())
	out := stripANSI(p.ArchiveNotice("/tmp/a.7z", archiveMessage(fs.ErrUnsupportedArchive)))
	if !strings.Contains(out, "not supported") {
		t.Errorf("unexpected message: %q", out)
	}
	if !strings.Contains(out, "a.7z") {
		t.Error("notice should show the file name")
	}
}

func TestArchiveMessageForCorruptArchive(t *testing.T) {
	msg := archiveMessage(errors.New("invalid zip file"))
	if !strings.Contains(msg, "could not read archive") {
		t.Errorf("unexpected message: %q", msg)
	}
	if !strings.Contains(msg, "invalid zip file") {
		t.Errorf("message should include the cause: %q", msg)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

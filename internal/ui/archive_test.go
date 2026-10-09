package ui

import (
	"errors"
	"strings"
	"testing"

	"omakaiju/internal/config"
	"omakaiju/internal/fs"
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
	if !strings.Contains(flat, "2.0 KB") {
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

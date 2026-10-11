package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
)

// ownANSI matches escape sequences this program generates for styling. They are
// stripped before checking for bytes that would corrupt the terminal.
var ownANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\a]*\a|\x1b[@-Z\\-_]`)

func assertNoControlBytes(t *testing.T, label string, rendered string) {
	t.Helper()

	plain := ownANSI.ReplaceAllString(rendered, "")
	var bad []string
	for _, r := range plain {
		if r < 0x20 && r != '\n' && r != '\t' {
			bad = append(bad, string(r))
			continue
		}
		if r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			bad = append(bad, string(r))
		}
	}
	if len(bad) > 0 {
		t.Errorf("%s emitted terminal control bytes %q", label, bad)
	}
}

// A torrent opens with readable bencoded keys and then embeds raw SHA1 blocks.
// Rendering one used to spray those bytes across the screen, which the terminal
// then acted on.
func TestTorrentPreviewDoesNotCorruptTerminal(t *testing.T) {
	dir := t.TempDir()

	var b strings.Builder
	b.WriteString("d8:announce35:http://tracker.example/announce")
	// Real torrents front-load long, entirely readable metadata, so the first
	// few hundred bytes contain nothing but ASCII. A sniffer that only looks
	// that far calls the file text.
	for b.Len() < 700 {
		b.WriteString("13:creation datei1700000000e4:infod6:lengthi1234567890e")
	}
	b.WriteString("20:piece lengthi262144e12:piecesl")
	for i := 0; i < 60; i++ {
		b.WriteString("\x01\x1b\xff\xfe\x8a\x9c\x41\xd2\x7f\x03\x99\xb1\xc5")
	}
	b.WriteString("ee")

	name := "sample.torrent"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelForDir(t, dir, name)
	out := m.View().Content

	assertNoControlBytes(t, "torrent preview", out)

	if !strings.Contains(plainText(out), "binary file") {
		t.Error("expected the torrent to be previewed as a binary file")
	}
}

// A filename may legally contain a newline or an escape byte on Linux, which is
// the same corruption vector arriving through the file list instead of the
// preview body.
func TestControlBytesInFilenameDoNotCorruptTerminal(t *testing.T) {
	dir := t.TempDir()
	name := "evil\x1b[2Jname\nsecond.txt"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("content\n"), 0o644); err != nil {
		t.Skipf("filesystem rejects the name: %v", err)
	}

	m := modelForDir(t, dir, name)
	out := m.View().Content

	assertNoControlBytes(t, "file list", out)
}

// plainText strips styling and the escapes this program emits, leaving the
// words a user would read.
func plainText(rendered string) string {
	return ownANSI.ReplaceAllString(rendered, "")
}

// modelForDir builds a model showing dir with the cursor on entry, sized so the
// panes actually render.
func modelForDir(t *testing.T, dir, entry string) Model {
	t.Helper()

	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "nonexistent.toml")})
	m.width, m.height = 120, 40
	m.leftPath, m.leftFiles = dir, files
	m.rightPath, m.rightFiles = dir, files
	for i, f := range files {
		if f.Name == entry {
			m.leftCursor = i
		}
	}
	m.updatePreview()
	return m
}

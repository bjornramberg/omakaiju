package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// A torrent starts as readable bencoded keys and then embeds raw SHA1 blocks.
// It has to read as binary, or the preview hands those bytes to the terminal.
func TestDetectFileTypeTreatsTorrentAsBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.720p.torrent")

	var b strings.Builder
	b.WriteString("d8:announce35:http://tracker.example/announce")
	b.WriteString("13:creation datei1700000000e4:infod6:lengthi1234567890e")
	b.WriteString("20:piece lengthi262144e12:piecesl")
	// Piece hashes: 20 bytes each, effectively random.
	for i := 0; i < 40; i++ {
		b.WriteString("\x00\x1b\xff\xfe\x8a\x9c\x41\xd2\x7f\x03\x99\xb1\xc5")
	}
	b.WriteString("ee")

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := DetectFileType(path); got != FileTypeBinary {
		t.Errorf("DetectFileType = %v, want FileTypeBinary", got)
	}
}

func TestDetectFileTypeText(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"plain":   "hello world\nsecond line\n",
		"cjk":     "\u5fbd\u9677\u5b50\u304b\u305f\u3051\n",
		"mixed":   "func main() {\n\tprintln(\"\u00e9\u00e8\")\n}\n",
		"longer":  strings.Repeat("abcdefghij\n", 500),
		"escapes": "colored: \x1b[31mred\x1b[0m\n",
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".dat")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := DetectFileType(path); got != FileTypeText {
				t.Errorf("DetectFileType = %v, want FileTypeText", got)
			}
		})
	}
}

func TestDetectFileTypeBinary(t *testing.T) {
	dir := t.TempDir()
	cases := map[string][]byte{
		"png":       {0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d},
		"randomish": {0xff, 0xfe, 0xfd, 0x00, 0x81, 0x8d, 0x90, 0x95},
		"latin1":    []byte("caf\xe9 na\xefve\nr\xe9sum\xe9 text\n"),
		"elf":       {0x7f, 'E', 'L', 'F', 0x02, 0x01, 0x01, 0x00},
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".bin")
			if err := os.WriteFile(path, content, 0o644); err != nil {
				t.Fatal(err)
			}
			if got := DetectFileType(path); got != FileTypeBinary {
				t.Errorf("DetectFileType = %v, want FileTypeBinary", got)
			}
		})
	}
}

func TestDetectFileTypeEmptyFileIsText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DetectFileType(path); got != FileTypeText {
		t.Errorf("DetectFileType = %v, want FileTypeText", got)
	}
}

// The sniffer must not depend on reading more than the window allows.
func TestDetectFileTypeShortFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tiny")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DetectFileType(path); got != FileTypeText {
		t.Errorf("DetectFileType = %v, want FileTypeText", got)
	}
	if !utf8.ValidString("hi") {
		t.Fatal("unreachable")
	}
}

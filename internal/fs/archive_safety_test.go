package fs

import (
	"strings"
	"testing"
)

// archiveWriter builds an archive of the given format so a safety case can be
// checked against both readers.
type archiveWriter func(t *testing.T, dir, name string, members []member) string

func archiveFormats() map[string]archiveWriter {
	return map[string]archiveWriter{
		"zip": func(t *testing.T, dir, name string, members []member) string {
			return writeZip(t, dir, name, members)
		},
		"tar": func(t *testing.T, dir, name string, members []member) string {
			return writeTar(t, dir, name, members, false)
		},
		"tar.gz": func(t *testing.T, dir, name string, members []member) string {
			return writeTar(t, dir, name, members, true)
		},
	}
}

// findEntry returns the listed entry with the given name, failing the test when
// the listing dropped it.
func findEntry(t *testing.T, entries []ArchiveEntry, name string) ArchiveEntry {
	t.Helper()
	for _, e := range entries {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("entry %q missing from listing %v", name, names(entries))
	return ArchiveEntry{}
}

// A member name carrying terminal instructions must come back as text the
// terminal cannot act on: the escape introducer, the newline and the tab are
// all replaced, while the printable remainder of the name is kept.
func TestListArchiveSanitizesControlBytesInNames(t *testing.T) {
	const raw = "esc\x1b[31mred\nname\t.txt"
	const want = "esc\uFFFD[31mred\uFFFDname .txt"

	for format, write := range archiveFormats() {
		t.Run(format, func(t *testing.T) {
			path := write(t, t.TempDir(), "evil."+format, []member{{name: raw, body: "x"}})

			entries, _, err := ListArchive(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("entries = %v, want one", names(entries))
			}
			if got := entries[0].Name; got != want {
				t.Errorf("Name = %q, want %q", got, want)
			}
			for i := 0; i < len(entries[0].Name); i++ {
				if b := entries[0].Name[i]; b < 0x20 || (b >= 0x7f && b <= 0x9f) {
					t.Fatalf("Name %q still contains the control byte %#x", entries[0].Name, b)
				}
			}
		})
	}
}

// A member named with ".." climbs out of the archive and must be flagged, but
// it is still listed.
func TestListArchiveFlagsParentTraversal(t *testing.T) {
	for format, write := range archiveFormats() {
		t.Run(format, func(t *testing.T) {
			path := write(t, t.TempDir(), "climb."+format, []member{{name: "../../etc/passwd", body: "x"}})

			entries, _, err := ListArchive(path)
			if err != nil {
				t.Fatal(err)
			}
			e := findEntry(t, entries, "../../etc/passwd")
			if !e.UnsafePath {
				t.Errorf("UnsafePath = false for %q, want true", e.Name)
			}
		})
	}
}

// A member named with an absolute path must be flagged, but it is still listed.
func TestListArchiveFlagsAbsolutePath(t *testing.T) {
	for format, write := range archiveFormats() {
		t.Run(format, func(t *testing.T) {
			path := write(t, t.TempDir(), "absolute."+format, []member{{name: "/etc/passwd", body: "x"}})

			entries, _, err := ListArchive(path)
			if err != nil {
				t.Fatal(err)
			}
			e := findEntry(t, entries, "/etc/passwd")
			if !e.UnsafePath {
				t.Errorf("UnsafePath = false for %q, want true", e.Name)
			}
		})
	}
}

// A harmless archive must list exactly as it did before names were sanitised:
// the same entries, in the same order, with the same sizes, none of them
// flagged. A long and a non-ASCII name are ordinary members, not hazards.
func TestListArchiveNormalListingUnchanged(t *testing.T) {
	longName := "long/" + strings.Repeat("x", 100) + ".txt"
	members := []member{
		{name: "b.txt", body: "hello"},
		{name: "A.txt", body: "worldly"},
		{name: "sub/", dir: true},
		{name: "café.txt", body: "abc"},
		{name: longName, body: "L"},
	}
	wantNames := []string{"A.txt", "b.txt", "café.txt", longName, "sub/"}
	wantSizes := []int64{7, 5, 3, 1, 0}
	wantDirs := []bool{false, false, false, false, true}

	for format, write := range archiveFormats() {
		t.Run(format, func(t *testing.T) {
			path := write(t, t.TempDir(), "normal."+format, members)

			entries, truncated, err := ListArchive(path)
			if err != nil {
				t.Fatal(err)
			}
			if truncated {
				t.Error("small archive should not be truncated")
			}
			if got := names(entries); len(got) != len(wantNames) {
				t.Fatalf("names = %v, want %v", got, wantNames)
			} else {
				for i := range wantNames {
					if got[i] != wantNames[i] {
						t.Errorf("names[%d] = %q, want %q", i, got[i], wantNames[i])
					}
				}
			}
			for i, e := range entries {
				if e.Size != wantSizes[i] {
					t.Errorf("%s: size = %d, want %d", e.Name, e.Size, wantSizes[i])
				}
				if e.IsDir != wantDirs[i] {
					t.Errorf("%s: IsDir = %v, want %v", e.Name, e.IsDir, wantDirs[i])
				}
				if e.UnsafePath {
					t.Errorf("%s: UnsafePath = true, want false", e.Name)
				}
			}
		})
	}
}

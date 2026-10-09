package fs

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type member struct {
	name string
	body string
	dir  bool
}

func writeZip(t *testing.T, dir string, name string, members []member) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for _, m := range members {
		w, err := zw.Create(m.name)
		if err != nil {
			t.Fatal(err)
		}
		if !m.dir {
			if _, err := w.Write([]byte(m.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTar(t *testing.T, dir, name string, members []member, compress bool) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var out io.Writer = f
	var gz *gzip.Writer
	if compress {
		gz = gzip.NewWriter(f)
		out = gz
	}

	tw := tar.NewWriter(out)
	for _, m := range members {
		hdr := &tar.Header{Name: m.name, Mode: 0o644, Size: int64(len(m.body))}
		if m.dir {
			hdr.Typeflag = tar.TypeDir
			hdr.Mode = 0o755
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if !m.dir {
			if _, err := tw.Write([]byte(m.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if gz != nil {
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func names(entries []ArchiveEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

func TestListZip(t *testing.T) {
	dir := t.TempDir()
	path := writeZip(t, dir, "test.zip", []member{
		{name: "b.txt", body: "hello"},
		{name: "a.txt", body: "worldly"},
	})

	entries, truncated, err := ListArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Error("zip listing should not report truncation")
	}
	// Sorted case-insensitively, so a.txt precedes b.txt.
	if got := names(entries); len(got) != 2 || got[0] != "a.txt" || got[1] != "b.txt" {
		t.Errorf("names = %v, want [a.txt b.txt]", got)
	}
	for _, e := range entries {
		if e.Name == "a.txt" && e.Size != 7 {
			t.Errorf("a.txt size = %d, want 7", e.Size)
		}
	}
}

func TestListZipDirectoryEntry(t *testing.T) {
	dir := t.TempDir()
	path := writeZip(t, dir, "withdir.zip", []member{
		{name: "sub/", dir: true},
		{name: "file.txt", body: "x"},
	})

	entries, _, err := ListArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name == "sub/" && !e.IsDir {
			t.Error("sub/ should be reported as a directory")
		}
	}
}

func TestListTar(t *testing.T) {
	dir := t.TempDir()
	path := writeTar(t, dir, "test.tar", []member{
		{name: "z.txt", body: "zzz"},
		{name: "a.txt", body: "a"},
	}, false)

	entries, truncated, err := ListArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Error("small tar should not be truncated")
	}
	if got := names(entries); len(got) != 2 || got[0] != "a.txt" || got[1] != "z.txt" {
		t.Errorf("names = %v, want [a.txt z.txt]", got)
	}
}

func TestListTarGz(t *testing.T) {
	dir := t.TempDir()
	path := writeTar(t, dir, "test.tar.gz", []member{
		{name: "inner.txt", body: "data"},
	}, true)

	entries, _, err := ListArchive(path)
	if err != nil {
		t.Fatalf("tar.gz listing failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "inner.txt" {
		t.Errorf("entries = %v, want [inner.txt]", names(entries))
	}
	if entries[0].Size != 4 {
		t.Errorf("size = %d, want 4", entries[0].Size)
	}
}

func TestBareGzIsUnsupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	if _, err := gz.Write([]byte("just some text, not a tar\n")); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if _, _, err := ListArchive(path); !errors.Is(err, ErrUnsupportedArchive) {
		t.Errorf("err = %v, want ErrUnsupportedArchive", err)
	}
}

func TestUnsupportedFormats(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.7z", "b.rar", "c.xz"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ListArchive(path); !errors.Is(err, ErrUnsupportedArchive) {
			t.Errorf("%s: err = %v, want ErrUnsupportedArchive", name, err)
		}
	}
}

func TestCorruptArchiveErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.zip")
	if err := os.WriteFile(path, []byte("this is not a zip file at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ListArchive(path); err == nil {
		t.Error("corrupt zip should return an error")
	} else if errors.Is(err, ErrUnsupportedArchive) {
		t.Error("corrupt zip should not report as unsupported")
	}
}

func TestMissingArchiveErrors(t *testing.T) {
	if _, _, err := ListArchive(filepath.Join(t.TempDir(), "nope.zip")); err == nil {
		t.Error("missing file should return an error")
	}
}

func TestListTarTruncatesAtScanCap(t *testing.T) {
	dir := t.TempDir()
	members := make([]member, 0, maxArchiveScan+10)
	for i := 0; i < maxArchiveScan+10; i++ {
		members = append(members, member{name: "f" + string(rune('a'+i%26)) + itoa(i) + ".txt", body: "x"})
	}
	path := writeTar(t, dir, "big.tar", members, false)

	entries, truncated, err := ListArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Error("large tar should report truncation")
	}
	if len(entries) != maxArchiveScan {
		t.Errorf("entries = %d, want %d", len(entries), maxArchiveScan)
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

func TestFormatSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, c := range cases {
		if got := FormatSize(c.in); got != c.want {
			t.Errorf("FormatSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

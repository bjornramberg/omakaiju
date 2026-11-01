package icons

import "testing"

// The icon map decides what every row in the file panes shows, so the
// extensions it knows have to keep their glyphs.
//
// Two known gaps are deliberately not pinned down here: the extension is
// matched case-sensitively, and a name without an extension (Dockerfile)
// gets the unknown icon. Both are issue #28.
func TestForExtension(t *testing.T) {
	cases := []struct {
		ext  string
		want string
	}{
		{".go", GoIcon},
		{".py", PythonIcon},
		{".js", JSIcon},
		{".ts", JSIcon},
		{".rs", RustIcon},
		{".sh", ShellIcon},
		{".md", MarkdownIcon},
		{".pdf", PDFIcon},
		{".txt", TextIcon},
		{".log", TextIcon},
		{".zip", ArchiveIcon},
		{".gz", ArchiveIcon},
		{".png", ImageIcon},
		{".mp4", VideoIcon},
		{".mp3", AudioIcon},
		{".toml", ConfigIcon},
		{".json", ConfigIcon},
		{".git", GitIcon},
		{"Dockerfile", DockerIcon},
		{".dockerfile", DockerIcon},
	}

	for _, tc := range cases {
		if got := ForExtension(tc.ext); got != tc.want {
			t.Errorf("ForExtension(%q) = %q, want %q", tc.ext, got, tc.want)
		}
	}
}

func TestForExtensionOfSomethingUnknown(t *testing.T) {
	for _, ext := range []string{"", ".qqq", ".not-a-real-extension"} {
		if got := ForExtension(ext); got != UnknownIcon {
			t.Errorf("ForExtension(%q) = %q, want the unknown icon", ext, got)
		}
	}
}

func TestForDir(t *testing.T) {
	if got := ForDir(); got != DirIcon {
		t.Errorf("ForDir() = %q, want %q", got, DirIcon)
	}
	if DirIcon == UnknownIcon {
		t.Error("a directory would show the unknown icon")
	}
}

// ForFile finds the extension in the name and hands it on.
func TestForFile(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"main.go", GoIcon},
		{"README.md", MarkdownIcon},
		{"notes.txt", TextIcon},
		{"archive.tar.gz", ArchiveIcon},
		{"photo.jpeg", ImageIcon},
		{"Makefile", UnknownIcon},
		{"", UnknownIcon},
	}

	for _, tc := range cases {
		if got := ForFile(tc.name); got != tc.want {
			t.Errorf("ForFile(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

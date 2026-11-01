package icons

import "testing"

// The icon map decides what every row in the file panes shows, so the
// extensions it knows have to keep their glyphs.
//
// The extension is matched without regard to case, and a name without an
// extension is looked up whole so Dockerfile reaches the map. Both came from
// issue #28.
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

// The same file type must not depend on how the extension was capitalised:
// README.MD is a Markdown file just as readme.md is.
func TestForExtensionIsCaseInsensitive(t *testing.T) {
	cases := []struct {
		upper string
		lower string
		want  string
	}{
		{".MD", ".md", MarkdownIcon},
		{".Go", ".go", GoIcon},
		{".PDF", ".pdf", PDFIcon},
		{".PNG", ".png", ImageIcon},
	}

	for _, tc := range cases {
		upper := ForExtension(tc.upper)
		lower := ForExtension(tc.lower)
		if upper != lower {
			t.Errorf("ForExtension(%q) = %q, ForExtension(%q) = %q; want the same icon",
				tc.upper, upper, tc.lower, lower)
		}
		if upper != tc.want {
			t.Errorf("ForExtension(%q) = %q, want %q", tc.upper, upper, tc.want)
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

// A name without a dot is looked up whole, so the name-based entry is
// reachable. The extension cases are checked alongside it to show that adding
// the whole-name path did not take them over.
func TestForFileMatchesWholeNamesAndExtensions(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"Dockerfile", DockerIcon},
		{"dockerfile", DockerIcon},
		{".dockerfile", DockerIcon},
		{"notes.txt", TextIcon},
		{"archive.tar.gz", ArchiveIcon},
		{"README.MD", MarkdownIcon},
		{"Makefile", UnknownIcon},
	}

	for _, tc := range cases {
		if got := ForFile(tc.name); got != tc.want {
			t.Errorf("ForFile(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Turning the icons off has to silence every entry, not just the unknown one,
// so no glyph reaches a terminal without a Nerd Font.
func TestTurningIconsOffReturnsNoGlyphs(t *testing.T) {
	SetEnabled(false)
	t.Cleanup(func() { SetEnabled(true) })

	for _, ext := range []string{".go", ".md", ".txt", ".png", ".zip", "Dockerfile", ".qqq"} {
		if got := ForExtension(ext); got != "" {
			t.Errorf("ForExtension(%q) = %q with the icons off, want an empty string", ext, got)
		}
	}
	for _, name := range []string{"main.go", "Dockerfile", "archive.tar.gz", "unknown.qqq"} {
		if got := ForFile(name); got != "" {
			t.Errorf("ForFile(%q) = %q with the icons off, want an empty string", name, got)
		}
	}
	if got := ForDir(); got != "" {
		t.Errorf("ForDir() = %q with the icons off, want an empty string", got)
	}
	if got := Glyph(LinkIcon); got != "" {
		t.Errorf("Glyph(%q) = %q with the icons off, want an empty string", LinkIcon, got)
	}
}

// The icons stay on by default: the setting is opt-in for terminals without
// the font, not a change to how the app looks with it.
func TestIconsAreOnByDefault(t *testing.T) {
	SetEnabled(true)
	t.Cleanup(func() { SetEnabled(true) })

	if !Enabled() {
		t.Fatal("icons should be enabled by default")
	}
	if got := ForFile("README.md"); got != MarkdownIcon {
		t.Errorf("ForFile(%q) = %q, want %q", "README.md", got, MarkdownIcon)
	}
	if got := ForDir(); got != DirIcon {
		t.Errorf("ForDir() = %q, want %q", got, DirIcon)
	}
	if got := Glyph(AppIcon); got != AppIcon {
		t.Errorf("Glyph(%q) = %q, want the glyph", AppIcon, got)
	}
}

package icons

import (
	"strings"
	"sync/atomic"
)

var (
	FileIcon     = "\uf15b"
	DirIcon      = "\uf07b"
	LinkIcon     = "\uf44c"
	ExecIcon     = "\uf120"
	ArchiveIcon  = "\uf1c6"
	ImageIcon    = "\uf1c5"
	VideoIcon    = "\uf03d"
	AudioIcon    = "\uf001"
	ConfigIcon   = "\uf013"
	GitIcon      = "\uf1d3"
	DockerIcon   = "\uf21a"
	GoIcon       = "\uf121"
	PythonIcon   = "\uf15c"
	JSIcon       = "\uf1c0"
	RustIcon     = "\uf1b2"
	ShellIcon    = "\uf120"
	MarkdownIcon = "\uf121"
	PDFIcon      = "\uf1c1"
	TextIcon     = "\uf15c"
	UnknownIcon  = "\uf15b"
	// Separator divides the top bar's cells: name, path, hostname.
	Separator = "\uf101"
	// AppIcon is the Nerd Font droplet glyph that prefixes the app name in the
	// top bar.
	AppIcon = "\uf0c5"
)

// enabled says whether the glyphs are drawn. The Nerd Font they need is
// optional, so a terminal without it can turn them off and get plain rows
// instead of a tofu box in every one. It starts on: the icons are part of how
// the app looks to the people who have the font.
var enabled atomic.Bool

func init() {
	enabled.Store(true)
}

// SetEnabled turns the glyphs on or off. The app calls it once at startup from
// the configuration, before the first frame is drawn.
func SetEnabled(on bool) {
	enabled.Store(on)
}

// Enabled reports whether the glyphs are drawn.
func Enabled() bool {
	return enabled.Load()
}

// Glyph returns g when the icons are on and the empty string when they are
// off. It is for the decorative glyphs that are not file-type icons, such as
// the symlink mark and the top bar's separator.
func Glyph(g string) string {
	if !enabled.Load() {
		return ""
	}
	return g
}

func ForExtension(ext string) string {
	if !enabled.Load() {
		return ""
	}
	// The extension is matched without regard to case, so README.MD is the
	// same file type as readme.md. A name-based entry like Dockerfile arrives
	// here whole (see ForFile), which is why the case is listed without a dot.
	switch strings.ToLower(ext) {
	case ".go":
		return GoIcon
	case ".py":
		return PythonIcon
	case ".js", ".ts", ".jsx", ".tsx":
		return JSIcon
	case ".rs":
		return RustIcon
	case ".sh", ".bash", ".zsh":
		return ShellIcon
	case ".md":
		return MarkdownIcon
	case ".pdf":
		return PDFIcon
	case ".txt", ".log":
		return TextIcon
	case ".zip", ".tar", ".gz", ".bz2", ".xz", ".7z", ".rar":
		return ArchiveIcon
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".svg", ".webp":
		return ImageIcon
	case ".mp4", ".avi", ".mkv", ".mov", ".webm":
		return VideoIcon
	case ".mp3", ".wav", ".flac", ".ogg", ".m4a":
		return AudioIcon
	case ".toml", ".yaml", ".yml", ".json", ".ini", ".cfg", ".conf":
		return ConfigIcon
	case ".git":
		return GitIcon
	case "dockerfile", ".dockerfile":
		return DockerIcon
	default:
		return UnknownIcon
	}
}

func ForDir() string {
	if !enabled.Load() {
		return ""
	}
	return DirIcon
}

func ForFile(name string) string {
	if !enabled.Load() {
		return ""
	}
	ext := ""
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			ext = name[i:]
			break
		}
	}
	// A name without a dot has no extension, but it may still be a known
	// name such as Dockerfile. Handing the whole name to ForExtension makes
	// those entries reachable without disturbing the extension case:
	// notes.txt is still a text file and archive.tar.gz is still an archive.
	if ext == "" {
		ext = name
	}
	return ForExtension(ext)
}

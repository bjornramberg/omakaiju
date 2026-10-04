package icons

var (
	FileIcon     = "\uf15b"
	DirIcon      = "\uf07b"
	LinkIcon     = "\uf0c1"
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
)

func ForExtension(ext string) string {
	switch ext {
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
	case "Dockerfile", ".dockerfile":
		return DockerIcon
	default:
		return UnknownIcon
	}
}

func ForDir() string {
	return DirIcon
}

func ForFile(name string) string {
	ext := ""
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			ext = name[i:]
			break
		}
	}
	return ForExtension(ext)
}

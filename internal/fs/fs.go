package fs

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Entry struct {
	Name    string
	Path    string
	IsDir   bool
	Size    int64
	ModTime int64
}

func ReadDir(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}

	result := make([]Entry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}

		result = append(result, Entry{
			Name:    e.Name(),
			Path:    filepath.Join(path, e.Name()),
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
		})
	}

	SortEntries(result)
	return result, nil
}

// SortEntries orders a listing the way a file manager should: directories
// first, then case-insensitive by name. ReadDir returns raw filesystem order,
// which is unstable across machines, so callers get a deterministic list.
func SortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if la != lb {
			return la < lb
		}
		return a.Name < b.Name
	})
}

func Copy(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}

func CopyRecursive(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return Copy(src, dst)
	}
	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if err := CopyRecursive(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func Move(src, dst string) error {
	return os.Rename(src, dst)
}

func Delete(path string) error {
	return os.RemoveAll(path)
}

func CreateFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return f.Close()
}

func CreateDir(path string) error {
	return os.MkdirAll(path, 0755)
}

type FileType int

const (
	FileTypeText FileType = iota
	FileTypeBinary
	FileTypeImage
	FileTypeArchive
	FileTypeDirectory
)

func DetectFileType(path string) FileType {
	info, err := os.Stat(path)
	if err != nil {
		return FileTypeBinary
	}
	if info.IsDir() {
		return FileTypeDirectory
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md", ".go", ".py", ".js", ".ts", ".rs", ".c", ".cpp", ".h", ".json", ".yaml", ".yml", ".toml", ".xml", ".html", ".css", ".sh", ".bash", ".zsh":
		return FileTypeText
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".svg", ".webp":
		return FileTypeImage
	case ".zip", ".tar", ".gz", ".bz2", ".xz", ".7z", ".rar":
		return FileTypeArchive
	}

	f, err := os.Open(path)
	if err != nil {
		return FileTypeBinary
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return FileTypeText
	}

	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return FileTypeBinary
		}
	}
	return FileTypeText
}

func ReadFileHead(path string, maxLines int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for i := 0; i < maxLines && scanner.Scan(); i++ {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func CountLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}

func WalkDir(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

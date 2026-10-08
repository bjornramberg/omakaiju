package nav

import (
	"strings"

	"omakaiju/internal/fs"
)

func FilterEntries(files []fs.Entry, q string) []fs.Entry {
	if q == "" {
		return files
	}

	needle := strings.ToLower(q)
	result := make([]fs.Entry, 0, len(files))
	for _, f := range files {
		if strings.Contains(strings.ToLower(f.Name), needle) {
			result = append(result, f)
		}
	}
	return result
}

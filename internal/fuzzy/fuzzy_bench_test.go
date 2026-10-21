package fuzzy

import (
	"fmt"
	"testing"
)

// Filtering a large tree is the finder's hot path. It used to run on the UI
// thread, so every keystroke scanned the whole tree before the input could
// be drawn.
func BenchmarkFilter(b *testing.B) {
	for _, n := range []int{1000, 10000, 50000} {
		items := make([]string, 0, n)
		for i := 0; i < n; i++ {
			items = append(items, fmt.Sprintf("/home/user/projects/project%d/internal/pkg/subdir%d/file_%d.go", i%100, i%50, i))
		}
		b.Run(fmt.Sprintf("%d files", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = Filter("cfg", items)
			}
		})
	}
}

// Typing is a sequence of filters, one per keystroke, and each one scans
// the whole tree.
func BenchmarkFilterPerKeystroke(b *testing.B) {
	items := make([]string, 0, 50000)
	for i := 0; i < 50000; i++ {
		items = append(items, fmt.Sprintf("/home/user/projects/project%d/internal/pkg/subdir%d/file_%d.go", i%100, i%50, i))
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, q := range []string{"c", "cf", "cfg", "cfgs"} {
			_ = Filter(q, items)
		}
	}
}

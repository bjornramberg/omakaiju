package ui

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/icons"
)

func topBarTitle(t *testing.T, bar TopBar) string {
	t.Helper()
	for _, line := range strings.Split(stripANSI(bar.Render()), "\n") {
		if strings.Contains(line, "omakaiju") {
			return line
		}
	}
	t.Fatal("top bar did not render the app name")
	return ""
}

func TestTopBarStartsOnPaneFrameColumn(t *testing.T) {
	bar := NewTopBar(80, config.DefaultTheme())
	bar.Path = "/tmp"
	bar.Host = "runner-host"

	want := "omakaiju"
	if icon := icons.Glyph(icons.AppIcon); icon != "" {
		want = icon + " " + want
	}
	if title := topBarTitle(t, bar); !strings.HasPrefix(title, want) {
		t.Errorf("title should start at the pane frame column: %q", title)
	}
}

func TestTopBarPinsHostnameRight(t *testing.T) {
	bar := NewTopBar(80, config.DefaultTheme())
	bar.Path = "/home/user/projects/omakaiju"
	bar.Host = "runner-host"

	title := topBarTitle(t, bar)
	trimmed := strings.TrimRight(title, " ")
	if !strings.HasSuffix(trimmed, "runner-host") {
		t.Errorf("hostname should be flush right: %q", title)
	}
	if got := DisplayWidth(trimmed); got != bar.Width-2 {
		t.Errorf("right-aligned content ends at %d, want %d", got, bar.Width-2)
	}
	if strings.Index(title, "/home/user/projects/omakaiju") > strings.Index(title, "runner-host") {
		t.Errorf("path should come before the hostname: %q", title)
	}
}

func TestTopBarDropsHostnameBeforePath(t *testing.T) {
	bar := NewTopBar(34, config.DefaultTheme())
	bar.Path = "/home/user/projects"
	bar.Host = "runner-host"

	title := topBarTitle(t, bar)
	if !strings.Contains(title, "projects") {
		t.Errorf("path should survive a tight bar: %q", title)
	}
	if strings.Contains(title, "runner-host") {
		t.Errorf("hostname should be dropped before the path: %q", title)
	}
}

func TestTopBarFilterDoesNotDisplaceHostname(t *testing.T) {
	bar := NewTopBar(62, config.DefaultTheme())
	bar.Path = "/home/user/projects/omakaiju"
	bar.Host = "runner-host"
	bar.Filter = "xyz"

	title := topBarTitle(t, bar)
	if !strings.Contains(title, "runner-host") {
		t.Errorf("filter should not push off a hostname that fits: %q", title)
	}
	if strings.Contains(title, "filter:") {
		t.Errorf("filter should yield to the hostname: %q", title)
	}
}

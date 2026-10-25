package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

// A panic in the update path must not end the session. The terminal is
// restored by the runtime, but the user still loses their panes, their
// cursor and any pending work — the worst possible response to a bug.
func TestUpdateSurvivesPanic(t *testing.T) {
	m, _ := fixture(t)
	m.updateHook = func() { panic("boom") }

	updated, cmd := m.Update(key('a'))
	if cmd != nil {
		t.Error("a recovered panic must not dispatch a command")
	}

	m2, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", updated)
	}
	if !m2.Crashed() {
		t.Error("the crash was not recorded")
	}

	reports := m2.CrashReports()
	if len(reports) != 1 {
		t.Fatalf("got %d crash reports, want 1", len(reports))
	}
	if reports[0].Where != "update" {
		t.Errorf("crash site = %q, want %q", reports[0].Where, "update")
	}
	if !strings.Contains(reports[0].Err, "boom") {
		t.Errorf("crash error = %q, want it to mention the panic value", reports[0].Err)
	}
	if reports[0].Stack == "" {
		t.Error("the crash report carries no stack")
	}
}

// The failed update is discarded and the pre-update state is handed back, so
// a keypress that trips a bug does not half-apply.
func TestUpdatePanicKeepsPreUpdateState(t *testing.T) {
	m, _ := fixture(t)
	m.leftCursor = 2
	m.updateHook = func() { panic("boom") }

	updated, _ := m.Update(arrow(tea.KeyDown))
	m2 := updated.(Model)

	if m2.leftCursor != 2 {
		t.Errorf("leftCursor = %d, want 2 (the pre-update state)", m2.leftCursor)
	}
}

// A marked set changed by the failed update must be discarded with the rest
// of it. Model is copied by value, but the map is a reference, so an in-place
// mark would survive the recovery and leave the user with a selection they
// never confirmed.
func TestUpdatePanicDiscardsMarkedMutation(t *testing.T) {
	m, dir := fixture(t)
	keep := filepath.Join(dir, "alpha.txt")
	m.marked = map[string]fs.Entry{
		keep: {Name: "alpha.txt", Path: keep},
	}
	m.leftCursor = indexOf(m.visibleFiles(0), "beta.txt")
	if m.leftCursor < 0 {
		t.Fatal("beta.txt missing from the listing")
	}
	m.updateAfterHook = func() { panic("boom") }

	// Shift+M marks beta.txt, which would add it to the shared map.
	updated, _ := m.Update(shiftKey('m'))
	m2 := updated.(Model)

	if !m2.Crashed() {
		t.Fatal("the crash was not recorded")
	}
	if len(m2.marked) != 1 {
		t.Fatalf("marked = %v, want only the pre-update entry", m2.marked)
	}
	if _, ok := m2.marked[keep]; !ok {
		t.Errorf("marked = %v, want %s still marked", m2.marked, keep)
	}
}

// Sorting must not reorder the caller's backing array: an in-place sort in a
// handler would leak out of a failed update, just like an in-place mark.
func TestApplySortLeavesTheCallersSliceAlone(t *testing.T) {
	m, _ := fixture(t)

	original := []fs.Entry{
		{Name: "b.txt", Path: "/b", Size: 1},
		{Name: "a.txt", Path: "/a", Size: 2},
	}
	m.leftFiles = append([]fs.Entry(nil), original...)
	m.leftSort = fs.SortByName
	m.applySort(0)

	if m.leftFiles[0].Name != "a.txt" {
		t.Errorf("the model's listing was not sorted: %v", m.leftFiles)
	}
	if original[0].Name != "b.txt" {
		t.Errorf("the caller's slice was reordered in place: %v", original)
	}
}

// The listing handed to callers must not be the model's own array, or a
// caller could change the pre-update state through it.
func TestVisibleFilesDoesNotExposeTheModelsArray(t *testing.T) {
	m, _ := fixture(t)

	files := m.visibleFiles(0)
	if len(files) == 0 {
		t.Fatal("the fixture listing is empty")
	}
	files[0] = fs.Entry{Name: "hijacked"}
	if m.leftFiles[0].Name == "hijacked" {
		t.Error("visibleFiles handed out the model's backing array")
	}
}

// The crash is reported to the user in the bottom bar, not swallowed.
func TestCrashErrorIsShownInTheBottomBar(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 120, 40
	m.updateHook = func() { panic("boom") }

	updated, _ := m.Update(key('a'))
	m2 := updated.(Model)

	view := m2.View().Content
	if !strings.Contains(view, "internal error") {
		t.Errorf("the crash should be reported in the view, got:\n%s", view)
	}
}

// A panic in the render path must not end the session either.
func TestViewSurvivesPanic(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 120, 40
	m.viewHook = func() { panic("boom") }

	view := m.View()
	if view.Content == "" {
		t.Fatal("View returned nothing after a panic")
	}
	if !strings.Contains(view.Content, "internal error") {
		t.Errorf("the fallback frame should say what happened, got:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "boom") {
		t.Errorf("the fallback frame should carry the panic value, got:\n%s", view.Content)
	}
}

// The next frame tries again, so a transient fault costs one frame rather
// than the session.
func TestViewRecoversAfterAPanic(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 120, 40

	boom := true
	m.viewHook = func() {
		if boom {
			panic("boom")
		}
	}

	if !strings.Contains(m.View().Content, "internal error") {
		t.Fatal("the first frame should have shown the fallback")
	}

	boom = false
	if strings.Contains(m.View().Content, "internal error") {
		t.Error("the frame after the fault should render normally again")
	}
}

// A panic in a command goroutine becomes a message instead of the end of
// the program.
func TestCmdPanicBecomesAMessage(t *testing.T) {
	cmd := safeCmd("test-cmd", func() tea.Msg { panic("boom") })

	msg := cmd()
	pm, ok := msg.(panicMsg)
	if !ok {
		t.Fatalf("the command returned %T, want panicMsg", msg)
	}
	if pm.report.Where != "test-cmd" {
		t.Errorf("crash site = %q, want %q", pm.report.Where, "test-cmd")
	}
	if !strings.Contains(fmt.Sprint(pm.report.Err), "boom") {
		t.Errorf("crash error = %q, want it to mention the panic value", pm.report.Err)
	}
}

// A command panic is delivered to Update and reported, not fatal.
func TestCmdPanicIsReportedToTheUser(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 120, 40

	cmd := safeCmd("test-cmd", func() tea.Msg { panic("boom") })
	updated, next := m.Update(cmd())
	m2 := updated.(Model)

	if next != nil {
		t.Error("a recovered command panic must not dispatch a command")
	}
	if !m2.Crashed() {
		t.Error("the command crash was not recorded")
	}
	if !strings.Contains(m2.View().Content, "internal error") {
		t.Error("the crash should be reported in the view")
	}
}

// Repeats of the same crash site are free: a transient failure that recurs
// on the next frame is exactly what the recovery is for.
func TestRepeatedCrashAtOneSiteDoesNotEndTheSession(t *testing.T) {
	m, _ := fixture(t)
	m.updateHook = func() { panic("boom") }

	for i := 0; i < 10; i++ {
		updated, cmd := m.Update(key('a'))
		if cmd != nil {
			t.Fatalf("crash %d ended the session", i+1)
		}
		if !updated.(Model).Crashed() {
			t.Fatalf("crash %d was not recorded", i+1)
		}
	}
}

// A spread of unrelated failures means the state is no longer trustworthy,
// so the app quits cleanly instead of limping on.
func TestCrashBudgetEndsTheSession(t *testing.T) {
	m, _ := fixture(t)

	// Each site is distinct, so each one spends budget.
	for i := 0; i < crashBudget; i++ {
		cmd := safeCmd(siteName(i), func() tea.Msg { panic("boom") })
		updated, next := m.Update(cmd())
		if next != nil {
			t.Fatalf("crash %d ended the session early", i+1)
		}
		m = updated.(Model)
	}

	// The next distinct site is one too many.
	cmd := safeCmd(siteName(crashBudget), func() tea.Msg { panic("boom") })
	_, next := m.Update(cmd())
	if next == nil {
		t.Fatal("the crash budget did not end the session")
	}
}

// The same site crashing over and over does not spend the budget.
func TestCrashBudgetCountsDistinctSites(t *testing.T) {
	m, _ := fixture(t)

	for i := 0; i < crashBudget*3; i++ {
		cmd := safeCmd("one-site", func() tea.Msg { panic("boom") })
		updated, next := m.Update(cmd())
		if next != nil {
			t.Fatalf("the session ended after %d repeats of one site", i+1)
		}
		m = updated.(Model)
	}
}

// The exit report carries what a bug report needs.
func TestCrashReportsAreFormatted(t *testing.T) {
	m, _ := fixture(t)
	m.updateHook = func() { panic("boom") }
	updated, _ := m.Update(key('a'))

	reports := updated.(Model).CrashReports()
	out := FormatCrashReports(reports)
	if out == "" {
		t.Fatal("the crash report is empty")
	}
	for _, want := range []string{"omakaiju", "update", "boom", "goroutine"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report is missing %q:\n%s", want, out)
		}
	}
}

// A model built without the constructor still recovers, because the crash
// state is reached through nil-safe accessors.
func TestCrashRecoveryWorksOnAZeroModel(t *testing.T) {
	var m Model
	m.updateHook = func() { panic("boom") }

	updated, cmd := m.Update(key('a'))
	if cmd != nil {
		t.Error("a recovered panic must not dispatch a command")
	}
	if !updated.(Model).Crashed() {
		t.Error("the crash was not recorded on a zero model")
	}
}

func siteName(i int) string {
	return "site-" + string(rune('a'+i))
}

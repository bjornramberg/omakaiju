package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

var plainView = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\a]*\a|\x1b[@-Z\\-_]`)

// tree builds a directory holding one file per name, for use as a distinct
// searchable tree.
func tree(t *testing.T, parent, name string, files ...string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// modelIn builds a model showing dir in both panes.
func modelIn(t *testing.T, dir string) Model {
	t.Helper()
	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "nonexistent.toml")})
	m.width, m.height = 100, 30
	m.leftPath, m.leftFiles = dir, files
	m.rightPath, m.rightFiles = dir, files
	m.updatePreview()
	return m
}

// Pressing Shift+F must not display the previous search's files while the new
// walk is still running.
func TestFuzzyOpenDoesNotShowStaleResults(t *testing.T) {
	base := t.TempDir()
	dirA := tree(t, base, "alpha", "a-only-file.go")
	dirB := tree(t, base, "beta", "b-only-file.go")

	m := modelIn(t, dirA)
	// Simulate a completed search over directory A.
	walkA, _ := fs.WalkDir(dirA)
	m.fuzzyAllFiles = walkA.Files
	m.fuzzyResults = m.fuzzyAllFiles
	m.fuzzyActive = true

	// Move to directory B and open the finder there.
	m2 := modelIn(t, dirB)
	m2.fuzzyAllFiles = walkA.Files // leftover from the previous search
	m2.fuzzyActive = false

	updated, _ := m2.Update(shiftKey('p'))
	m2 = updated.(Model)

	if !m2.fuzzyLoading {
		t.Fatal("the finder should report a walk in progress")
	}
	if len(m2.fuzzyResults) != 0 {
		t.Errorf("stale results retained: %v", m2.fuzzyResults)
	}
	if m2.fuzzyRoot != dirB {
		t.Errorf("fuzzyRoot = %q, want %q", m2.fuzzyRoot, dirB)
	}

	view := plainView.ReplaceAllString(m2.View().Content, "")
	if strings.Contains(view, "a-only-file.go") {
		t.Errorf("files from the previous search are still on screen:\n%s", view)
	}
	if !strings.Contains(view, "scanning") {
		t.Errorf("expected a scanning notice, got:\n%s", view)
	}
}

// A walk that finishes after the user moved on must not replace live results.
func TestFuzzyIgnoresWalkFromAnotherRoot(t *testing.T) {
	base := t.TempDir()
	dirA := tree(t, base, "alpha", "a-only-file.go")
	dirB := tree(t, base, "beta", "b-only-file.go")

	m := modelIn(t, dirB)
	m.fuzzyActive = true
	m.fuzzyRoot = dirB
	m.fuzzyLoading = true
	// An empty query matches everything, so a stale walk that slips through
	// would immediately put directory A's file on screen.
	m.fuzzyInput = ""

	// A late walk rooted in A arrives after the session moved to B.
	updated, _ := m.Update(fuzzyFilesLoadedMsg{files: []string{"a-only-file.go"}, root: dirA})
	m = updated.(Model)

	if len(m.fuzzyResults) != 0 {
		t.Errorf("a stale walk produced results: %v", m.fuzzyResults)
	}
	if !m.fuzzyLoading {
		t.Error("a stale walk should not clear the loading state")
	}
}

// Typing during the scan is remembered and applied when the walk lands.
func TestFuzzyAppliesQueryTypedDuringScan(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "needle.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelIn(t, dir)
	m.fuzzyActive = true
	m.fuzzyRoot = dir
	m.fuzzyLoading = true

	for _, r := range "needle" {
		updated, _ := m.Update(key(r))
		m = updated.(Model)
	}
	if m.fuzzyInput != "needle" {
		t.Fatalf("fuzzyInput = %q, want the typed query", m.fuzzyInput)
	}
	if len(m.fuzzyResults) != 0 {
		t.Errorf("results produced before the walk finished: %v", m.fuzzyResults)
	}

	updated, cmd := m.Update(fuzzyFilesLoadedMsg{files: []string{
		filepath.Join(dir, "needle.go"),
		filepath.Join(dir, "other.go"),
	}, root: dir})
	m = updated.(Model)
	m = settleFuzzy(t, m, cmd)

	if m.fuzzyLoading {
		t.Error("loading should end once the walk lands")
	}
	if len(m.fuzzyResults) != 1 {
		t.Fatalf("results = %v, want just the matching file", m.fuzzyResults)
	}
	if !strings.HasSuffix(m.fuzzyResults[0], "needle.go") {
		t.Errorf("result = %q, want needle.go", m.fuzzyResults[0])
	}
}

// settleFuzzy runs a pending filter and feeds the results back, so a test
// can assert on results that are now computed in the background.
func settleFuzzy(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	updated, _ := m.Update(msg)
	return updated.(Model)
}

// A slow filter that finishes after a faster one must not overwrite the
// faster one's results: the user would see matches for a query they have
// already typed past.
func TestStaleFuzzyResultsAreDropped(t *testing.T) {
	dir := t.TempDir()
	m := modelIn(t, dir)
	m.fuzzyActive = true
	m.fuzzyRoot = dir
	m.fuzzyLoading = false
	m.fuzzyAllFiles = []string{
		filepath.Join(dir, "alpha.go"),
		filepath.Join(dir, "beta.go"),
	}

	// A run for an old query, still in flight.
	stale := fuzzyResultsMsg{
		query:   "a",
		results: []string{filepath.Join(dir, "alpha.go")},
		gen:     m.fuzzyGen,
	}

	// The user types on, which starts a newer run.
	updated, cmd := m.Update(key('b'))
	m = updated.(Model)
	m = settleFuzzy(t, m, cmd)

	if m.fuzzyGen == stale.gen {
		t.Fatal("the new query should have started a new filter run")
	}

	// The old run lands late.
	updated, _ = m.Update(stale)
	m = updated.(Model)

	for _, r := range m.fuzzyResults {
		if strings.HasSuffix(r, "alpha.go") {
			t.Errorf("a stale result was applied: %v", m.fuzzyResults)
		}
	}
}
func TestFuzzyEscapeRetiresSession(t *testing.T) {
	dir := t.TempDir()
	m := modelIn(t, dir)
	m.fuzzyActive = true
	m.fuzzyRoot = dir
	m.fuzzyLoading = true
	m.fuzzyAllFiles = []string{"leftover"}

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = updated.(Model)

	if m.fuzzyRoot != "" || m.fuzzyLoading || len(m.fuzzyAllFiles) != 0 {
		t.Errorf("session not retired: root=%q loading=%v files=%v",
			m.fuzzyRoot, m.fuzzyLoading, m.fuzzyAllFiles)
	}

	updated, _ = m.Update(fuzzyFilesLoadedMsg{files: []string{"late.go"}, root: dir})
	m = updated.(Model)
	if len(m.fuzzyAllFiles) != 0 {
		t.Errorf("a late walk repopulated a closed session: %v", m.fuzzyAllFiles)
	}
}

// Opening the finder must not start the animation loop.
func TestFuzzyOpenDoesNotStartSpinner(t *testing.T) {
	dir := t.TempDir()
	m := modelIn(t, dir)

	updated, _ := m.Update(shiftKey('p'))
	m = updated.(Model)

	if m.fuzzySpinning {
		t.Error("the spinner started on open, before anything was typed")
	}
}

// Typing during the scan is what starts the animation.
func TestFuzzyFirstKeypressStartsSpinner(t *testing.T) {
	dir := t.TempDir()
	m := modelIn(t, dir)
	m.fuzzyActive = true
	m.fuzzyRoot = dir
	m.fuzzyLoading = true

	updated, cmd := m.Update(key('a'))
	m = updated.(Model)

	if !m.fuzzySpinning {
		t.Error("typing should start the animation")
	}
	if cmd == nil {
		t.Error("the animation loop should be scheduled on the first keypress")
	}
}

// Clearing the query again stops the animation rather than leaving it running
// with nothing on screen to animate.
func TestFuzzyClearedQueryStopsSpinner(t *testing.T) {
	dir := t.TempDir()
	m := modelIn(t, dir)
	m.fuzzyActive = true
	m.fuzzyRoot = dir
	m.fuzzyLoading = true

	updated, _ := m.Update(key('a'))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	m = updated.(Model)

	if m.fuzzyInput != "" {
		t.Fatalf("fuzzyInput = %q, want it cleared", m.fuzzyInput)
	}
	updated, cmd := m.Update(fuzzyTickMsg{})
	m = updated.(Model)
	if cmd != nil {
		t.Error("the loop should stop once the query is cleared")
	}
	if m.fuzzySpinning {
		t.Error("the spinner should stop when the query is cleared")
	}
}

// The spinner must advance while the walk is running, and keep going.
func TestFuzzyTickAdvancesSpinnerWhileLoading(t *testing.T) {
	dir := t.TempDir()
	m := modelIn(t, dir)
	m.fuzzyActive = true
	m.fuzzyRoot = dir
	m.fuzzyLoading = true
	// Animation only runs for a query the user is waiting on.
	m.fuzzyInput = "a"
	m.fuzzySpinning = true

	updated, cmd := m.Update(fuzzyTickMsg{})
	m = updated.(Model)

	if m.fuzzySpinner != 1 {
		t.Errorf("fuzzySpinner = %d, want 1", m.fuzzySpinner)
	}
	if cmd == nil {
		t.Error("the loop should be reissued while the walk runs")
	}
}

// A tick that outlives the walk must not restart the loop, otherwise the timer
// runs forever in the background.
func TestFuzzyTickStopsAfterLoad(t *testing.T) {
	dir := t.TempDir()
	m := modelIn(t, dir)
	m.fuzzyActive = true
	m.fuzzyRoot = dir
	m.fuzzyLoading = true
	m.fuzzyInput = "a"
	m.fuzzySpinning = true

	updated, _ := m.Update(fuzzyFilesLoadedMsg{files: []string{"a.go"}, root: dir})
	m = updated.(Model)

	before := m.fuzzySpinner
	updated, cmd := m.Update(fuzzyTickMsg{})
	m = updated.(Model)

	if cmd != nil {
		t.Error("no command should be reissued once the walk has landed")
	}
	if m.fuzzySpinner != before {
		t.Errorf("fuzzySpinner moved to %d after loading finished", m.fuzzySpinner)
	}
}

func TestFuzzyTickStopsAfterEscape(t *testing.T) {
	dir := t.TempDir()
	m := modelIn(t, dir)
	m.fuzzyActive = true
	m.fuzzyRoot = dir
	m.fuzzyLoading = true
	m.fuzzyInput = "a"
	m.fuzzySpinning = true

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = updated.(Model)

	before := m.fuzzySpinner
	updated, cmd := m.Update(fuzzyTickMsg{})
	m = updated.(Model)

	if cmd != nil {
		t.Error("no command should be reissued after the finder is closed")
	}
	if m.fuzzySpinner != before {
		t.Errorf("fuzzySpinner moved to %d after escape", m.fuzzySpinner)
	}
}
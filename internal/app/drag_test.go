package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

// dragLeftX and dragRightX are columns inside each pane of the drag fixture. A
// 100-column terminal splits into two 50-column panes side by side, so x=3 is
// inside the left one and x=50 inside the right one.
const (
	dragLeftX  = 3
	dragRightX = 50
)

// dragRowY is the screen row a listing row is drawn on in the fixture. It uses
// the same geometry the mouse tests assert: the top bar, then the pane's
// border, padding, header, blank line and ".." row above the first file.
func dragRowY(row int) int {
	return mouseTopBarRows + mouseFirstFileRow + row
}

// dragFixture builds a model whose two panes show different directories, so a
// drop has somewhere to move an entry to. The mouse is enabled and the terminal
// is large enough for the side-by-side layout with a preview column.
func dragFixture(t *testing.T) (Model, string, string) {
	t.Helper()
	left, right := t.TempDir(), t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(left, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(right, "other.txt"), []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}

	leftFiles, err := fs.ReadDir(left)
	if err != nil {
		t.Fatal(err)
	}
	rightFiles, err := fs.ReadDir(right)
	if err != nil {
		t.Fatal(err)
	}

	m := testModel(t, config.Config{ThemePath: filepath.Join(left, "none.toml")})
	m.cfg.Mouse = true
	m.leftPath, m.leftFiles = left, leftFiles
	m.rightPath, m.rightFiles = right, rightFiles
	m.width, m.height = 100, 30
	m.updatePreview()
	return m, left, right
}

// dragMotionAt is the pointer moving while the left button is down, which is
// what a terminal in cell-motion mode reports during a drag.
func dragMotionAt(x, y int, mod tea.KeyMod) tea.Msg {
	return tea.MouseMotionMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft, Mod: mod})
}

// dragReleaseAt is the left button coming up at a position, with the modifiers
// the terminal reported for that event.
func dragReleaseAt(x, y int, mod tea.KeyMod) tea.Msg {
	return tea.MouseReleaseMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft, Mod: mod})
}

// runDrop runs the command a drop returned to completion the way the event loop
// does, so the model it hands back carries the operation's result and the entry
// is where the drop put it on disk.
func runDrop(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("the drop started no operation")
	}
	return runTransfer(t, m, cmd)
}

// A press on a row arms a drag for that entry, on top of the click's own work.
func TestDragPressStartsDrag(t *testing.T) {
	m, left, _ := dragFixture(t)
	m.activePane = 1
	row := mouseRow(t, m, "b.txt")

	got := updateModel(t, m, clickAt(dragLeftX, dragRowY(row)))
	if got.drag == nil {
		t.Fatal("a press on a row armed no drag")
	}
	if got.drag.pane != 0 || got.drag.row != row {
		t.Errorf("drag started at pane %d row %d, want pane 0 row %d", got.drag.pane, got.drag.row, row)
	}
	if want := filepath.Join(left, "b.txt"); got.drag.entry.Path != want {
		t.Errorf("drag carries %q, want the row under the pointer %q", got.drag.entry.Path, want)
	}
	if got.drag.target != -1 {
		t.Errorf("a new drag already has target %d, want none (-1)", got.drag.target)
	}

	// The press is still a click: the drag is what happens on top of one.
	if got.activePane != 0 {
		t.Errorf("activePane = %d, want the pressed pane 0", got.activePane)
	}
	if got.leftCursor != row {
		t.Errorf("leftCursor = %d, want the pressed row %d", got.leftCursor, row)
	}
}

// The ".." row is not an entry, so a press on it is a click with nothing to
// drag.
func TestDragPressOnParentRowStartsNoDrag(t *testing.T) {
	m, _, _ := dragFixture(t)
	m.activePane = 1

	got := updateModel(t, m, clickAt(dragLeftX, mouseTopBarRows+mouseParentRow))
	if got.drag != nil {
		t.Errorf("a press on the parent row armed a drag of %q", got.drag.entry.Path)
	}
	// The click itself still did its work.
	if got.leftCursor != parentCursor {
		t.Errorf("leftCursor = %d, want the parent row %d", got.leftCursor, parentCursor)
	}
	if got.activePane != 0 {
		t.Errorf("activePane = %d, want the pressed pane 0", got.activePane)
	}
}

// The pane under the pointer is the drop target, and nothing else is: the pane
// the drag came from, the bars and the preview column all leave no target.
func TestDragMotionTracksTheOtherPane(t *testing.T) {
	m, _, _ := dragFixture(t)
	pressed := updateModel(t, m, clickAt(dragLeftX, dragRowY(0)))
	if pressed.drag == nil {
		t.Fatal("setup: the press armed no drag")
	}

	over, _ := updateModelCmd(t, pressed, dragMotionAt(dragRightX, dragRowY(1), 0))
	if over.drag == nil {
		t.Fatal("the drag was dropped by a motion")
	}
	if over.drag.target != 1 {
		t.Errorf("a motion over the other pane made target %d, want 1", over.drag.target)
	}

	for _, tc := range []struct {
		name string
		x, y int
	}{
		{"back over the origin pane", dragLeftX, dragRowY(0)},
		{"the top bar", 40, 1},
		{"the bottom bar", 40, 28},
		{"the preview column", 80, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := updateModel(t, over, dragMotionAt(tc.x, tc.y, 0))
			if got.drag == nil {
				t.Fatal("the drag was dropped by a motion")
			}
			if got.drag.target != -1 {
				t.Errorf("a motion over %s made target %d, want none (-1)", tc.name, got.drag.target)
			}
		})
	}
}

// A drag passes over the other pane's rows without touching its cursor: the
// cursor belongs to the pane the drag started in and has to stay on the row it
// started on, so the release can still be understood.
func TestDragMotionDoesNotMoveTheCursor(t *testing.T) {
	m, _, _ := dragFixture(t)
	pressed := updateModel(t, m, clickAt(dragLeftX, dragRowY(1)))

	got := updateModel(t, pressed, dragMotionAt(dragRightX, dragRowY(1), 0))
	if got.drag == nil || got.drag.target != 1 {
		t.Fatalf("setup: the motion did not target the other pane: %+v", got.drag)
	}
	if got.leftCursor != 1 {
		t.Errorf("leftCursor = %d, want the row the drag started on (1)", got.leftCursor)
	}
	if got.rightCursor != 0 {
		t.Errorf("rightCursor = %d, want the untouched 0: dragging must not select in the other pane", got.rightCursor)
	}
}

// A release over the other pane moves the entry into that pane's directory.
func TestDragReleaseOverOtherPaneMoves(t *testing.T) {
	m, left, right := dragFixture(t)
	name := "a.txt"
	row := mouseRow(t, m, name)

	pressed := updateModel(t, m, clickAt(dragLeftX, dragRowY(row)))
	hovered := updateModel(t, pressed, dragMotionAt(dragRightX, dragRowY(0), 0))
	if hovered.drag == nil || hovered.drag.target != 1 {
		t.Fatalf("setup: the motion did not target the other pane: %+v", hovered.drag)
	}

	got, cmd := updateModelCmd(t, hovered, dragReleaseAt(dragRightX, dragRowY(0), 0))
	if cmd == nil {
		t.Fatal("a release over the other pane started no operation")
	}
	if got.drag != nil {
		t.Error("the drag outlived its release")
	}
	if want := "moving " + name + "..."; got.opResult != want {
		t.Errorf("opResult = %q, want %q", got.opResult, want)
	}
	// The reload that follows the operation has to land on the entry in the
	// pane it arrived in.
	if landing := got.restoreOf(1); landing != name {
		t.Errorf("the target pane will land on %q, want %q", landing, name)
	}

	// Running the command is what performs the move, so the result on the
	// model is the proof that the drop's command moved one entry.
	after := runDrop(t, got, cmd)
	if want := "moved 1 file(s)"; after.opResult != want {
		t.Errorf("the move reported %q, want %q", after.opResult, want)
	}
	if after.opErr != nil {
		t.Errorf("the move failed: %v", after.opErr)
	}

	if _, err := os.Stat(filepath.Join(right, name)); err != nil {
		t.Errorf("%s did not land in the target pane's directory: %v", name, err)
	}
	if _, err := os.Stat(filepath.Join(left, name)); !os.IsNotExist(err) {
		t.Errorf("%s is still in the origin pane's directory (stat err = %v)", name, err)
	}
}

// Holding Ctrl while dropping copies instead of moving.
func TestDragReleaseWithCtrlCopies(t *testing.T) {
	m, left, right := dragFixture(t)
	name := "a.txt"
	row := mouseRow(t, m, name)

	pressed := updateModel(t, m, clickAt(dragLeftX, dragRowY(row)))
	hovered := updateModel(t, pressed, dragMotionAt(dragRightX, dragRowY(0), tea.ModCtrl))
	if hovered.drag == nil || !hovered.drag.copy {
		t.Fatalf("setup: the motion did not record the copy modifier: %+v", hovered.drag)
	}

	got, cmd := updateModelCmd(t, hovered, dragReleaseAt(dragRightX, dragRowY(0), tea.ModCtrl))
	if cmd == nil {
		t.Fatal("a release with Ctrl held started no operation")
	}
	if want := "copying " + name + "..."; got.opResult != want {
		t.Errorf("opResult = %q, want %q", got.opResult, want)
	}

	// Running the command is what performs the copy. The result on the model
	// proves it copied one entry: the report is the count the copier
	// finished with, so a drop that never told the model how many files it
	// was moving would show "copied 0 file(s)" here.
	after := runDrop(t, got, cmd)
	if want := "copied 1 file(s)"; after.opResult != want {
		t.Errorf("the copy reported %q, want %q", after.opResult, want)
	}
	if after.opErr != nil {
		t.Errorf("the copy failed: %v", after.opErr)
	}

	src := filepath.Join(left, name)
	dst := filepath.Join(right, name)
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("the copy did not land in the target pane's directory: %v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("a copy removed the original: %v", err)
	}
	copied, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(copied) != name {
		t.Errorf("the copy holds %q, want %q", copied, name)
	}
}

// A release that does not end over the other pane is a cancelled drag: nothing
// is started, and the click the press made stands.
func TestDragReleaseOutsideTheTargetPaneDoesNothing(t *testing.T) {
	cases := []struct {
		name string
		x, y int
	}{
		{"the origin pane", dragLeftX, dragRowY(1)},
		{"the top bar", 40, 1},
		{"the bottom bar", 40, 28},
		{"the preview column", 80, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, left, _ := dragFixture(t)
			name := "a.txt"
			row := mouseRow(t, m, name)
			pressed := updateModel(t, m, clickAt(dragLeftX, dragRowY(row)))

			// The pointer reaches the release position while the button is
			// still down, which is the motion a terminal reports before it
			// reports the release.
			hovered := updateModel(t, pressed, dragMotionAt(tc.x, tc.y, 0))
			got, cmd := updateModelCmd(t, hovered, dragReleaseAt(tc.x, tc.y, 0))

			if cmd != nil {
				t.Error("a release outside the other pane started an operation")
			}
			if got.drag != nil {
				t.Error("the drag outlived its release")
			}
			// A drop that happened anyway would leave its own notice here:
			// dropping into the origin pane reports "already in this
			// directory", and every other destination would say what it was
			// doing.
			if got.opResult != "" {
				t.Errorf("a release over %s reported %q, want no operation", tc.name, got.opResult)
			}
			if _, err := os.Stat(filepath.Join(left, name)); err != nil {
				t.Errorf("%s left the origin pane's directory: %v", name, err)
			}
		})
	}
}

// A press and release with no motion is an ordinary click: the row is selected,
// no drag is left behind and nothing is started.
func TestDragPressAndReleaseWithoutMotionIsAClick(t *testing.T) {
	m, left, _ := dragFixture(t)
	m.activePane = 1
	name := "b.txt"
	row := mouseRow(t, m, name)

	pressed := updateModel(t, m, clickAt(dragLeftX, dragRowY(row)))
	if pressed.drag == nil {
		t.Fatal("setup: the press armed no drag")
	}

	got, cmd := updateModelCmd(t, pressed, dragReleaseAt(dragLeftX, dragRowY(row), 0))
	if cmd != nil {
		t.Error("a click's release started an operation")
	}
	if got.drag != nil {
		t.Error("a click's release left a drag behind")
	}
	if got.opResult != "" {
		t.Errorf("a click's release reported %q, want nothing", got.opResult)
	}
	if got.activePane != 0 || got.leftCursor != row {
		t.Errorf("the click's own work was undone: focus %d cursor %d, want 0/%d", got.activePane, got.leftCursor, row)
	}
	if _, err := os.Stat(filepath.Join(left, name)); err != nil {
		t.Errorf("%s left its directory on a plain click: %v", name, err)
	}
}

// The ".." row has no entry to drag, so the whole gesture over it does nothing.
func TestDragParentRowCannotBeDropped(t *testing.T) {
	m, _, right := dragFixture(t)
	parentY := mouseTopBarRows + mouseParentRow

	pressed := updateModel(t, m, clickAt(dragLeftX, parentY))
	if pressed.drag != nil {
		t.Fatalf("a press on the parent row armed a drag of %q", pressed.drag.entry.Path)
	}

	hovered, _ := updateModelCmd(t, pressed, dragMotionAt(dragRightX, dragRowY(1), 0))
	got, cmd := updateModelCmd(t, hovered, dragReleaseAt(dragRightX, dragRowY(1), 0))
	if cmd != nil {
		t.Error("a release after pressing the parent row started an operation")
	}
	if got.drag != nil {
		t.Error("a release after pressing the parent row left a drag behind")
	}
	if got.opResult != "" {
		t.Errorf("a release after pressing the parent row reported %q", got.opResult)
	}
	// Nothing arrived in the other pane: the listing it shows is the one it
	// started with.
	if files := got.visibleFiles(1); len(files) != 1 || files[0].Name != "other.txt" {
		t.Errorf("the target pane shows %v, want only its own entry", files)
	}
	if _, err := os.Stat(filepath.Join(right, "other.txt")); err != nil {
		t.Errorf("the target directory changed: %v", err)
	}
}

// Dropping into the pane that shows the same directory is a no-op: the entry is
// already there.
func TestDragDropIntoTheSameDirectoryDoesNothing(t *testing.T) {
	m, dir := mouseFixture(t, 4)
	name := "f01.txt"
	row := mouseRow(t, m, name)

	pressed := updateModel(t, m, clickAt(dragLeftX, dragRowY(row)))
	hovered := updateModel(t, pressed, dragMotionAt(dragRightX, dragRowY(0), 0))
	if hovered.drag == nil || hovered.drag.target != 1 {
		t.Fatalf("setup: the motion did not target the other pane: %+v", hovered.drag)
	}

	got, cmd := updateModelCmd(t, hovered, dragReleaseAt(dragRightX, dragRowY(0), 0))
	if cmd != nil {
		t.Error("a drop into the directory the entry already sits in started an operation")
	}
	if want := "already in this directory"; got.opResult != want {
		t.Errorf("opResult = %q, want %q", got.opResult, want)
	}
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		t.Errorf("%s is gone: %v", name, err)
	}
}

// A second transfer cannot start while one is still running: the copier is a
// single field on the model, so a drop that replaced it would abandon the copy
// that was in flight and leak what it had open.
func TestDragDropRefusedWhileATransferRuns(t *testing.T) {
	m, left, right := dragFixture(t)

	copier, err := fs.NewCopier([]string{filepath.Join(left, "b.txt")}, right)
	if err != nil {
		t.Fatal(err)
	}
	m.copier = copier

	name := "a.txt"
	row := mouseRow(t, m, name)
	pressed := updateModel(t, m, clickAt(dragLeftX, dragRowY(row)))
	hovered := updateModel(t, pressed, dragMotionAt(dragRightX, dragRowY(0), 0))

	got, cmd := updateModelCmd(t, hovered, dragReleaseAt(dragRightX, dragRowY(0), 0))
	if cmd != nil {
		t.Error("a drop started a second transfer over a running one")
	}
	if want := "copy in progress"; got.opResult != want {
		t.Errorf("opResult = %q, want %q", got.opResult, want)
	}
	if _, err := os.Stat(filepath.Join(left, name)); err != nil {
		t.Errorf("%s moved even though the drop was refused: %v", name, err)
	}
}

// The pane under the pointer is the one the indicator lights up, and only it:
// the pane the drag came from must keep saying that nothing would happen there.
func TestDropIndicatorFollowsTheTargetPane(t *testing.T) {
	m, _, _ := dragFixture(t)

	panes := m.paneViews(m.layout())
	if panes[0].Drop != ui.DropNone || panes[1].Drop != ui.DropNone {
		t.Errorf("with no drag the panes report %v/%v, want DropNone/DropNone", panes[0].Drop, panes[1].Drop)
	}

	pressed := updateModel(t, m, clickAt(dragLeftX, dragRowY(0)))
	if panes := pressed.paneViews(pressed.layout()); panes[0].Drop != ui.DropNone || panes[1].Drop != ui.DropNone {
		t.Errorf("without a motion the panes report %v/%v, want DropNone/DropNone", panes[0].Drop, panes[1].Drop)
	}

	hovered := updateModel(t, pressed, dragMotionAt(dragRightX, dragRowY(0), 0))
	panes = hovered.paneViews(hovered.layout())
	if panes[1].Drop != ui.DropMove {
		t.Errorf("the pane under the pointer reports %v, want DropMove", panes[1].Drop)
	}
	if panes[0].Drop != ui.DropNone {
		t.Errorf("the pane the drag came from reports %v, want DropNone", panes[0].Drop)
	}

	// Pressing the modifier turns the cue into a copy without the pointer
	// having to move again.
	copying := updateModel(t, hovered, dragMotionAt(dragRightX, dragRowY(0), tea.ModCtrl))
	panes = copying.paneViews(copying.layout())
	if panes[1].Drop != ui.DropCopy {
		t.Errorf("with Ctrl held the pane under the pointer reports %v, want DropCopy", panes[1].Drop)
	}
	if panes[0].Drop != ui.DropNone {
		t.Errorf("with Ctrl held the pane the drag came from reports %v, want DropNone", panes[0].Drop)
	}

	// Back over the origin pane nobody would receive the drop.
	back := updateModel(t, copying, dragMotionAt(dragLeftX, dragRowY(0), 0))
	if panes := back.paneViews(back.layout()); panes[0].Drop != ui.DropNone || panes[1].Drop != ui.DropNone {
		t.Errorf("over the origin pane the panes report %v/%v, want DropNone/DropNone", panes[0].Drop, panes[1].Drop)
	}
}

// Alt is accepted as the copy modifier too, because a terminal can take Ctrl
// for its own selection. Shift and the bare button do not copy.
func TestCopyModifier(t *testing.T) {
	for _, tc := range []struct {
		name string
		mod  tea.KeyMod
		want bool
	}{
		{"no modifier", 0, false},
		{"ctrl", tea.ModCtrl, true},
		{"alt", tea.ModAlt, true},
		{"shift", tea.ModShift, false},
		{"ctrl+shift", tea.ModCtrl | tea.ModShift, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := copyModifier(tc.mod); got != tc.want {
				t.Errorf("copyModifier(%v) = %v, want %v", tc.mod, got, tc.want)
			}
		})
	}
}

// dragFolderFixture builds a model whose target pane lists a directory of its
// own, so a drop can land inside it rather than on the pane's background. The
// left pane holds one file to drag; the right pane lists "sub" first and a file
// after it, which gives the tests a folder row and a plain file row side by
// side.
func dragFolderFixture(t *testing.T) (Model, string, string) {
	t.Helper()
	left, right := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(left, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(right, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(right, "z.txt"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}

	leftFiles, err := fs.ReadDir(left)
	if err != nil {
		t.Fatal(err)
	}
	rightFiles, err := fs.ReadDir(right)
	if err != nil {
		t.Fatal(err)
	}

	m := testModel(t, config.Config{ThemePath: filepath.Join(left, "none.toml")})
	m.cfg.Mouse = true
	m.leftPath, m.leftFiles = left, leftFiles
	m.rightPath, m.rightFiles = right, rightFiles
	m.width, m.height = 100, 30
	m.updatePreview()
	return m, left, right
}

// A motion over a folder row of the other pane makes that folder the drop's
// destination; moving off it onto a file, the ".." row or the pane's background
// clears it again.
func TestDragMotionTracksTheFolderRow(t *testing.T) {
	m, _, _ := dragFolderFixture(t)
	pressed := updateModel(t, m, clickAt(dragLeftX, dragRowY(0)))
	if pressed.drag == nil {
		t.Fatal("setup: the press armed no drag")
	}

	overFolder, _ := updateModelCmd(t, pressed, dragMotionAt(dragRightX, dragRowY(0), 0))
	if overFolder.drag == nil {
		t.Fatal("the drag was dropped by a motion")
	}
	if overFolder.drag.target != 1 || overFolder.drag.targetRow != 0 {
		t.Errorf("a motion over the folder row made target/targetRow %d/%d, want 1/0",
			overFolder.drag.target, overFolder.drag.targetRow)
	}
	if got := overFolder.dropRowFor(1); got != 0 {
		t.Errorf("dropRowFor(1) = %d, want the folder row 0", got)
	}
	if got := overFolder.dropRowFor(0); got != -1 {
		t.Errorf("dropRowFor(0) = %d, want -1 for the pane the drag came from", got)
	}

	for _, tc := range []struct {
		name string
		x, y int
	}{
		{"a file row", dragRightX, dragRowY(1)},
		{"the pane background", dragRightX, dragRowY(8)},
		{"the parent row", dragRightX, mouseTopBarRows + mouseParentRow},
		{"the origin pane", dragLeftX, dragRowY(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := updateModel(t, overFolder, dragMotionAt(tc.x, tc.y, 0))
			if got.drag == nil {
				t.Fatal("the drag was dropped by a motion")
			}
			if got.drag.targetRow != -1 {
				t.Errorf("a motion over %s left targetRow %d, want -1", tc.name, got.drag.targetRow)
			}
		})
	}
}

// A release over a folder row moves the dragged entry into that folder.
func TestDragReleaseOnFolderMovesIntoIt(t *testing.T) {
	m, left, right := dragFolderFixture(t)
	name := "a.txt"

	pressed := updateModel(t, m, clickAt(dragLeftX, dragRowY(0)))
	hovered := updateModel(t, pressed, dragMotionAt(dragRightX, dragRowY(0), 0))
	if hovered.drag == nil || hovered.drag.targetRow != 0 {
		t.Fatalf("setup: the motion did not target the folder row: %+v", hovered.drag)
	}

	got, cmd := updateModelCmd(t, hovered, dragReleaseAt(dragRightX, dragRowY(0), 0))
	if cmd == nil {
		t.Fatal("a release over a folder started no operation")
	}
	after := runDrop(t, got, cmd)
	if after.opErr != nil {
		t.Fatalf("the move failed: %v", after.opErr)
	}
	if want := "moved 1 file(s)"; after.opResult != want {
		t.Errorf("the move reported %q, want %q", after.opResult, want)
	}
	if _, err := os.Stat(filepath.Join(right, "sub", name)); err != nil {
		t.Errorf("%s did not land inside the folder: %v", name, err)
	}
	if _, err := os.Stat(filepath.Join(left, name)); !os.IsNotExist(err) {
		t.Errorf("%s is still in the origin pane's directory (stat err = %v)", name, err)
	}
}

// A release with Ctrl over a folder row copies the entry into that folder.
func TestDragReleaseOnFolderWithCtrlCopiesIntoIt(t *testing.T) {
	m, left, right := dragFolderFixture(t)
	name := "a.txt"

	pressed := updateModel(t, m, clickAt(dragLeftX, dragRowY(0)))
	hovered := updateModel(t, pressed, dragMotionAt(dragRightX, dragRowY(0), tea.ModCtrl))
	if hovered.drag == nil || !hovered.drag.copy || hovered.drag.targetRow != 0 {
		t.Fatalf("setup: the motion did not record the folder copy: %+v", hovered.drag)
	}

	got, cmd := updateModelCmd(t, hovered, dragReleaseAt(dragRightX, dragRowY(0), tea.ModCtrl))
	if cmd == nil {
		t.Fatal("a copy drop on a folder started no operation")
	}
	after := runDrop(t, got, cmd)
	if after.opErr != nil {
		t.Fatalf("the copy failed: %v", after.opErr)
	}
	if want := "copied 1 file(s)"; after.opResult != want {
		t.Errorf("the copy reported %q, want %q", after.opResult, want)
	}
	if _, err := os.Stat(filepath.Join(right, "sub", name)); err != nil {
		t.Errorf("%s was not copied into the folder: %v", name, err)
	}
	if _, err := os.Stat(filepath.Join(left, name)); err != nil {
		t.Errorf("a copy removed the original: %v", err)
	}
}

// Dropping a folder into itself, or into a folder below it, is refused: the
// operation cannot finish, so it must not start.
func TestDragDropFolderIntoItselfIsRefused(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "folder", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}

	rootFiles, err := fs.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	innerFiles, err := fs.ReadDir(filepath.Join(root, "folder"))
	if err != nil {
		t.Fatal(err)
	}

	m := testModel(t, config.Config{ThemePath: filepath.Join(root, "none.toml")})
	m.cfg.Mouse = true
	m.leftPath, m.leftFiles = root, rootFiles
	m.rightPath, m.rightFiles = filepath.Join(root, "folder"), innerFiles
	m.width, m.height = 100, 30
	m.updatePreview()

	// Row 0 of the left pane is the folder itself; row 0 of the right pane is
	// the folder inside it.
	pressed := updateModel(t, m, clickAt(dragLeftX, dragRowY(0)))
	if pressed.drag == nil || !pressed.drag.entry.IsDir {
		t.Fatalf("setup: the press did not arm a drag of the folder: %+v", pressed.drag)
	}
	hovered := updateModel(t, pressed, dragMotionAt(dragRightX, dragRowY(0), 0))
	if hovered.drag == nil || hovered.drag.targetRow != 0 {
		t.Fatalf("setup: the motion did not target the inner folder: %+v", hovered.drag)
	}

	got, cmd := updateModelCmd(t, hovered, dragReleaseAt(dragRightX, dragRowY(0), 0))
	if cmd != nil {
		t.Error("a drop of a folder into its own descendant started an operation")
	}
	if want := "cannot move into itself"; got.opResult != want {
		t.Errorf("opResult = %q, want %q", got.opResult, want)
	}
	if _, err := os.Stat(filepath.Join(root, "folder")); err != nil {
		t.Errorf("the folder was disturbed by the refused drop: %v", err)
	}
}

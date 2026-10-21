package nav

type Action int

const (
	ActionNone Action = iota
	ActionUp
	ActionDown
	ActionLeft
	ActionRight
	ActionEnter
	ActionTab
	ActionQuit
	ActionStack
	ActionFilter
	ActionFuzzyFind
	ActionRefresh
	ActionClear
	ActionSort
	ActionTogglePreview
	ActionGoto
	ActionKeyList
	ActionCopy
	ActionPaste
	ActionDelete
	ActionMark
	ActionAdd
	ActionRename
	ActionJump
)

// Section groups actions for the key-list overlay.
type Section string

const (
	SectionNavigation Section = "navigation"
	SectionJump       Section = "jump"
	SectionFileOps    Section = "file_ops"
	SectionPaneView   Section = "pane_view"
	SectionSystem     Section = "system"
)

type ActionRow struct {
	Keys    string
	Desc    string
	Section Section
}

type Keybind struct {
	Key     string
	Action  Action
	Desc    string
	Section Section
}

// Keybinds is the single source of truth for dispatch (Lookup) and
// documentation (the key-list overlay).
var Keybinds = []Keybind{
	// Navigation (direct keys)
	{"up", ActionUp, "move up", SectionNavigation},
	{"down", ActionDown, "move down", SectionNavigation},
	{"left", ActionLeft, "parent directory", SectionNavigation},
	{"right", ActionRight, "enter directory", SectionNavigation},
	{"enter", ActionEnter, "enter dir / open file", SectionNavigation},
	{"tab", ActionTab, "switch pane", SectionNavigation},

	// Jump (Space + letter)
	{"space", ActionJump, "jump to file by first letter", SectionJump},

	// File Operations (Shift + key)
	{"shift+c", ActionCopy, "copy (yank)", SectionFileOps},
	{"shift+v", ActionPaste, "paste / move marked", SectionFileOps},
	{"shift+d", ActionDelete, "delete", SectionFileOps},
	{"shift+m", ActionMark, "mark / unmark", SectionFileOps},
	{"shift+a", ActionAdd, "add file / directory", SectionFileOps},
	{"shift+r", ActionRename, "rename", SectionFileOps},

	// Pane / View (Shift + key)
	{"shift+s", ActionStack, "stack / unstack panes", SectionPaneView},
	{"shift+o", ActionSort, "cycle sort order", SectionPaneView},
	{"shift+i", ActionTogglePreview, "toggle preview", SectionPaneView},
	{"shift+p", ActionFuzzyFind, "fuzzy find", SectionPaneView},
	{"shift+f", ActionFilter, "filter pane", SectionPaneView},
	{"shift+g", ActionGoto, "goto path", SectionPaneView},
	{"shift+e", ActionRefresh, "refresh pane", SectionPaneView},
	{"shift+k", ActionKeyList, "key list overlay", SectionPaneView},

	// System
	{"q", ActionQuit, "quit", SectionSystem},
	{"shift+q", ActionQuit, "quit", SectionSystem},
	{"esc", ActionClear, "cancel / clear / back", SectionSystem},
}

func KeyRows() []ActionRow {
	var rows []ActionRow
	index := map[Action]int{}
	for _, kb := range Keybinds {
		// Skip lowercase 'q' for display: it is the same key as shift+q, and
		// the shift+ form is the canonical one shown in the key list.
		if kb.Key == "q" {
			continue
		}
		// Display space as "space+key" for the jump action
		key := kb.Key
		if key == "space" {
			key = "space+key"
		}
		if i, ok := index[kb.Action]; ok {
			rows[i].Keys = rows[i].Keys + "/" + key
			continue
		}
		rows = append(rows, ActionRow{Keys: key, Desc: kb.Desc, Section: kb.Section})
		index[kb.Action] = len(rows) - 1
	}
	return rows
}

func Lookup(key string) Action {
	for _, kb := range Keybinds {
		if kb.Key == key {
			return kb.Action
		}
	}
	return ActionNone
}

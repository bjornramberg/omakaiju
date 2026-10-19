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
	ActionYank
	ActionMove
	ActionPaste
	ActionDelete
	ActionAdd
	ActionRename
	ActionFilter
	ActionFuzzyFind
	ActionRefresh
	ActionClear
	ActionQuit
	ActionStack
	ActionSort
	ActionTogglePreview
	ActionGoto
	ActionKeyList
)

type ActionRow struct {
	Keys string
	Desc string
}

type Keybind struct {
	Key    string
	Action Action
	// Desc is the human-readable label shown in the key-list overlay. Every
	// entry must carry one so the overlay built from this table stays
	// self-explanatory.
	Desc string
}

// Keybinds is the single source of truth for dispatch (Lookup) and
// documentation (the key-list overlay). Equivalent keys for one action are
// listed here separately and merged for display by KeyRows.
var Keybinds = []Keybind{
	{"k", ActionUp, "up"},
	{"up", ActionUp, "up"},
	{"j", ActionDown, "down"},
	{"down", ActionDown, "down"},
	{"h", ActionLeft, "out / back"},
	{"left", ActionLeft, "out / back"},
	{"l", ActionRight, "in / forward"},
	{"right", ActionRight, "in / forward"},
	{"enter", ActionEnter, "enter dir, open file"},
	{"tab", ActionTab, "switch pane"},
	{"y", ActionYank, "yank (copy)"},
	{"m", ActionMove, "mark / unmark"},
	{"p", ActionPaste, "paste, else move marked"},
	{"d", ActionDelete, "delete prompt"},
	{"a", ActionAdd, "add file / dir"},
	{"r", ActionRename, "rename"},
	{"/", ActionFilter, "filter pane"},
	{"ctrl+f", ActionFuzzyFind, "fuzzy find"},
	{"ctrl+r", ActionRefresh, "refresh pane"},
	{"esc", ActionClear, "cancel / back"},
	{"q", ActionQuit, "quit"},
	{"s", ActionStack, "stack panes"},
	{"o", ActionSort, "sort order"},
	{"i", ActionTogglePreview, "toggle preview"},
	{"g", ActionGoto, "goto path"},
	{"ctrl+k", ActionKeyList, "key list"},
}

// KeyRows complies the bindings into overlay rows: one row per action, keys
// merged as "h/left", in first-seen order. Motion keys therefore collapse
// naturally instead of reading as duplicates.
func KeyRows() []ActionRow {
	var rows []ActionRow
	index := map[Action]int{}
	for _, kb := range Keybinds {
		if i, ok := index[kb.Action]; ok {
			rows[i].Keys = rows[i].Keys + "/" + kb.Key
			continue
		}
		rows = append(rows, ActionRow{Keys: kb.Key, Desc: kb.Desc})
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

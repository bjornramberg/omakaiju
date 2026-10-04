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
)

type Keybind struct {
	Key    string
	Action Action
}

var Keybinds = []Keybind{
	{"k", ActionUp},
	{"up", ActionUp},
	{"j", ActionDown},
	{"down", ActionDown},
	{"h", ActionLeft},
	{"left", ActionLeft},
	{"l", ActionRight},
	{"right", ActionRight},
	{"enter", ActionEnter},
	{"tab", ActionTab},
	{"y", ActionYank},
	{"m", ActionMove},
	{"p", ActionPaste},
	{"d", ActionDelete},
	{"a", ActionAdd},
	{"r", ActionRename},
	{"/", ActionFilter},
	{"ctrl+f", ActionFuzzyFind},
	{"ctrl+r", ActionRefresh},
	{"esc", ActionClear},
	{"q", ActionQuit},
}

func Lookup(key string) Action {
	for _, kb := range Keybinds {
		if kb.Key == key {
			return kb.Action
		}
	}
	return ActionNone
}

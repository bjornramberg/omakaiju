package nav

type Navigator struct {
	cursor int
	offset int
}

func NewNavigator() Navigator {
	return Navigator{}
}

func (n *Navigator) MoveUp() {
	if n.cursor > 0 {
		n.cursor--
	}
}

func (n *Navigator) MoveDown(itemCount int) {
	if n.cursor < itemCount-1 {
		n.cursor++
	}
}

func (n *Navigator) Cursor() int {
	return n.cursor
}

func (n *Navigator) Offset() int {
	return n.offset
}

func (n *Navigator) SetOffset(offset int) {
	n.offset = offset
}

func (n *Navigator) Reset() {
	n.cursor = 0
	n.offset = 0
}

package tui

// notesScreen is one layer in the notes UI (browse, search overlay, editor, etc.).
type notesScreen int

const (
	notesScreenList notesScreen = iota
	notesScreenSearch
	notesScreenEdit
	notesScreenActionables
)

// notesNavStack tracks nested notes screens so esc/back returns to the previous layer.
type notesNavStack struct {
	frames []notesScreen
}

func newNotesNav(initial notesScreen) notesNavStack {
	return notesNavStack{frames: []notesScreen{initial}}
}

func (s *notesNavStack) reset(initial notesScreen) {
	s.frames = []notesScreen{initial}
}

func (s *notesNavStack) push(frame notesScreen) {
	s.frames = append(s.frames, frame)
}

func (s *notesNavStack) pop() (notesScreen, bool) {
	if len(s.frames) <= 1 {
		return s.frames[0], false
	}
	s.frames = s.frames[:len(s.frames)-1]
	return s.frames[len(s.frames)-1], true
}

func (s *notesNavStack) peek() notesScreen {
	return s.frames[len(s.frames)-1]
}

func (s notesNavStack) canPop() bool {
	return len(s.frames) > 1
}

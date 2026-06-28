package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type noteMode int

const (
	noteModeInsert noteMode = iota
	noteModeNormal
	noteModeVisual
	noteModeCommand
	noteModeSearch
)

// noteAction tells the caller what to do after a keypress.
type noteAction int

const (
	noteActNone noteAction = iota
	noteActSave
	noteActSaveQuit
	noteActQuit
	noteActBrowse
	noteActNew
)

// vimNoteEditor wraps bubbles' textarea with vim-style modal editing.
// bubbles v2 ships a multi-line textarea (emacs-ish defaults); there is no
// built-in vim mode (see charmbracelet/bubbles#207). This is a small local
// wrapper rather than pulling in vim-bubble, which targets bubbletea v1.
type vimNoteEditor struct {
	ta      textarea.Model
	mode    noteMode
	cmdLine string
	lastZ   bool
	pending string // operator or motion prefix: d,c,y,g,r,f,F,t,T
	count   int

	visualKind visualKind
	selAnchor  selPos

	undoStack   []editorSnapshot
	redoStack   []editorSnapshot
	insertSaved bool
	yankBuf     string
	lastFind    *findMotion
	searchLine  string
	searchBack  bool
	lastSearch  noteSearchState
}

func newVimNoteEditor(width, height int) vimNoteEditor {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Placeholder = "type i to insert · :wq to save & browse"
	ta.Prompt = "  "
	ta.CharLimit = 0
	ta.MaxHeight = 0
	ta.MaxContentHeight = 0
	ta.SetWidth(width)
	ta.SetHeight(height)
	ta.Focus()

	// Insert-mode keys only; ctrl/cmd allowed for paste, nothing else.
	ta.KeyMap = vimTextareaKeyMap()

	return vimNoteEditor{ta: ta, mode: noteModeInsert, yankBuf: noteYankRegister}
}

// vimTextareaKeyMap is the bubbles textarea keymap without emacs ctrl shortcuts.
// ctrl/cmd+v is kept for paste in insert mode.
func vimTextareaKeyMap() textarea.KeyMap {
	return textarea.KeyMap{
		CharacterForward:        key.NewBinding(key.WithKeys("right")),
		CharacterBackward:       key.NewBinding(key.WithKeys("left")),
		WordForward:             key.NewBinding(key.WithKeys("alt+f")),
		WordBackward:            key.NewBinding(key.WithKeys("alt+b")),
		LineNext:                key.NewBinding(key.WithKeys("down")),
		LinePrevious:            key.NewBinding(key.WithKeys("up")),
		DeleteWordBackward:      key.NewBinding(key.WithKeys("alt+backspace")),
		DeleteWordForward:       key.NewBinding(key.WithKeys("alt+d")),
		InsertNewline:           key.NewBinding(key.WithKeys("enter")),
		DeleteCharacterBackward: key.NewBinding(key.WithKeys("backspace")),
		DeleteCharacterForward:  key.NewBinding(key.WithKeys("delete")),
		LineStart:               key.NewBinding(key.WithKeys("home")),
		LineEnd:                 key.NewBinding(key.WithKeys("end")),
		PageUp:                  key.NewBinding(key.WithKeys("pgup")),
		PageDown:                key.NewBinding(key.WithKeys("pgdown")),
		Paste:                   key.NewBinding(key.WithKeys("ctrl+v", "cmd+v")),
		InputBegin:              key.NewBinding(key.WithKeys("alt+<")),
		InputEnd:                key.NewBinding(key.WithKeys("alt+>")),
		CapitalizeWordForward:   key.NewBinding(key.WithKeys("alt+c")),
		LowercaseWordForward:    key.NewBinding(key.WithKeys("alt+l")),
		UppercaseWordForward:    key.NewBinding(key.WithKeys("alt+u")),
	}
}

func (e *vimNoteEditor) Resize(width, height int) {
	e.ta.SetWidth(width)
	e.ta.SetHeight(height)
}

// editorInfoHint returns a status line when the note overflows the viewport.
func (e *vimNoteEditor) editorInfoHint(s Styles) string {
	if hint := e.searchInfoHint(); hint != "" {
		return s.Dim.Render(hint)
	}
	viewH := e.ta.Height()
	if viewH <= 0 {
		return ""
	}
	top := e.ta.ScrollYOffset()
	below := e.ta.ScrollPercent() < 1.0
	above := top > 0
	lineCount := e.ta.LineCount()
	// Fallback when soft-wrap makes visual height exceed the viewport.
	if !above && !below && lineCount <= viewH {
		return ""
	}

	var parts []string
	if lineCount > viewH {
		parts = append(parts, fmt.Sprintf("%d lines", lineCount))
	}
	if above {
		parts = append(parts, "↑ more above")
	}
	if below {
		parts = append(parts, "↓ more below")
	}
	if !above && !below && lineCount > viewH {
		parts = append(parts, "scrollable")
	}
	parts = append(parts, "H/L/M")
	return s.Dim.Render(strings.Join(parts, "  ·  "))
}

func (e *vimNoteEditor) Load(body string) {
	e.ta.SetValue(body)
	e.mode = noteModeInsert
	e.cmdLine = ""
	e.clearPending()
	e.lastZ = false
	e.undoStack = e.undoStack[:0]
	e.redoStack = e.redoStack[:0]
	e.insertSaved = false
	e.visualKind = visualChar
	e.selAnchor = selPos{}
	e.yankBuf = noteYankRegister
	e.ta.Focus()
}

func (e *vimNoteEditor) Reset() {
	e.ta.Reset()
	e.mode = noteModeInsert
	e.cmdLine = ""
	e.lastZ = false
	e.clearPending()
	e.undoStack = e.undoStack[:0]
	e.redoStack = e.redoStack[:0]
	e.insertSaved = false
	e.visualKind = visualChar
	e.selAnchor = selPos{}
	e.yankBuf = noteYankRegister
	e.ta.Focus()
}

func (e *vimNoteEditor) Blur() { e.ta.Blur() }

func (e *vimNoteEditor) Value() string { return e.ta.Value() }

func (e *vimNoteEditor) ModeLabel() string {
	switch e.mode {
	case noteModeInsert:
		return "-- INSERT --"
	case noteModeVisual:
		if e.visualKind == visualLine {
			return "-- VISUAL LINE --"
		}
		return "-- VISUAL --"
	case noteModeCommand:
		return ":" + e.cmdLine
	case noteModeSearch:
		return e.searchLabel()
	default:
		return "-- NORMAL --"
	}
}

func (e *vimNoteEditor) feed(keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		e.ta, cmd = e.ta.Update(tea.KeyPressMsg{Text: k})
	}
	return cmd
}

func (e *vimNoteEditor) Update(msg tea.Msg) (tea.Cmd, noteAction) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return e.handleKey(msg)
	case tea.PasteMsg:
		if e.mode != noteModeInsert {
			return nil, noteActNone
		}
		return e.noteInsertPaste(msg)
	}
	var cmd tea.Cmd
	e.ta, cmd = e.ta.Update(msg)
	return cmd, noteActNone
}

func (e *vimNoteEditor) handleKey(msg tea.KeyPressMsg) (tea.Cmd, noteAction) {
	k := msg.String()

	switch e.mode {
	case noteModeInsert:
		switch k {
		case "esc":
			e.mode = noteModeNormal
			e.clearPending()
			e.insertSaved = false
			return e.feed("left"), noteActNone
		case "tab":
			return e.noteInsertTab(), noteActNone
		case "ctrl+c", "cmd+c":
			e.noteCopy()
			return nil, noteActNone
		case "ctrl+f":
			e.beginSearch(false)
			return nil, noteActNone
		default:
			return e.noteInsertKey(msg)
		}

	case noteModeSearch:
		return e.handleSearchKey(msg)

	case noteModeCommand:
		switch k {
		case "esc":
			e.mode = noteModeNormal
			e.cmdLine = ""
			return nil, noteActNone
		case "enter":
			act := e.execCommand(strings.TrimSpace(e.cmdLine))
			e.cmdLine = ""
			if act == noteActNone {
				e.mode = noteModeNormal
			}
			return nil, act
		case "backspace":
			if len(e.cmdLine) > 0 {
				e.cmdLine = e.cmdLine[:len(e.cmdLine)-1]
			}
			if e.cmdLine == "" {
				e.mode = noteModeNormal
			}
			return nil, noteActNone
		default:
			if len(k) == 1 && k[0] >= 32 {
				e.cmdLine += k
			}
			return nil, noteActNone
		}

	case noteModeVisual:
		return e.handleVisualKey(k)

	case noteModeNormal:
		return e.handleNormalKey(k)
	}
	return nil, noteActNone
}

func (e *vimNoteEditor) handleNormalKey(k string) (tea.Cmd, noteAction) {
	if cmd, act, ok := e.consumePendingKey(k); ok {
		return cmd, act
	}

	if k >= "1" && k <= "9" || k == "0" && e.count > 0 {
		e.count = e.count*10 + int(k[0]-'0')
		return nil, noteActNone
	}

	if k != "Z" {
		e.lastZ = false
	}

	switch k {
	case "esc":
		e.clearPending()
		return nil, noteActNone
	case "u":
		e.clearPending()
		return e.undo(), noteActNone
	case "i":
		e.beginInsert()
		return nil, noteActNone
	case "v":
		e.clearPending()
		e.enterVisual(visualChar)
		return nil, noteActNone
	case "V":
		e.clearPending()
		e.enterVisual(visualLine)
		return nil, noteActNone
	case "a":
		e.beginInsert()
		return e.feed("right"), noteActNone
	case "A":
		e.beginInsert()
		return e.feed("end"), noteActNone
	case "I":
		e.beginInsert()
		return e.feed("home"), noteActNone
	case "o":
		e.beginInsert()
		return e.feed("end", "enter"), noteActNone
	case "O":
		e.beginInsert()
		return e.feed("home", "enter", "up"), noteActNone
	case "h", "j", "k", "l", "w", "b", "0", "$", "{", "}", "+", "enter", "-", "H", "M", "L", "pgup", "pgdown":
		e.clearPending()
		return e.execMotion(k), noteActNone
	case "e":
		e.clearPending()
		e.moveWordEnd()
		return nil, noteActNone
	case "^":
		e.clearPending()
		e.moveToFirstNonBlank()
		return nil, noteActNone
	case "g":
		e.pending = "g"
		return nil, noteActNone
	case "G":
		e.clearPending()
		e.ta.MoveToEnd()
		return nil, noteActNone
	case "f":
		e.pending = "f"
		return nil, noteActNone
	case "F":
		e.pending = "F"
		return nil, noteActNone
	case "t":
		e.pending = "t"
		return nil, noteActNone
	case "T":
		e.pending = "T"
		return nil, noteActNone
	case ";":
		e.clearPending()
		e.repeatFind(false)
		return nil, noteActNone
	case ",":
		e.clearPending()
		e.repeatFind(true)
		return nil, noteActNone
	case "x":
		e.clearPending()
		e.pushUndo()
		return e.feed("delete"), noteActNone
	case "X":
		e.clearPending()
		e.pushUndo()
		e.applyDelete(e.spanCharBack())
		return nil, noteActNone
	case "s":
		e.clearPending()
		e.pushUndo()
		e.applyDelete(e.spanCharForward())
		e.beginInsert()
		e.insertSaved = true
		return nil, noteActNone
	case "S":
		e.clearPending()
		e.pushUndo()
		e.deleteWholeLines()
		e.beginInsert()
		e.insertSaved = true
		return nil, noteActNone
	case "C":
		e.clearPending()
		e.pushUndo()
		e.applyDelete(e.spanLineEnd())
		e.beginInsert()
		e.insertSaved = true
		return nil, noteActNone
	case "D":
		e.clearPending()
		e.pushUndo()
		e.applyDelete(e.spanLineEnd())
		return nil, noteActNone
	case "J":
		e.clearPending()
		e.pushUndo()
		e.joinLines()
		return nil, noteActNone
	case "p":
		e.clearPending()
		e.pushUndo()
		e.pasteAfter()
		return nil, noteActNone
	case "P":
		e.clearPending()
		e.pushUndo()
		e.pasteBefore()
		return nil, noteActNone
	case "~":
		e.clearPending()
		e.pushUndo()
		e.toggleCase()
		return nil, noteActNone
	case "ctrl+c", "cmd+c":
		e.noteCopy()
		return nil, noteActNone
	case "ctrl+f":
		e.beginSearch(false)
		return nil, noteActNone
	case "/":
		e.beginSearch(false)
		return nil, noteActNone
	case "?":
		e.beginSearch(true)
		return nil, noteActNone
	case "n":
		e.clearPending()
		e.searchNext(false)
		return nil, noteActNone
	case "N":
		e.clearPending()
		e.searchNext(true)
		return nil, noteActNone
	case "r":
		e.pending = "r"
		return nil, noteActNone
	case "d", "c", "y":
		e.pending = k
		return nil, noteActNone
	case "Z":
		e.clearPending()
		if e.lastZ {
			return nil, noteActSaveQuit
		}
		e.lastZ = true
		return nil, noteActNone
	case ":":
		e.mode = noteModeCommand
		e.cmdLine = ""
		e.clearPending()
		return nil, noteActNone
	default:
		e.clearPending()
		return nil, noteActNone
	}
}

func (e *vimNoteEditor) execCommand(cmd string) noteAction {
	switch cmd {
	case "w":
		return noteActSave
	case "wq", "x":
		return noteActSaveQuit
	case "q", "q!":
		return noteActQuit
	case "e":
		return noteActBrowse
	case "new":
		return noteActNew
	case "undo":
		e.undo()
		return noteActNone
	case "redo":
		e.redo()
		return noteActNone
	default:
		return noteActNone
	}
}

func modeStyle(mode noteMode, s Styles) lipgloss.Style {
	switch mode {
	case noteModeInsert:
		return lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	case noteModeVisual:
		return lipgloss.NewStyle().Foreground(colMagenta).Bold(true)
	default:
		return s.Dim
	}
}

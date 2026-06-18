package tui

import (
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
	noteModeCommand
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
	pending string // partial normal-mode command (e.g. "g" before "gg")
}

func newVimNoteEditor(width, height int) vimNoteEditor {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Placeholder = "type i to insert · :wq to save & browse"
	ta.Prompt = "  "
	ta.SetWidth(width)
	ta.SetHeight(height)
	ta.Focus()

	// Emacs-style movement keys only; letter keys are handled by our modes.
	km := textarea.DefaultKeyMap()
	km.CharacterForward = key.NewBinding(key.WithKeys("right"))
	km.CharacterBackward = key.NewBinding(key.WithKeys("left"))
	km.LineNext = key.NewBinding(key.WithKeys("down"))
	km.LinePrevious = key.NewBinding(key.WithKeys("up"))
	km.WordForward = key.NewBinding(key.WithKeys("alt+f"))
	km.WordBackward = key.NewBinding(key.WithKeys("alt+b"))
	km.LineStart = key.NewBinding(key.WithKeys("home"))
	km.LineEnd = key.NewBinding(key.WithKeys("end"))
	km.Paste = key.NewBinding() // paste via terminal only, not ctrl+v
	ta.KeyMap = km

	return vimNoteEditor{ta: ta, mode: noteModeInsert}
}

func (e *vimNoteEditor) Resize(width, height int) {
	e.ta.SetWidth(width)
	e.ta.SetHeight(height)
}

func (e *vimNoteEditor) Load(body string) {
	e.ta.SetValue(body)
	e.mode = noteModeInsert
	e.cmdLine = ""
	e.pending = ""
	e.lastZ = false
	e.ta.Focus()
}

func (e *vimNoteEditor) Reset() {
	e.ta.Reset()
	e.mode = noteModeInsert
	e.cmdLine = ""
	e.lastZ = false
	e.pending = ""
	e.ta.Focus()
}

func (e *vimNoteEditor) Blur() { e.ta.Blur() }

func (e *vimNoteEditor) Value() string { return e.ta.Value() }

func (e *vimNoteEditor) View() string { return e.ta.View() }

func (e *vimNoteEditor) ModeLabel() string {
	switch e.mode {
	case noteModeInsert:
		return "-- INSERT --"
	case noteModeCommand:
		return ":" + e.cmdLine
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
		if e.mode == noteModeInsert {
			var cmd tea.Cmd
			e.ta, cmd = e.ta.Update(msg)
			return cmd, noteActNone
		}
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
			e.pending = ""
			return e.feed("left"), noteActNone
		default:
			var cmd tea.Cmd
			e.ta, cmd = e.ta.Update(msg)
			return cmd, noteActNone
		}

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

	default: // normal
		if k != "Z" {
			e.lastZ = false
		}
		switch k {
		case "esc":
			e.pending = ""
			return nil, noteActNone
		case "i":
			e.mode = noteModeInsert
			e.pending = ""
			return nil, noteActNone
		case "a":
			e.mode = noteModeInsert
			e.pending = ""
			return e.feed("right"), noteActNone
		case "A":
			e.mode = noteModeInsert
			e.pending = ""
			return e.feed("end"), noteActNone
		case "I":
			e.mode = noteModeInsert
			e.pending = ""
			return e.feed("home"), noteActNone
		case "o":
			e.mode = noteModeInsert
			e.pending = ""
			return e.feed("end", "enter"), noteActNone
		case "O":
			e.mode = noteModeInsert
			e.pending = ""
			return e.feed("home", "enter", "up"), noteActNone
		case "h":
			e.pending = ""
			return e.feed("left"), noteActNone
		case "j":
			e.pending = ""
			return e.feed("down"), noteActNone
		case "k":
			e.pending = ""
			return e.feed("up"), noteActNone
		case "l":
			e.pending = ""
			return e.feed("right"), noteActNone
		case "w":
			e.pending = ""
			return e.feed("alt+f"), noteActNone
		case "b":
			e.pending = ""
			return e.feed("alt+b"), noteActNone
		case "e":
			e.pending = ""
			return e.feed("alt+f"), noteActNone // close enough for notes
		case "0":
			e.pending = ""
			return e.feed("home"), noteActNone
		case "$":
			e.pending = ""
			return e.feed("end"), noteActNone
		case "g":
			if e.pending == "g" {
				e.pending = ""
				e.ta.MoveToBegin()
				return nil, noteActNone
			}
			e.pending = "g"
			return nil, noteActNone
		case "G":
			e.pending = ""
			e.ta.MoveToEnd()
			return nil, noteActNone
		case "x":
			e.pending = ""
			return e.feed("delete"), noteActNone
		case "d":
			if e.pending == "d" {
				e.pending = ""
				return e.deleteLine(), noteActNone
			}
			e.pending = "d"
			return nil, noteActNone
		case "Z":
			if e.lastZ {
				return nil, noteActSaveQuit
			}
			e.lastZ = true
			return nil, noteActNone
		case ":":
			e.mode = noteModeCommand
			e.cmdLine = ""
			e.pending = ""
			return nil, noteActNone
		default:
			e.pending = ""
			return nil, noteActNone
		}
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
	default:
		return noteActNone
	}
}

func (e *vimNoteEditor) deleteLine() tea.Cmd {
	lines := strings.Split(e.ta.Value(), "\n")
	if len(lines) <= 1 {
		e.ta.SetValue("")
		return nil
	}
	row := e.ta.Line()
	if row < 0 || row >= len(lines) {
		row = len(lines) - 1
	}
	out := append(lines[:row], lines[row+1:]...)
	e.ta.SetValue(strings.Join(out, "\n"))
	return nil
}

func modeStyle(mode noteMode, s Styles) lipgloss.Style {
	switch mode {
	case noteModeInsert:
		return lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	default:
		return s.Dim
	}
}

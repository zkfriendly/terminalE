package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

type visualKind int

const (
	visualChar visualKind = iota
	visualLine
)

type selPos struct {
	line int
	col  int
}

func orderSel(a, b selPos) (from, to selPos) {
	if a.line < b.line || (a.line == b.line && a.col <= b.col) {
		return a, b
	}
	return b, a
}

func (e *vimNoteEditor) enterVisual(kind visualKind) {
	e.mode = noteModeVisual
	e.visualKind = kind
	e.selAnchor = selPos{e.ta.Line(), e.ta.Column()}
	e.clearPending()
}

func (e *vimNoteEditor) selectionEndpoints() (from, to selPos) {
	cur := selPos{e.ta.Line(), e.ta.Column()}
	from, to = orderSel(e.selAnchor, cur)
	if e.visualKind == visualLine {
		lines := noteLines(e.ta.Value())
		from.col = 0
		if to.line < len(lines) {
			to.col = len(lineRunes(lines, to.line))
		}
	}
	return from, to
}

func (e *vimNoteEditor) selectionSpan() deleteSpan {
	from, to := e.selectionEndpoints()
	lines := noteLines(e.ta.Value())
	toCol := to.col + 1
	if to.line < len(lines) {
		max := len(lineRunes(lines, to.line))
		if toCol > max {
			toCol = max
		}
	}
	return deleteSpan{from.line, from.col, to.line, toCol}
}

func (e *vimNoteEditor) selectionText() string {
	span := e.selectionSpan()
	lines := noteLines(e.ta.Value())
	if span.fromLine == span.toLine {
		lr := lineRunes(lines, span.fromLine)
		from, to := span.fromCol, span.toCol
		if from > to {
			from, to = to, from
		}
		if from > len(lr) {
			from = len(lr)
		}
		if to > len(lr) {
			to = len(lr)
		}
		return string(lr[from:to])
	}
	var b strings.Builder
	for i := span.fromLine; i <= span.toLine && i < len(lines); i++ {
		if i > span.fromLine {
			b.WriteByte('\n')
		}
		lr := lineRunes(lines, i)
		from, to := 0, len(lr)
		if i == span.fromLine {
			from = span.fromCol
		}
		if i == span.toLine {
			to = span.toCol
		}
		if from > len(lr) {
			from = len(lr)
		}
		if to > len(lr) {
			to = len(lr)
		}
		if from > to {
			from, to = to, from
		}
		b.WriteString(string(lr[from:to]))
	}
	return b.String()
}

func (e *vimNoteEditor) yankSelection() {
	text := e.selectionText()
	if e.visualKind == visualLine && text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	e.setYank(text)
}

func (e *vimNoteEditor) deleteSelection() {
	span := e.selectionSpan()
	e.pushUndo()
	e.applyDelete(span)
	e.mode = noteModeNormal
}

func (e *vimNoteEditor) changeSelection() {
	span := e.selectionSpan()
	e.pushUndo()
	e.applyDelete(span)
	e.beginInsert()
	e.insertSaved = true
}

func (e *vimNoteEditor) visualSwapEnds() {
	cur := selPos{e.ta.Line(), e.ta.Column()}
	e.restoreCursor(e.selAnchor.line, e.selAnchor.col)
	e.selAnchor = cur
}

func posSelected(line, col int, from, to selPos) bool {
	from, to = orderSel(from, to)
	if line < from.line || line > to.line {
		return false
	}
	if from.line == to.line {
		return line == from.line && col >= from.col && col <= to.col
	}
	if line == from.line {
		return col >= from.col
	}
	if line == to.line {
		return col <= to.col
	}
	return true
}

func (e *vimNoteEditor) handleVisualKey(k string) (tea.Cmd, noteAction) {
	if cmd, act, ok := e.consumeVisualPending(k); ok {
		return cmd, act
	}

	switch k {
	case "esc":
		e.mode = noteModeNormal
		return nil, noteActNone
	case "y":
		e.yankSelection()
		e.mode = noteModeNormal
		return nil, noteActNone
	case "d", "x":
		e.deleteSelection()
		return nil, noteActNone
	case "c":
		e.changeSelection()
		return nil, noteActNone
	case "o":
		e.visualSwapEnds()
		return nil, noteActNone
	case "ctrl+c", "cmd+c":
		e.noteCopy()
		return nil, noteActNone
	case "v":
		e.visualKind = visualChar
		return nil, noteActNone
	case "V":
		e.visualKind = visualLine
		return nil, noteActNone
	case "g":
		e.pending = "g"
		return nil, noteActNone
	case "G":
		e.ta.MoveToEnd()
		return nil, noteActNone
	case "~":
		e.pushUndo()
		e.toggleCase()
		return nil, noteActNone
	default:
		if cmd := e.execMotion(k); cmd != nil || isMotionKey(k) || k == ";" || k == "," {
			if k == ";" {
				e.repeatFind(false)
				return nil, noteActNone
			}
			if k == "," {
				e.repeatFind(true)
				return nil, noteActNone
			}
			return cmd, noteActNone
		}
	}
	return nil, noteActNone
}

func (e *vimNoteEditor) consumeVisualPending(k string) (tea.Cmd, noteAction, bool) {
	if e.pending == "" {
		return nil, noteActNone, false
	}
	if e.pending == "g" {
		e.pending = ""
		if k == "g" {
			e.ta.MoveToBegin()
			return nil, noteActNone, true
		}
		return nil, noteActNone, false
	}
	if e.pending == "f" || e.pending == "F" || e.pending == "t" || e.pending == "T" {
		pending := e.pending
		e.pending = ""
		if r, ok := pendingKeyRune(k); ok {
			switch pending {
			case "f":
				_ = e.findOnLine(findMotion{}, r)
			case "F":
				_ = e.findOnLine(findMotion{back: true}, r)
			case "t":
				_ = e.findOnLine(findMotion{to: true}, r)
			case "T":
				_ = e.findOnLine(findMotion{to: true, back: true}, r)
			}
			return nil, noteActNone, true
		}
		return nil, noteActNone, false
	}
	e.pending = ""
	return nil, noteActNone, false
}

func (e *vimNoteEditor) execMotion(k string) tea.Cmd {
	switch k {
	case "h":
		return e.feed("left")
	case "j":
		return e.feed("down")
	case "k":
		return e.feed("up")
	case "l":
		return e.feed("right")
	case "w":
		return e.feed("alt+f")
	case "b":
		return e.feed("alt+b")
	case "e":
		e.moveWordEnd()
		return nil
	case "0":
		return e.feed("home")
	case "^":
		e.moveToFirstNonBlank()
		return nil
	case "$":
		return e.feed("end")
	case "{":
		e.moveParagraph(-1)
		return nil
	case "}":
		e.moveParagraph(1)
		return nil
	case "+", "enter":
		e.ta.CursorDown()
		e.moveToFirstNonBlank()
		return nil
	case "-":
		e.ta.CursorUp()
		e.moveToFirstNonBlank()
		return nil
	case "H":
		e.moveScreenLine(0)
		return nil
	case "M":
		e.moveScreenLine(e.ta.Height() / 2)
		return nil
	case "L":
		h := e.ta.Height()
		if h > 0 {
			e.moveScreenLine(h - 1)
		}
		return nil
	case "pgup":
		e.ta.PageUp()
		return nil
	case "pgdown":
		e.ta.PageDown()
		return nil
	case "f":
		e.pending = "f"
		return nil
	case "F":
		e.pending = "F"
		return nil
	case "t":
		e.pending = "t"
		return nil
	case "T":
		e.pending = "T"
		return nil
	default:
		return nil
	}
}

package tui

import "github.com/atotto/clipboard"

// noteYankRegister persists the last yanked text across note switches and
// editor instances (zone notes, all notes, new note buffers).
var noteYankRegister string

func (e *vimNoteEditor) setYank(text string) {
	e.yankBuf = text
	noteYankRegister = text
	copyToSystemClipboard(text)
}

func (e *vimNoteEditor) pasteSource() string {
	if e.yankBuf != "" {
		return e.yankBuf
	}
	if noteYankRegister != "" {
		e.yankBuf = noteYankRegister
		return noteYankRegister
	}
	text, err := clipboard.ReadAll()
	if err != nil || text == "" {
		return ""
	}
	e.yankBuf = text
	noteYankRegister = text
	return text
}

func copyToSystemClipboard(text string) {
	if text == "" {
		return
	}
	_ = clipboard.WriteAll(text)
}

func (e *vimNoteEditor) noteCopy() {
	switch e.mode {
	case noteModeVisual:
		e.yankSelection()
	case noteModeInsert:
		lines := noteLines(e.ta.Value())
		line := e.ta.Line()
		if line < len(lines) {
			e.setYank(lines[line])
		}
	default:
		if text := e.pasteSource(); text != "" {
			copyToSystemClipboard(text)
		}
	}
}

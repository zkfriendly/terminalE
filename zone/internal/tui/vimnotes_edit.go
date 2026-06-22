package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

const maxUndoSteps = 200

type editorSnapshot struct {
	value string
	line  int
	col   int
}

type findMotion struct {
	char rune
	to   bool // t/T vs f/F
	back bool // F/T
}

func noteLines(s string) []string {
	if s == "" {
		return []string{""}
	}
	return strings.Split(s, "\n")
}

func lineRunes(lines []string, line int) []rune {
	if line < 0 || line >= len(lines) {
		return nil
	}
	return []rune(lines[line])
}

func isWordChar(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func (e *vimNoteEditor) snapshot() editorSnapshot {
	return editorSnapshot{
		value: e.ta.Value(),
		line:  e.ta.Line(),
		col:   e.ta.Column(),
	}
}

func (e *vimNoteEditor) restoreSnapshot(s editorSnapshot) {
	e.ta.SetValue(s.value)
	e.restoreCursor(s.line, s.col)
}

func (e *vimNoteEditor) restoreCursor(line, col int) {
	lines := noteLines(e.ta.Value())
	if len(lines) == 0 {
		return
	}
	if line < 0 {
		line = 0
	}
	if line >= len(lines) {
		line = len(lines) - 1
	}
	lr := lineRunes(lines, line)
	if col < 0 {
		col = 0
	}
	if col > len(lr) {
		col = len(lr)
	}

	e.ta.MoveToBegin()
	for range line {
		e.ta.CursorEnd()
		e.ta, _ = e.ta.Update(tea.KeyPressMsg{Text: "right"})
	}
	e.ta.SetCursorColumn(col)
}

func (e *vimNoteEditor) setValueAt(val string, line, col int) {
	e.ta.SetValue(val)
	e.restoreCursor(line, col)
}

func (e *vimNoteEditor) pushUndo() {
	s := e.snapshot()
	e.undoStack = append(e.undoStack, s)
	if len(e.undoStack) > maxUndoSteps {
		e.undoStack = e.undoStack[len(e.undoStack)-maxUndoSteps:]
	}
	e.redoStack = e.redoStack[:0]
}

func (e *vimNoteEditor) undo() tea.Cmd {
	if len(e.undoStack) == 0 {
		return nil
	}
	cur := e.snapshot()
	idx := len(e.undoStack) - 1
	prev := e.undoStack[idx]
	e.undoStack = e.undoStack[:idx]
	e.redoStack = append(e.redoStack, cur)
	e.restoreSnapshot(prev)
	e.insertSaved = false
	return nil
}

func (e *vimNoteEditor) redo() tea.Cmd {
	if len(e.redoStack) == 0 {
		return nil
	}
	cur := e.snapshot()
	idx := len(e.redoStack) - 1
	next := e.redoStack[idx]
	e.redoStack = e.redoStack[:idx]
	e.undoStack = append(e.undoStack, cur)
	e.restoreSnapshot(next)
	e.insertSaved = false
	return nil
}

func (e *vimNoteEditor) clearPending() {
	e.pending = ""
	e.count = 0
}

func (e *vimNoteEditor) repeatCount() int {
	if e.count <= 0 {
		return 1
	}
	return e.count
}

func (e *vimNoteEditor) beginInsert() {
	e.mode = noteModeInsert
	e.clearPending()
	e.insertSaved = false
}

func (e *vimNoteEditor) noteInsertKey(msg tea.KeyPressMsg) (tea.Cmd, noteAction) {
	if !e.insertSaved {
		e.pushUndo()
		e.insertSaved = true
	}
	var cmd tea.Cmd
	e.ta, cmd = e.ta.Update(msg)
	return cmd, noteActNone
}

func (e *vimNoteEditor) insertAtCursor(s string) {
	line, col := e.ta.Line(), e.ta.Column()
	lines := noteLines(e.ta.Value())
	if len(lines) == 0 {
		lines = []string{""}
	}
	if line < 0 {
		line = 0
	}
	if line >= len(lines) {
		line = len(lines) - 1
	}
	lr := lineRunes(lines, line)
	if col > len(lr) {
		col = len(lr)
	}
	insert := []rune(s)
	newLr := append(append(append([]rune(nil), lr[:col]...), insert...), lr[col:]...)
	lines[line] = string(newLr)
	e.setValueAt(strings.Join(lines, "\n"), line, col+len(insert))
}

func (e *vimNoteEditor) noteInsertTab() tea.Cmd {
	if !e.insertSaved {
		e.pushUndo()
		e.insertSaved = true
	}
	// bubbles textarea sanitizes '\t' to four spaces; insert that directly.
	e.insertAtCursor("    ")
	return nil
}

func (e *vimNoteEditor) noteInsertPaste(msg tea.PasteMsg) (tea.Cmd, noteAction) {
	if !e.insertSaved {
		e.pushUndo()
		e.insertSaved = true
	}
	var cmd tea.Cmd
	e.ta, cmd = e.ta.Update(msg)
	return cmd, noteActNone
}

func (e *vimNoteEditor) firstNonBlankCol(line int) int {
	lr := lineRunes(noteLines(e.ta.Value()), line)
	for i, r := range lr {
		if !unicode.IsSpace(r) {
			return i
		}
	}
	return 0
}

func (e *vimNoteEditor) moveToFirstNonBlank() {
	e.ta.SetCursorColumn(e.firstNonBlankCol(e.ta.Line()))
}

func (e *vimNoteEditor) moveScreenLine(offsetFromTop int) {
	top := e.ta.ScrollYOffset()
	line := top + offsetFromTop
	maxLine := e.ta.LineCount() - 1
	if maxLine < 0 {
		maxLine = 0
	}
	if line < 0 {
		line = 0
	}
	if line > maxLine {
		line = maxLine
	}
	col := e.ta.Column()
	e.restoreCursor(line, col)
}

func (e *vimNoteEditor) moveWordEnd() {
	line, col := e.ta.Line(), e.ta.Column()
	lr := lineRunes(noteLines(e.ta.Value()), line)
	if len(lr) == 0 {
		return
	}
	if col >= len(lr) {
		col = len(lr) - 1
	}
	if col < len(lr) && isWordChar(lr[col]) {
		for col+1 < len(lr) && isWordChar(lr[col+1]) {
			col++
		}
	} else {
		for col < len(lr) && !isWordChar(lr[col]) {
			col++
		}
		if col < len(lr) {
			for col+1 < len(lr) && isWordChar(lr[col+1]) {
				col++
			}
		} else {
			col = len(lr) - 1
		}
	}
	e.ta.SetCursorColumn(col)
}

func (e *vimNoteEditor) moveParagraph(delta int) {
	line := e.ta.Line()
	lines := noteLines(e.ta.Value())
	if delta > 0 {
		for line+1 < len(lines) {
			line++
			if strings.TrimSpace(lines[line]) == "" {
				for line+1 < len(lines) && strings.TrimSpace(lines[line+1]) == "" {
					line++
				}
				break
			}
		}
		if line >= len(lines) {
			line = len(lines) - 1
		}
	} else {
		for line > 0 {
			line--
			if strings.TrimSpace(lines[line]) == "" {
				break
			}
		}
	}
	e.restoreCursor(line, 0)
}

func (e *vimNoteEditor) findOnLine(f findMotion, ch rune) bool {
	line := e.ta.Line()
	col := e.ta.Column()
	lr := lineRunes(noteLines(e.ta.Value()), line)
	if len(lr) == 0 {
		return false
	}
	e.lastFind = &findMotion{char: ch, to: f.to, back: f.back}
	if f.back {
		start := col - 1
		if start >= len(lr) {
			start = len(lr) - 1
		}
		for i := start; i >= 0; i-- {
			if lr[i] == ch {
				target := i
				if f.to && i+1 < len(lr) {
					target = i + 1
				}
				e.ta.SetCursorColumn(target)
				return true
			}
		}
		return false
	}
	start := col + 1
	for i := start; i < len(lr); i++ {
		if lr[i] == ch {
			target := i
			if f.to && i > 0 {
				target = i - 1
			}
			e.ta.SetCursorColumn(target)
			return true
		}
	}
	return false
}

func (e *vimNoteEditor) repeatFind(reverse bool) {
	if e.lastFind == nil {
		return
	}
	f := *e.lastFind
	if reverse {
		f.back = !f.back
	}
	_ = e.findOnLine(f, f.char)
}

type deleteSpan struct {
	fromLine, fromCol int
	toLine, toCol     int
}

func (e *vimNoteEditor) spanWordForward() deleteSpan {
	line, col := e.ta.Line(), e.ta.Column()
	lr := lineRunes(noteLines(e.ta.Value()), line)
	end := col
	if end < len(lr) && isWordChar(lr[end]) {
		for end < len(lr) && isWordChar(lr[end]) {
			end++
		}
		for end < len(lr) && unicode.IsSpace(lr[end]) {
			end++
		}
	} else {
		for end < len(lr) && !isWordChar(lr[end]) {
			end++
		}
	}
	return deleteSpan{line, col, line, end}
}

func (e *vimNoteEditor) spanWordBack() deleteSpan {
	line, col := e.ta.Line(), e.ta.Column()
	lr := lineRunes(noteLines(e.ta.Value()), line)
	start := col
	if start > 0 && isWordChar(lr[start-1]) {
		for start > 0 && isWordChar(lr[start-1]) {
			start--
		}
	} else {
		for start > 0 && !isWordChar(lr[start-1]) {
			start--
		}
	}
	return deleteSpan{line, start, line, col}
}

func (e *vimNoteEditor) spanWordEnd() deleteSpan {
	line, col := e.ta.Line(), e.ta.Column()
	lr := lineRunes(noteLines(e.ta.Value()), line)
	end := col
	if end < len(lr) && isWordChar(lr[end]) {
		for end+1 < len(lr) && isWordChar(lr[end+1]) {
			end++
		}
		end++
	} else {
		for end < len(lr) && !isWordChar(lr[end]) {
			end++
		}
		if end < len(lr) {
			for end+1 < len(lr) && isWordChar(lr[end+1]) {
				end++
			}
			end++
		}
	}
	return deleteSpan{line, col, line, end}
}

func (e *vimNoteEditor) spanLineEnd() deleteSpan {
	line, col := e.ta.Line(), e.ta.Column()
	lr := lineRunes(noteLines(e.ta.Value()), line)
	return deleteSpan{line, col, line, len(lr)}
}

func (e *vimNoteEditor) spanLineStart() deleteSpan {
	line, col := e.ta.Line(), e.ta.Column()
	return deleteSpan{line, 0, line, col}
}

func (e *vimNoteEditor) spanFirstNonBlank() deleteSpan {
	line, col := e.ta.Line(), e.ta.Column()
	start := e.firstNonBlankCol(line)
	if start > col {
		start = col
	}
	return deleteSpan{line, start, line, col}
}

func (e *vimNoteEditor) spanCharForward() deleteSpan {
	line, col := e.ta.Line(), e.ta.Column()
	lr := lineRunes(noteLines(e.ta.Value()), line)
	end := col + 1
	if end > len(lr) {
		end = len(lr)
	}
	return deleteSpan{line, col, line, end}
}

func (e *vimNoteEditor) spanCharBack() deleteSpan {
	line, col := e.ta.Line(), e.ta.Column()
	start := col - 1
	if start < 0 {
		start = 0
	}
	return deleteSpan{line, start, line, col}
}

func (e *vimNoteEditor) applyDelete(span deleteSpan) {
	lines := noteLines(e.ta.Value())
	if span.fromLine == span.toLine {
		lr := lineRunes(lines, span.fromLine)
		if span.fromCol > len(lr) {
			span.fromCol = len(lr)
		}
		if span.toCol > len(lr) {
			span.toCol = len(lr)
		}
		if span.fromCol > span.toCol {
			span.fromCol, span.toCol = span.toCol, span.fromCol
		}
		newLine := string(append(append([]rune(nil), lr[:span.fromCol]...), lr[span.toCol:]...))
		lines[span.fromLine] = newLine
		e.setValueAt(strings.Join(lines, "\n"), span.fromLine, span.fromCol)
		return
	}

	// Multi-line delete (not used by current motions but kept for completeness).
	out := append([]string(nil), lines[:span.fromLine]...)
	mid := string(append(lineRunes(lines, span.fromLine)[:span.fromCol], lineRunes(lines, span.toLine)[span.toCol:]...))
	if mid != "" || span.fromLine == span.toLine {
		out = append(out, mid)
	}
	out = append(out, lines[span.toLine+1:]...)
	if len(out) == 0 {
		out = []string{""}
	}
	e.setValueAt(strings.Join(out, "\n"), span.fromLine, span.fromCol)
}

func (e *vimNoteEditor) yankSpan(span deleteSpan) {
	lines := noteLines(e.ta.Value())
	if span.fromLine == span.toLine {
		lr := lineRunes(lines, span.fromLine)
		if span.fromCol > len(lr) {
			span.fromCol = len(lr)
		}
		if span.toCol > len(lr) {
			span.toCol = len(lr)
		}
		if span.fromCol > span.toCol {
			span.fromCol, span.toCol = span.toCol, span.fromCol
		}
		e.setYank(string(lr[span.fromCol:span.toCol]))
		return
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
		b.WriteString(string(lr[from:to]))
	}
	e.setYank(b.String())
}

func (e *vimNoteEditor) deleteWholeLines() {
	line := e.ta.Line()
	col := e.ta.Column()
	lines := noteLines(e.ta.Value())
	if len(lines) == 0 {
		e.ta.SetValue("")
		return
	}
	n := e.repeatCount()
	if line >= len(lines) {
		line = len(lines) - 1
	}
	if line+n > len(lines) {
		n = len(lines) - line
	}
	text := strings.Join(lines[line:line+n], "\n")
	if n < len(lines) {
		text += "\n"
	}
	e.setYank(text)
	rest := append([]string(nil), lines[:line]...)
	rest = append(rest, lines[line+n:]...)
	if len(rest) == 0 {
		rest = []string{""}
	}
	newLine := line
	if newLine >= len(rest) {
		newLine = len(rest) - 1
	}
	newCol := min(col, len(lineRunes(rest, newLine)))
	e.setValueAt(strings.Join(rest, "\n"), newLine, newCol)
}

func (e *vimNoteEditor) yankWholeLines() {
	line := e.ta.Line()
	lines := noteLines(e.ta.Value())
	if line >= len(lines) {
		return
	}
	n := e.repeatCount()
	if line+n > len(lines) {
		n = len(lines) - line
	}
	text := strings.Join(lines[line:line+n], "\n")
	if n < len(lines) {
		text += "\n"
	}
	e.setYank(text)
}

func (e *vimNoteEditor) deleteByMotion(motion string) {
	switch motion {
	case "w":
		e.applyDelete(e.spanWordForward())
	case "b":
		e.applyDelete(e.spanWordBack())
	case "e":
		e.applyDelete(e.spanWordEnd())
	case "$":
		e.applyDelete(e.spanLineEnd())
	case "0":
		e.applyDelete(e.spanLineStart())
	case "^":
		e.applyDelete(e.spanFirstNonBlank())
	case "l", "h":
		if motion == "l" {
			e.applyDelete(e.spanCharForward())
		} else {
			e.applyDelete(e.spanCharBack())
		}
	}
}

func (e *vimNoteEditor) yankByMotion(motion string) {
	switch motion {
	case "w":
		e.yankSpan(e.spanWordForward())
	case "b":
		e.yankSpan(e.spanWordBack())
	case "e":
		e.yankSpan(e.spanWordEnd())
	case "$":
		e.yankSpan(e.spanLineEnd())
	case "0":
		e.yankSpan(e.spanLineStart())
	case "^":
		e.yankSpan(e.spanFirstNonBlank())
	}
}

func (e *vimNoteEditor) replaceChar(ch rune) {
	line, col := e.ta.Line(), e.ta.Column()
	lines := noteLines(e.ta.Value())
	lr := lineRunes(lines, line)
	if len(lr) == 0 {
		e.ta.InsertRune(ch)
		return
	}
	if col >= len(lr) {
		col = len(lr)
		lr = append(lr, ch)
	} else {
		lr[col] = ch
	}
	lines[line] = string(lr)
	e.setValueAt(strings.Join(lines, "\n"), line, col+1)
}

func (e *vimNoteEditor) toggleCase() {
	line, col := e.ta.Line(), e.ta.Column()
	lines := noteLines(e.ta.Value())
	lr := lineRunes(lines, line)
	if col >= len(lr) {
		return
	}
	r := lr[col]
	if unicode.IsUpper(r) {
		lr[col] = unicode.ToLower(r)
	} else if unicode.IsLower(r) {
		lr[col] = unicode.ToUpper(r)
	}
	lines[line] = string(lr)
	e.setValueAt(strings.Join(lines, "\n"), line, col+1)
}

func (e *vimNoteEditor) joinLines() {
	line := e.ta.Line()
	lines := noteLines(e.ta.Value())
	if line+1 >= len(lines) {
		return
	}
	lr := lineRunes(lines, line)
	nr := lineRunes(lines, line+1)
	if len(lr) > 0 && len(nr) > 0 && !unicode.IsSpace(lr[len(lr)-1]) && !unicode.IsSpace(nr[0]) {
		lr = append(lr, ' ')
	}
	lr = append(lr, nr...)
	lines[line] = string(lr)
	out := append(append([]string(nil), lines[:line+1]...), lines[line+2:]...)
	e.setValueAt(strings.Join(out, "\n"), line, len(lr))
}

func (e *vimNoteEditor) pasteAfter() {
	text := e.pasteSource()
	if text == "" {
		return
	}
	if strings.HasSuffix(text, "\n") {
		line := e.ta.Line()
		lines := noteLines(e.ta.Value())
		content := strings.TrimSuffix(text, "\n")
		yanked := strings.Split(content, "\n")
		insertAt := line + 1
		out := append(append(append([]string(nil), lines[:insertAt]...), yanked...), lines[insertAt:]...)
		e.setValueAt(strings.Join(out, "\n"), insertAt, 0)
		return
	}
	if strings.Contains(text, "\n") {
		line := e.ta.Line()
		col := e.ta.Column()
		lines := noteLines(e.ta.Value())
		lr := lineRunes(lines, line)
		before := string(lr[:col])
		after := string(lr[col:])
		parts := strings.Split(text, "\n")
		if len(parts) == 0 {
			return
		}
		if len(parts) == 1 {
			lines[line] = before + parts[0] + after
			e.setValueAt(strings.Join(lines, "\n"), line, col+len([]rune(parts[0])))
			return
		}
		lines[line] = before + parts[0]
		insert := parts[1 : len(parts)-1]
		last := parts[len(parts)-1] + after
		out := append(append(append([]string(nil), lines[:line+1]...), insert...), last)
		newLine := line + len(parts) - 1
		newCol := len([]rune(parts[len(parts)-1]))
		e.setValueAt(strings.Join(out, "\n"), newLine, newCol)
		return
	}
	e.ta.InsertString(text)
}

func (e *vimNoteEditor) pasteBefore() {
	text := e.pasteSource()
	if text == "" {
		return
	}
	if strings.Contains(text, "\n") {
		line := e.ta.Line()
		lines := noteLines(e.ta.Value())
		parts := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		out := append(append(append([]string(nil), lines[:line]...), parts...), lines[line:]...)
		e.setValueAt(strings.Join(out, "\n"), line, 0)
		return
	}
	col := e.ta.Column()
	e.ta.SetCursorColumn(max(0, col-1))
	e.ta.InsertString(text)
}

func isMotionKey(k string) bool {
	switch k {
	case "w", "b", "e", "$", "0", "^", "h", "l":
		return true
	default:
		return false
	}
}

func (e *vimNoteEditor) consumePendingKey(k string) (tea.Cmd, noteAction, bool) {
	if e.pending == "" {
		return nil, noteActNone, false
	}

	pending := e.pending
	n := e.repeatCount()

	if pending == "r" || pending == "f" || pending == "F" || pending == "t" || pending == "T" {
		if r, ok := pendingKeyRune(k); ok {
			e.clearPending()
			switch pending {
			case "r":
				e.pushUndo()
				e.replaceChar(r)
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
		e.clearPending()
		return nil, noteActNone, false
	}

	if pending == "g" {
		e.clearPending()
		if k == "g" {
			e.ta.MoveToBegin()
			return nil, noteActNone, true
		}
		return nil, noteActNone, false
	}

	e.clearPending()
	switch pending {
	case "d":
		if k == "d" {
			e.pushUndo()
			for i := 0; i < n; i++ {
				e.deleteWholeLines()
			}
			return nil, noteActNone, true
		}
		if !isMotionKey(k) {
			return nil, noteActNone, false
		}
		e.pushUndo()
		for i := 0; i < n; i++ {
			e.deleteByMotion(k)
		}
		return nil, noteActNone, true
	case "c":
		if k == "c" {
			e.pushUndo()
			for i := 0; i < n; i++ {
				e.deleteWholeLines()
			}
			e.beginInsert()
			e.insertSaved = true
			return nil, noteActNone, true
		}
		if !isMotionKey(k) {
			return nil, noteActNone, false
		}
		e.pushUndo()
		for i := 0; i < n; i++ {
			e.deleteByMotion(k)
		}
		e.beginInsert()
		e.insertSaved = true
		return nil, noteActNone, true
	case "y":
		if k == "y" {
			for i := 0; i < n; i++ {
				e.yankWholeLines()
			}
			return nil, noteActNone, true
		}
		if !isMotionKey(k) {
			return nil, noteActNone, false
		}
		for i := 0; i < n; i++ {
			e.yankByMotion(k)
		}
		return nil, noteActNone, true
	}

	return nil, noteActNone, false
}

func pendingKeyRune(k string) (rune, bool) {
	if k == "enter" || k == "tab" || k == "esc" || strings.Contains(k, "+") {
		return 0, false
	}
	r, size := utf8.DecodeRuneInString(k)
	if r == utf8.RuneError && size <= 1 {
		return 0, false
	}
	return r, true
}

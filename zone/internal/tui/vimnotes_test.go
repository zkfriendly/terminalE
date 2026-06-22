package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestVimNoteEditorModes(t *testing.T) {
	e := newVimNoteEditor(40, 6)
	if e.mode != noteModeInsert {
		t.Fatal("expected insert mode on open")
	}

	_, act := e.Update(tea.KeyPressMsg{Text: "esc"})
	if act != noteActNone || e.mode != noteModeNormal {
		t.Fatalf("esc should enter normal mode, got mode=%d act=%d", e.mode, act)
	}

	_, act = e.Update(tea.KeyPressMsg{Text: "i"})
	if e.mode != noteModeInsert {
		t.Fatal("i should enter insert mode")
	}

	_, act = e.Update(tea.KeyPressMsg{Text: "esc"})
	e.Update(tea.KeyPressMsg{Text: ":"})
	if e.mode != noteModeCommand {
		t.Fatal(": should enter command mode")
	}

	_, act = e.Update(tea.KeyPressMsg{Text: "w"})
	e.Update(tea.KeyPressMsg{Text: "q"})
	_, act = e.Update(tea.KeyPressMsg{Text: "enter"})
	if act != noteActSaveQuit {
		t.Fatalf(":wq should save and quit, got act=%d", act)
	}
}

func TestVimNoteEditorPaste(t *testing.T) {
	e := newVimNoteEditor(40, 6)
	paste := tea.PasteMsg{Content: "hello from outside\nline two"}
	_, act := e.Update(paste)
	if act != noteActNone {
		t.Fatalf("paste should not trigger action, got act=%d", act)
	}
	if e.Value() != "hello from outside\nline two" {
		t.Fatalf("paste content mismatch: %q", e.Value())
	}

	e.mode = noteModeNormal
	e.Update(paste)
	if e.Value() != "hello from outside\nline two" {
		t.Fatal("paste should be ignored in normal mode")
	}
}

func TestVimNoteEditorUndo(t *testing.T) {
	e := newVimNoteEditor(40, 6)
	e.ta.SetValue("hello world")
	e.mode = noteModeNormal

	e.pushUndo()
	e.ta.SetValue("hello world!")
	if e.Value() != "hello world!" {
		t.Fatal("setup failed")
	}

	e.undo()
	if e.Value() != "hello world" {
		t.Fatalf("undo should restore previous text, got %q", e.Value())
	}

	e.redo()
	if e.Value() != "hello world!" {
		t.Fatalf("redo should restore change, got %q", e.Value())
	}
}

func TestVimNoteEditorUndoInsertSession(t *testing.T) {
	e := newVimNoteEditor(40, 6)
	e.ta.SetValue("start")
	e.mode = noteModeInsert

	e.Update(tea.KeyPressMsg{Text: "!"})
	e.Update(tea.KeyPressMsg{Text: "!"})
	e.Update(tea.KeyPressMsg{Text: "esc"})
	if e.Value() != "start!!" {
		t.Fatalf("expected inserted text, got %q", e.Value())
	}

	e.undo()
	if e.Value() != "start" {
		t.Fatalf("u should undo whole insert session, got %q", e.Value())
	}
}

func TestVimNoteEditorDeleteLine(t *testing.T) {
	e := newVimNoteEditor(40, 6)
	e.ta.SetValue("one\ntwo\nthree")
	e.mode = noteModeNormal
	e.restoreCursor(1, 0)

	e.Update(tea.KeyPressMsg{Text: "d"})
	e.Update(tea.KeyPressMsg{Text: "d"})
	if e.Value() != "one\nthree" {
		t.Fatalf("dd should delete current line, got %q", e.Value())
	}

	e.undo()
	if e.Value() != "one\ntwo\nthree" {
		t.Fatalf("undo should restore deleted line, got %q", e.Value())
	}
}

func TestVimNoteEditorYankPaste(t *testing.T) {
	e := newVimNoteEditor(40, 6)
	e.ta.SetValue("alpha\nbeta")
	e.mode = noteModeNormal
	e.restoreCursor(0, 0)

	e.Update(tea.KeyPressMsg{Text: "y"})
	e.Update(tea.KeyPressMsg{Text: "y"})
	e.Update(tea.KeyPressMsg{Text: "p"})
	if e.Value() != "alpha\nalpha\nbeta" {
		t.Fatalf("yy then p should duplicate line below, got %q", e.Value())
	}
}

func TestVimNoteEditorNoLineLimit(t *testing.T) {
	e := newVimNoteEditor(40, 10)
	var b strings.Builder
	for i := range 100 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	e.Load(strings.TrimSuffix(b.String(), "\n"))
	if e.ta.LineCount() < 100 {
		t.Fatalf("expected at least 100 lines, got %d", e.ta.LineCount())
	}
	e.mode = noteModeInsert
	e.Update(tea.KeyPressMsg{Text: "!"})
	if strings.Count(e.Value(), "\n") < 99 {
		t.Fatalf("should edit past default 99-line cap, got %d lines", strings.Count(e.Value(), "\n")+1)
	}
}

func TestVimNoteEditorScrollHint(t *testing.T) {
	e := newVimNoteEditor(40, 4)
	var b strings.Builder
	for i := range 20 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	e.Load(strings.TrimSuffix(b.String(), "\n"))
	e.ta.MoveToBegin()
	hint := e.editorInfoHint(newStyles())
	if hint == "" {
		t.Fatal("expected scroll hint for long note in short viewport")
	}
	if !strings.Contains(hint, "below") && !strings.Contains(hint, "lines") {
		t.Fatalf("expected overflow hint, got %q", hint)
	}
}

func TestVimNoteEditorVisualYank(t *testing.T) {
	e := newVimNoteEditor(40, 6)
	e.ta.SetValue("hello world")
	e.mode = noteModeNormal
	e.restoreCursor(0, 0)
	e.enterVisual(visualChar)
	for range 4 {
		e.execMotion("l")
	}
	if e.mode != noteModeVisual {
		t.Fatal("expected visual mode")
	}
	e.handleVisualKey("y")
	if e.yankBuf != "hello" {
		t.Fatalf("visual yank expected hello, got %q", e.yankBuf)
	}
	if e.mode != noteModeNormal {
		t.Fatal("y should return to normal mode")
	}
}

func TestVimNoteEditorVisualDelete(t *testing.T) {
	e := newVimNoteEditor(40, 6)
	e.ta.SetValue("hello world")
	e.mode = noteModeNormal
	e.restoreCursor(0, 0)
	e.enterVisual(visualChar)
	for range 4 {
		e.execMotion("l")
	}
	e.handleVisualKey("d")
	if e.Value() != " world" {
		t.Fatalf("visual delete expected ' world', got %q", e.Value())
	}
}

func TestVimNoteEditorVisualLine(t *testing.T) {
	e := newVimNoteEditor(40, 6)
	e.ta.SetValue("one\ntwo\nthree")
	e.mode = noteModeNormal
	e.restoreCursor(1, 0)
	e.enterVisual(visualLine)
	e.handleVisualKey("y")
	if e.yankBuf != "two\n" {
		t.Fatalf("line visual yank expected two\\\\n, got %q", e.yankBuf)
	}
}

func TestYankPersistsAcrossNotes(t *testing.T) {
	a := newVimNoteEditor(40, 6)
	a.setYank("shared clip")
	a.Load("target note")

	b := newVimNoteEditor(40, 6)
	b.ta.SetValue("prefix ")
	b.mode = noteModeNormal
	b.restoreCursor(0, 7)
	b.handleNormalKey("p")
	if b.Value() != "prefix shared clip" {
		t.Fatalf("second editor should paste shared yank, got %q", b.Value())
	}

	a.mode = noteModeNormal
	a.restoreCursor(0, 0)
	a.handleNormalKey("p")
	if a.Value() != "shared cliptarget note" {
		t.Fatalf("load should keep yank for paste, got %q", a.Value())
	}
}

func TestInsertTab(t *testing.T) {
	e := newVimNoteEditor(40, 6)
	e.mode = noteModeInsert
	_, act := e.Update(tea.KeyPressMsg{Text: "tab"})
	if act != noteActNone {
		t.Fatalf("unexpected action %d", act)
	}
	if e.Value() != "    " {
		t.Fatalf("tab key should insert indent, got %q", e.Value())
	}
}

func TestVimNoteEditorRedoCommand(t *testing.T) {
	e := newVimNoteEditor(40, 6)
	e.ta.SetValue("hello world")
	e.mode = noteModeNormal

	e.pushUndo()
	e.ta.SetValue("hello world!")
	e.undo()
	if e.Value() != "hello world" {
		t.Fatalf("setup undo failed, got %q", e.Value())
	}

	e.Update(tea.KeyPressMsg{Text: ":"})
	e.Update(tea.KeyPressMsg{Text: "r"})
	e.Update(tea.KeyPressMsg{Text: "e"})
	e.Update(tea.KeyPressMsg{Text: "d"})
	e.Update(tea.KeyPressMsg{Text: "o"})
	_, act := e.Update(tea.KeyPressMsg{Text: "enter"})
	if act != noteActNone {
		t.Fatalf(":redo should not trigger save action, got %d", act)
	}
	if e.Value() != "hello world!" {
		t.Fatalf(":redo should restore change, got %q", e.Value())
	}
}

func TestVimNoteEditorZZ(t *testing.T) {
	e := newVimNoteEditor(40, 6)
	e.mode = noteModeNormal
	_, act := e.Update(tea.KeyPressMsg{Text: "Z"})
	if act != noteActNone {
		t.Fatal("first Z should wait")
	}
	_, act = e.Update(tea.KeyPressMsg{Text: "Z"})
	if act != noteActSaveQuit {
		t.Fatalf("ZZ should save and quit, got act=%d", act)
	}
}

func TestDeleteLineCursorStays(t *testing.T) {
	e := newVimNoteEditor(40, 6)
	e.ta.SetValue("one\ntwo\nthree")
	e.mode = noteModeNormal
	e.restoreCursor(1, 1)
	e.Update(tea.KeyPressMsg{Text: "d"})
	e.Update(tea.KeyPressMsg{Text: "d"})
	if e.Value() != "one\nthree" {
		t.Fatalf("unexpected value %q", e.Value())
	}
	if e.ta.Line() != 1 {
		t.Fatalf("dd on middle line should keep cursor on line 1, got line %d", e.ta.Line())
	}
	if e.ta.Column() != 1 {
		t.Fatalf("dd should preserve column when possible, got col %d", e.ta.Column())
	}

	e.Load("one\ntwo\nthree")
	e.mode = noteModeNormal
	e.restoreCursor(0, 0)
	e.Update(tea.KeyPressMsg{Text: "d"})
	e.Update(tea.KeyPressMsg{Text: "d"})
	if e.ta.Line() != 0 {
		t.Fatalf("dd on first line should stay on line 0, got line %d", e.ta.Line())
	}

	e.Load("one\ntwo\nthree")
	e.mode = noteModeNormal
	e.restoreCursor(2, 0)
	e.Update(tea.KeyPressMsg{Text: "d"})
	e.Update(tea.KeyPressMsg{Text: "d"})
	if e.ta.Line() != 1 {
		t.Fatalf("dd on last line should move to previous line 1, got line %d", e.ta.Line())
	}
}

func TestDeleteLineCursorNarrowWidth(t *testing.T) {
	e := newVimNoteEditor(10, 8)
	e.ta.SetValue("short\nthis is a much longer line that wraps\nlast")
	e.mode = noteModeNormal
	e.restoreCursor(1, 5)
	e.Update(tea.KeyPressMsg{Text: "d"})
	e.Update(tea.KeyPressMsg{Text: "d"})
	if e.Value() != "short\nlast" {
		t.Fatalf("unexpected value %q", e.Value())
	}
	if e.ta.Line() != 1 {
		t.Fatalf("dd on middle line should keep cursor on line 1, got line %d", e.ta.Line())
	}
	if e.ta.Column() != 4 {
		t.Fatalf("dd should clamp column to new line length, got col %d", e.ta.Column())
	}
}

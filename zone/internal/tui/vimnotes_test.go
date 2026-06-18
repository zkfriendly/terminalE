package tui

import (
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

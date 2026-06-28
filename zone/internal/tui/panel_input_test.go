package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPanelInputCapturesTyping(t *testing.T) {
	p := newPanelInput("> ", "type here", 100)
	p.markFocused()

	_, act := p.handleMsg(keyPress("j"))
	if act != panelInputNone {
		t.Fatalf("expected j to stay in input, got action %d", act)
	}
	_, act = p.handleMsg(keyPress("k"))
	if act != panelInputNone {
		t.Fatalf("expected k to stay in input, got action %d", act)
	}
	if !p.IsFocused() {
		t.Fatal("expected input to stay focused after j/k")
	}
}

func TestPanelInputEscBlurs(t *testing.T) {
	p := newPanelInput("> ", "", 100)
	p.markFocused()
	p.SetValue("hello")

	_, act := p.handleMsg(keyPress("esc"))
	if act != panelInputCancel {
		t.Fatalf("expected cancel on esc, got %d", act)
	}
	if p.IsFocused() {
		t.Fatal("expected input blurred after esc")
	}
	if p.Value() != "hello" {
		t.Fatalf("expected value preserved on esc blur, got %q", p.Value())
	}
}

func TestPanelInputSubmit(t *testing.T) {
	p := newPanelInput("> ", "", 100)
	p.markFocused()
	p.handleMsg(tea.KeyPressMsg{Text: "hello"})
	_, act := p.handleMsg(keyPress("enter"))
	if act != panelInputSubmit {
		t.Fatalf("expected submit, got %d", act)
	}
}

func TestNotesSearchEscBlursInput(t *testing.T) {
	var s notesSearchState
	s.open()
	s.handleMsg(keyPress("esc"), nil, nil, nil)
	if !s.active {
		t.Fatal("expected search to stay open after esc from query input")
	}
	if s.focus != searchFocusResults {
		t.Fatalf("expected results mode after esc, got %d", s.focus)
	}
	if s.inputFocused() {
		t.Fatal("expected no input focused after esc")
	}

	s.focus = searchFocusChat
	s.chatInput.markFocused()
	s.handleMsg(keyPress("j"), nil, nil, nil)
	s.handleMsg(keyPress("esc"), nil, nil, nil)
	if !s.active {
		t.Fatal("expected search to stay open after esc from chat input")
	}
	if s.focus != searchFocusResults {
		t.Fatalf("expected results mode after chat esc, got %d", s.focus)
	}

	s.handleMsg(keyPress("esc"), nil, nil, nil)
	if s.active {
		t.Fatal("expected search closed after esc from results mode")
	}
}

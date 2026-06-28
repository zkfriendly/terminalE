package tui

import "testing"

func TestNotesNavStack(t *testing.T) {
	nav := newNotesNav(notesScreenList)
	nav.push(notesScreenSearch)
	nav.push(notesScreenEdit)
	if nav.peek() != notesScreenEdit {
		t.Fatalf("expected edit on top, got %d", nav.peek())
	}
	if top, ok := nav.pop(); !ok || top != notesScreenSearch {
		t.Fatalf("expected pop to search, got %d ok=%v", top, ok)
	}
	if top, ok := nav.pop(); !ok || top != notesScreenList {
		t.Fatalf("expected pop to list, got %d ok=%v", top, ok)
	}
	if _, ok := nav.pop(); ok {
		t.Fatal("expected cannot pop last frame")
	}
}

package store

import "testing"

func TestSearchNotes(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("Work", "")
	task, _ := s.CreateTask(p.ID, "Prototype")
	sess, _ := s.CreateSession(&task.ID, 3000, 600, 14400, 180)
	s.AddSessionNote(sess.ID, "whir needs a better buffer API")
	s.AddSessionNote(sess.ID, "unrelated grocery list")

	p2, _ := s.CreateProject("Other", "")
	task2, _ := s.CreateTask(p2.ID, "Misc")
	sess2, _ := s.CreateSession(&task2.ID, 3000, 600, 14400, 180)
	s.AddSessionNote(sess2.ID, "something about WHIR deployment")

	hits, err := s.SearchNotes("what about whir do i know", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d: %+v", len(hits), hits)
	}
	if !containsBody(hits, "whir needs a better buffer API") {
		t.Fatalf("missing first note: %+v", hits)
	}
}

func TestSearchNotesEmptyQuery(t *testing.T) {
	s := newTestStore(t)
	hits, err := s.SearchNotes("   ", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if hits != nil {
		t.Fatalf("expected nil for empty query, got %+v", hits)
	}
}

func containsBody(notes []GlobalNote, want string) bool {
	for _, n := range notes {
		if n.Body == want {
			return true
		}
	}
	return false
}

func TestExtractSearchTerms(t *testing.T) {
	terms := extractSearchTerms("what about whir do i know")
	if len(terms) != 1 || terms[0] != "whir" {
		t.Fatalf("got %v", terms)
	}
}

func TestRRFMergeNotes(t *testing.T) {
	a := GlobalNote{SessionNote: SessionNote{ID: 1, Body: "a"}}
	b := GlobalNote{SessionNote: SessionNote{ID: 2, Body: "b"}}
	c := GlobalNote{SessionNote: SessionNote{ID: 3, Body: "c"}}
	merged := RRFMergeNotes([][]GlobalNote{
		{a, b},
		{b, c},
	}, 10)
	if len(merged) != 3 || merged[0].ID != 2 {
		t.Fatalf("expected note 2 on top, got %+v", merged)
	}
}

func TestOrderNotesByIDs(t *testing.T) {
	notes := []GlobalNote{
		{SessionNote: SessionNote{ID: 1}},
		{SessionNote: SessionNote{ID: 2}},
		{SessionNote: SessionNote{ID: 3}},
	}
	out := OrderNotesByIDs(notes, []int64{3, 1}, 10)
	if len(out) != 3 || out[0].ID != 3 || out[1].ID != 1 || out[2].ID != 2 {
		t.Fatalf("unexpected order: %+v", out)
	}
}

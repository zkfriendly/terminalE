package store

import "testing"

func TestSessionNotes(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("Notes", "")
	task, _ := s.CreateTask(p.ID, "Write")
	sess, err := s.CreateSession(&task.ID, 3000, 600, 14400, 180)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := s.AddSessionNote(sess.ID, "first thought"); err != nil {
		t.Fatalf("add note: %v", err)
	}
	if _, err := s.AddSessionNote(sess.ID, "second line\nwith a break"); err != nil {
		t.Fatalf("add note: %v", err)
	}

	notes, err := s.ListSessionNotes(sess.ID)
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(notes))
	}
	if notes[0].Body != "second line\nwith a break" {
		t.Fatalf("expected newest first, got %q", notes[0].Body)
	}
	if notes[1].Body != "first thought" {
		t.Fatalf("unexpected older note: %q", notes[1].Body)
	}

	summaries, _ := s.RecentSessions(5)
	if len(summaries) != 1 || summaries[0].NoteCount != 2 {
		t.Fatalf("expected note count 2 in summary, got %+v", summaries)
	}

	meta, err := s.SetSessionNoteMeta(notes[1].ID, "Revised", "✏️")
	if err != nil {
		t.Fatalf("set meta: %v", err)
	}
	if meta.Title != "Revised" || meta.Emoji != "✏️" {
		t.Fatalf("unexpected meta: %+v", meta)
	}

	updated, err := s.UpdateSessionNote(notes[1].ID, "revised thought")
	if err != nil {
		t.Fatalf("update note: %v", err)
	}
	if updated.Title != "Revised" || updated.Emoji != "✏️" {
		t.Fatal("update should preserve generated meta")
	}

	notes, _ = s.ListSessionNotes(sess.ID)
	if notes[1].Body != "revised thought" || notes[1].Title != "Revised" {
		t.Fatalf("update not persisted: %+v", notes)
	}
}

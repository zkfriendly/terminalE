package store

import (
	"testing"

	"github.com/zkfriendly/zone/internal/llm"
)

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

	if err := s.DeleteSessionNote(notes[0].ID); err != nil {
		t.Fatalf("delete note: %v", err)
	}
	notes, _ = s.ListSessionNotes(sess.ID)
	if len(notes) != 1 {
		t.Fatalf("expected 1 note after delete, got %d", len(notes))
	}
	if notes[0].Body != "revised thought" {
		t.Fatalf("wrong note remained: %+v", notes[0])
	}
	summaries, _ = s.RecentSessions(5)
	if len(summaries) != 1 || summaries[0].NoteCount != 1 {
		t.Fatalf("expected note count 1 in summary, got %+v", summaries)
	}
}

func TestListAllNotes(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("Demo", "")
	task, _ := s.CreateTask(p.ID, "Write")
	sess1, _ := s.CreateSession(&task.ID, 3000, 600, 14400, 180)
	s.AddSessionNote(sess1.ID, "from session one")

	sess2, _ := s.CreateSession(nil, 3000, 600, 14400, 180)
	s.AddSessionNote(sess2.ID, "from session two")

	all, err := s.ListAllNotes(100)
	if err != nil {
		t.Fatalf("list all notes: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(all))
	}
	if all[0].Body != "from session two" {
		t.Fatalf("expected newest first, got %q", all[0].Body)
	}
	if all[0].TaskTitle != "" || all[1].TaskTitle != "Write" {
		t.Fatalf("expected task context on notes: %+v", all)
	}
}

func TestSessionNoteActionableScan(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("Notes", "")
	task, _ := s.CreateTask(p.ID, "Write")
	sess, _ := s.CreateSession(&task.ID, 3000, 600, 14400, 180)

	n, err := s.AddSessionNote(sess.ID, "todo: ship the feature")
	if err != nil {
		t.Fatalf("add note: %v", err)
	}
	if !n.NeedsActionableScan() {
		t.Fatal("new note should need actionable scan")
	}

	scanned, err := s.SetSessionNoteActionableScan(n.ID, true)
	if err != nil {
		t.Fatalf("set scan: %v", err)
	}
	if !scanned.HasActionables || scanned.ActionablesScannedAt == nil {
		t.Fatalf("expected actionable scan result, got %+v", scanned)
	}
	if scanned.ActionablesScanVersion != llm.ActionablesScanVersion {
		t.Fatalf("expected scan version %d, got %d", llm.ActionablesScanVersion, scanned.ActionablesScanVersion)
	}
	if scanned.NeedsActionableScan() {
		t.Fatal("fresh scan should not need rescan")
	}

	updated, err := s.UpdateSessionNote(n.ID, "todo: ship the feature and write tests")
	if err != nil {
		t.Fatalf("update note: %v", err)
	}
	if updated.HasActionables {
		t.Fatal("edit should clear actionable flag")
	}
	if !updated.NeedsActionableScan() {
		t.Fatal("edit should invalidate actionable scan")
	}

	rescan, err := s.SetSessionNoteActionableScan(n.ID, false)
	if err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if rescan.HasActionables {
		t.Fatal("expected no actionables")
	}
}

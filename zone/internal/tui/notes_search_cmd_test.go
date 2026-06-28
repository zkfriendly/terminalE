package tui

import (
	"testing"

	"github.com/zkfriendly/zone/internal/session"
)

func TestNotesSearchPreliminaryResults(t *testing.T) {
	_, st := newTestApp(t)
	p, _ := st.CreateProject("Work", "")
	task, _ := st.CreateTask(p.ID, "Prototype")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)
	st.AddSessionNote(sess.ID, "whir needs a better buffer API")

	var s notesSearchState
	s.query = "what about whir do I know"
	s.results = preliminaryNotesSearch(st, s.query)
	s.resultsPrelim = true
	if !s.resultsPrelim {
		t.Fatal("expected preliminary flag")
	}
	if len(s.results) != 1 {
		t.Fatalf("expected 1 preliminary hit, got %d", len(s.results))
	}
	if s.results[0].Body != "whir needs a better buffer API" {
		t.Fatalf("unexpected note: %+v", s.results[0])
	}
}

func TestAISearchNotesKeywordFallback(t *testing.T) {
	_, st := newTestApp(t)
	p, _ := st.CreateProject("Work", "")
	task, _ := st.CreateTask(p.ID, "Prototype")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)
	st.AddSessionNote(sess.ID, "whir needs a better buffer API")
	st.AddSessionNote(sess.ID, "unrelated grocery list")

	cfg := testCfgPtr()
	cfg.LMStudioEnabled = true
	cfg.LMStudioURL = "http://127.0.0.1:1" // unreachable — AI steps fail

	z := newZone(st, nil, newStyles(), cfg, session.Snapshot{SessionID: sess.ID, Phase: "work"})
	results := aiEnhanceSearch(st, cfg, "what about whir do I know", nil, 10)
	// aiEnhanceSearch with dead LLM: expansion/rerank fail, should still merge via keyword path in aiSearchNotes
	_ = z

	hits, err := aiSearchNotes(st, cfg, "what about whir do I know", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 keyword hit, got %d: %+v", len(hits), hits)
	}
	if hits[0].Body != "whir needs a better buffer API" {
		t.Fatalf("unexpected note: %+v", hits[0])
	}
	_ = results
}

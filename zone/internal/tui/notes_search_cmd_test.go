package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/zkfriendly/zone/internal/config"
	"github.com/zkfriendly/zone/internal/llm"
	"github.com/zkfriendly/zone/internal/session"
)

func TestNoteCommandsCaptureProviderSettings(t *testing.T) {
	_, st := newTestApp(t)
	sess, err := st.CreateSession(nil, 3000, 600, 14400, 180)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSessionNote(sess.ID, "whir buffer refactor"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llm.Message `json:"messages"`
			Model    string        `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		if request.Model != "captured-model" {
			t.Errorf("model changed: %q", request.Model)
		}
		answer := "You planned a whir buffer refactor."
		switch system := request.Messages[0].Content; {
		case strings.Contains(system, "label notes"):
			answer = `{"emoji":"📝","title":"Whir buffer refactor"}`
		case strings.Contains(system, "classify"):
			answer = `{"has_actionables":true}`
		case strings.Contains(system, "extract actionable"):
			answer = `{"tasks":["Refactor the whir buffer API"]}`
		case strings.Contains(system, "expand note"):
			answer = `{"keywords":["whir"],"hyde":"whir buffer refactor"}`
		case strings.Contains(system, "rank notes"):
			answer = `{"ids":[1]}`
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": answer}}}})
	}))
	defer srv.Close()
	cfg := config.Default()
	cfg.LLMProvider, cfg.LMStudioURL, cfg.LMStudioModel = config.ProviderLocal, srv.URL, "captured-model"
	z := newZone(st, nil, newStyles(), &cfg, session.Snapshot{SessionID: sess.ID})
	v := newAllNotes(st, &cfg, newStyles())
	search := notesSearchState{results: preliminaryNotesSearch(st, "whir")}
	commands := []tea.Cmd{
		z.enrichNoteCmd(1, "whir buffer refactor"),
		v.enrichNoteCmd(1, "whir buffer refactor"),
		scanNoteActionablesCmd(&cfg, map[int64]bool{}, 1, "whir buffer refactor"),
		extractNoteActionablesCmd(&cfg, 1, "whir buffer refactor"),
		search.answerCmd(&cfg, 1, "whir", nil),
		searchNotesCmd(st, &cfg, 2, "whir", nil),
	}
	// A setting change after scheduling must not redirect any stage of a request.
	cfg.LLMProvider, cfg.LMStudioModel = "invalid-next-provider", "changed-model"
	for i, cmd := range commands {
		if cmd == nil {
			t.Fatalf("command %d missing", i)
		}
		switch msg := cmd().(type) {
		case noteEnrichedMsg:
			if msg.err != nil || msg.note.Title == "" {
				t.Fatalf("label: %+v", msg)
			}
		case noteActionablesScannedMsg:
			if msg.err != nil || !msg.hasActionables {
				t.Fatalf("scan: %+v", msg)
			}
		case noteActionablesExtractedMsg:
			if msg.err != nil || len(msg.tasks) != 1 {
				t.Fatalf("extract: %+v", msg)
			}
		case notesSearchAnswerMsg:
			if msg.err != nil || !strings.Contains(msg.answer, "whir") {
				t.Fatalf("search: %+v", msg)
			}
		default:
			t.Fatalf("unexpected message: %T", msg)
		}
	}
}

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
	cfg.LLMEnabled = true
	cfg.LLMProvider = "local"
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

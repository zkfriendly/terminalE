package llm

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnrichNote(t *testing.T) {
	ResetModelCache()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"emoji\":\"🧠\",\"title\":\"Deep refactor plan\"}"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	meta, err := EnrichNote(srv.URL, "", "need to refactor the daemon ipc layer")
	if err != nil {
		t.Fatalf("enrich: %v", err)
	}
	if meta.Emoji != "🧠" || meta.Title != "Deep refactor plan" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}

func TestEnrichNoteReasoningModel(t *testing.T) {
	ResetModelCache()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen-test"}]}`))
			return
		}
		// Reasoning models often leave content empty and put JSON in reasoning_content.
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","reasoning_content":"Final answer: {\"emoji\":\"☕\",\"title\":\"Coffee break idea\"}"}}]}`))
	}))
	defer srv.Close()

	meta, err := EnrichNote(srv.URL, "", "grab coffee")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Emoji != "☕" || meta.Title != "Coffee break idea" {
		t.Fatalf("unexpected: %+v", meta)
	}
}

func TestParseNoteMetaMarkdown(t *testing.T) {
	meta, err := parseNoteMeta("```json\n{\"emoji\":\"☕\",\"title\":\"Coffee break idea\"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Emoji != "☕" || meta.Title != "Coffee break idea" {
		t.Fatalf("unexpected: %+v", meta)
	}
}

func TestParseNoteMetaIgnoresPromptTemplate(t *testing.T) {
	// Reasoning models may echo the prompt example before the real answer.
	text := `Use format {"emoji":"<one emoji>","title":"<3-6 word title>"}
Final: {"emoji":"🔧","title":"Refactor Daemon IPC Layer"}`
	meta, err := parseNoteMeta(text)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Emoji != "🔧" || meta.Title != "Refactor Daemon IPC Layer" {
		t.Fatalf("unexpected: %+v", meta)
	}
}

func TestParseNoteMetaTitleFirst(t *testing.T) {
	meta, err := parseNoteMeta(`{"title":"Coffee break","emoji":"☕"}`)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Emoji != "☕" || meta.Title != "Coffee break" {
		t.Fatalf("unexpected: %+v", meta)
	}
}

func TestResolveModelSkipsEmbedding(t *testing.T) {
	ResetModelCache()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"text-embedding-nomic"},{"id":"qwen/qwen3"}]}`))
	}))
	defer srv.Close()

	id, err := resolveModel(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if id != "qwen/qwen3" {
		t.Fatalf("got %q", id)
	}
}

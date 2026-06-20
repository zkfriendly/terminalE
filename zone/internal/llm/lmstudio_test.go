package llm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnrichNote(t *testing.T) {
	ResetModelCache()
	ResetStats()
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
	snap := Snapshot()
	if snap.OK != 1 || snap.Err != 0 || snap.InFlight != 0 {
		t.Fatalf("stats: %+v", snap)
	}
	if snap.LastMS <= 0 {
		t.Fatalf("expected last duration, got %dms", snap.LastMS)
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

func TestParseActionablesReasoningEcho(t *testing.T) {
	// Reasoning models echo the prompt template and can hit token limits before
	// emitting a final content field. The parser must not pick the template's
	// trailing false example over the model's true conclusion.
	text := `3.  **Determine Output:**
   - {"has_actionables": true}
   Check constraints: "Reply with ONLY one JSON object: {"has_actionables": true} or {"has_actionables": false}"`
	has, err := parseActionablesResult(text)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected true from reasoning conclusion, not template echo")
	}
}

func TestParseActionablesBacktickTemplateEcho(t *testing.T) {
	// Qwen3 reasoning often echoes the allowed answers with backticks, which
	// must not be treated as the model's false conclusion when truncated.
	text := `1.  **Analyze User Input:**
   - Output format: ONLY raw JSON ` + "`{\"has_actionables\": true}` or `{\"has_actionables\": false}`.\n\n2.  **Evaluate against Criteria:**\n   - Matches TRUE criteria.\n\n"
	has, err := parseActionablesResult(text)
	if err == nil && !has {
		t.Fatal("expected error or true, not false from backtick template echo")
	}
}

func TestParseActionablesLiveReasoningTruncated(t *testing.T) {
	// Captured from qwen3.6-35b-a3b on the LLM Task Extraction note (finish_reason=length).
	text := `3.  **Determine Output:**
   - {"has_actionables": true}

4.  **Format Output:**
   - Must be ONLY raw JSON. No extra text.
   - {"has_actionables": true} matches requirement.

   Check constraints: "Reply with ONLY one JSON object: {"has_actionables": true} or {"has_actionables": false}"`
	has, err := parseActionablesResult(text)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected true for LLM task extraction note reasoning")
	}
}

func TestParseActionablesPromptIncludesExamples(t *testing.T) {
	prompt := actionablesPrompt("sample note")
	for _, want := range []string{
		"I want to add a feature",
		"we need to fix that so users know to scroll",
		"what hardware would I need",
		"This is fun",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing example %q", want)
		}
	}
}

func TestDetectActionables(t *testing.T) {
	ResetModelCache()
	ResetStats()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"has_actionables\": true}"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	has, err := DetectActionables(srv.URL, "", "todo: refactor the buffer abstraction")
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if !has {
		t.Fatal("expected actionables")
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

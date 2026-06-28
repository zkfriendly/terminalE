package llm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFormatNoteSnippets(t *testing.T) {
	out := FormatNoteSnippets([]NoteSnippet{
		{
			ID: 1, Title: "Whir API", Emoji: "⚙️", Body: "buffer refactor",
			TaskTitle: "Prototype", ProjectName: "Whir",
			CreatedAt: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
		},
	})
	if !strings.Contains(out, "[1]") || !strings.Contains(out, "Whir API") || !strings.Contains(out, "buffer refactor") {
		t.Fatalf("unexpected format:\n%s", out)
	}
}

func TestAnswerNotesQuestion(t *testing.T) {
	ResetModelCache()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"You wrote about whir buffers in note [1]."}}]}`))
		}
	}))
	defer srv.Close()

	ctx := FormatNoteSnippets([]NoteSnippet{{ID: 1, Body: "whir buffer API"}})
	answer, err := AnswerNotesQuestion(srv.URL, "", "what about whir do I know", ctx, nil)
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if !strings.Contains(strings.ToLower(answer), "whir") {
		t.Fatalf("expected whir in answer, got %q", answer)
	}
}

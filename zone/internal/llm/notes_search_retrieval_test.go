package llm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExpandNotesSearchQuery(t *testing.T) {
	ResetModelCache()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"keywords\":[\"whir\",\"zk proof\",\"buffer\"],\"hyde\":\"Notes on the WHIR zero-knowledge protocol buffer API.\"}"}}]}`))
		}
	}))
	defer srv.Close()

	exp, err := localTestClient(srv.URL).ExpandNotesSearchQuery("what do I know about ZK proofs")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if len(exp.Keywords) < 2 {
		t.Fatalf("expected keywords, got %+v", exp.Keywords)
	}
	if exp.Hyde == "" {
		t.Fatal("expected hyde text")
	}
}

func TestRerankNotesForQuery(t *testing.T) {
	ResetModelCache()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ids\":[17,3]}"}}]}`))
		}
	}))
	defer srv.Close()

	catalog := []NoteSnippet{
		{ID: 3, Body: "deployment notes"},
		{ID: 17, Body: "whir protocol buffer"},
	}
	ids, err := localTestClient(srv.URL).RerankNotesForQuery("whir protocol", catalog, 10)
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}
	if len(ids) != 2 || ids[0] != 17 {
		t.Fatalf("expected [17 3], got %v", ids)
	}
}

func TestFormatNoteCatalog(t *testing.T) {
	out := FormatNoteCatalog([]NoteSnippet{
		{ID: 5, Title: "Whir", Emoji: "⚙️", Body: "buffer API", CreatedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
	})
	if !strings.Contains(out, "id=5") || !strings.Contains(out, "buffer API") || !strings.Contains(out, "[1]") {
		t.Fatalf("unexpected catalog:\n%s", out)
	}
}

func TestMapRerankIDs(t *testing.T) {
	catalog := []NoteSnippet{
		{ID: 42, Body: "whir note"},
		{ID: 99, Body: "other"},
	}
	// model returned catalog index instead of db id
	got := mapRerankIDs([]int64{1}, catalog)
	if len(got) != 1 || got[0] != 42 {
		t.Fatalf("expected [42], got %v", got)
	}
	// model returned real id
	got = mapRerankIDs([]int64{99}, catalog)
	if len(got) != 1 || got[0] != 99 {
		t.Fatalf("expected [99], got %v", got)
	}
}

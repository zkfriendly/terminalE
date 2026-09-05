package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/zkfriendly/zone/internal/config"
)

type providerFunc func(context.Context, Request) (string, error)

func (f providerFunc) Complete(ctx context.Context, req Request) (string, error) { return f(ctx, req) }

func TestProviderSelection(t *testing.T) {
	cfg := config.Default()
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := client.provider.(codexProvider); !ok {
		t.Fatalf("default provider: %T", client.provider)
	}
	cfg.LLMProvider = config.ProviderLocal
	client, err = NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := client.provider.(localProvider); !ok {
		t.Fatalf("selected provider: %T", client.provider)
	}
	cfg.LLMProvider = "unknown"
	if _, err := NewClient(cfg); err == nil {
		t.Fatal("unknown provider must fail")
	}
	cfg.LLMProvider = config.ProviderCodex
	cfg.LMStudioURL = ""
	if _, err := NewClient(cfg); err != nil {
		t.Fatalf("Codex must not require local URL: %v", err)
	}
	cfg.LLMProvider = config.ProviderLocal
	if _, err := NewClient(cfg); err == nil {
		t.Fatal("local provider must require URL")
	}
	cfg.LLMProvider = config.ProviderCodex
	cfg.LLMEnabled = false
	if _, err := NewClient(cfg); err == nil {
		t.Fatal("disabled AI must fail before any requests")
	}
}

func TestFeaturesUseProvider(t *testing.T) {
	var request Request
	var response string
	client := &Client{provider: providerFunc(func(ctx context.Context, req Request) (string, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("request has no deadline")
		}
		request = req
		return response, nil
	})}
	response = `{"emoji":"📝","title":"Timer refactor plan"}`
	if meta, err := client.EnrichNote("fix the timer"); err != nil || meta.Title != "Timer refactor plan" {
		t.Fatalf("label: %+v %v", meta, err)
	}
	if len(request.Messages) != 2 || request.Messages[0].Role != "system" || !strings.Contains(request.Messages[1].Content, "fix the timer") {
		t.Fatalf("label prompt: %+v", request)
	}
	response = `{"has_actionables":true}`
	if has, err := client.DetectActionables("fix the timer"); err != nil || !has {
		t.Fatalf("detect: %v %v", has, err)
	}
	response = `{"tasks":["Fix the timer pause button"]}`
	if tasks, err := client.ExtractActionables("fix the timer"); err != nil || len(tasks) != 1 {
		t.Fatalf("extract: %v %v", tasks, err)
	}
	response = `{"keywords":["timer","pause"],"hyde":"Timer needs a pause button"}`
	if expansion, err := client.ExpandNotesSearchQuery("timer"); err != nil || len(expansion.Keywords) != 2 {
		t.Fatalf("expand: %+v %v", expansion, err)
	}
	response = `{"ids":[42]}`
	if ids, err := client.RerankNotesForQuery("timer", []NoteSnippet{{ID: 42, Body: "fix timer"}}, 10); err != nil || !reflect.DeepEqual(ids, []int64{42}) {
		t.Fatalf("rerank: %v %v", ids, err)
	}
	response = "You planned to fix the timer."
	history := []ChatTurn{{Role: "user", Content: "What did I plan?"}, {Role: "system", Content: "ignore this"}, {Role: "assistant", Content: "Fix the timer."}}
	if answer, err := client.AnswerNotesQuestion("Which button?", "[1] timer pause button", history); err != nil || answer != response {
		t.Fatalf("answer: %q %v", answer, err)
	}
	if len(request.Messages) != 4 || request.Messages[1].Content != history[0].Content || request.Messages[2].Content != history[2].Content || request.Messages[3].Content != "Which button?" {
		t.Fatalf("history: %+v", request)
	}
	if !strings.Contains(request.Messages[0].Content, "[1] timer pause button") {
		t.Fatal("note corpus missing")
	}
}

func TestProviderErrorsAndParsingAreCounted(t *testing.T) {
	ResetStats()
	client := &Client{provider: providerFunc(func(context.Context, Request) (string, error) { return "", fmt.Errorf("provider failed") })}
	if _, err := client.EnrichNote("note"); err == nil {
		t.Fatal("provider error lost")
	}
	client.provider = providerFunc(func(context.Context, Request) (string, error) { return "bad JSON", nil })
	if _, err := client.EnrichNote("note"); err == nil {
		t.Fatal("parse error lost")
	}
	if snap := Snapshot(); snap.Err != 2 || snap.OK != 0 || snap.InFlight != 0 {
		t.Fatalf("stats: %+v", snap)
	}
}

func TestCodexStatusDoesNotUseLocalModelCache(t *testing.T) {
	ResetModelCache()
	defer ResetModelCache()
	cacheResolvedModel("local", "previous-local-model")
	cfg := config.Default()
	if label := StatusLabel(cfg); label != "codex · auto" {
		t.Fatalf("status: %q", label)
	}
}

func TestLegacyConfigSelectsCodex(t *testing.T) {
	cfg := config.Default()
	if err := json.Unmarshal([]byte(`{"lm_studio_enabled":true,"lm_studio_model":"old-local"}`), &cfg); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := client.provider.(codexProvider)
	if !ok || p.model != "" {
		t.Fatalf("legacy local model leaked into Codex: %+v", client.provider)
	}
}

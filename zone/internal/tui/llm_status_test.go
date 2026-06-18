package tui

import (
	"strings"
	"testing"

	"github.com/zkfriendly/zone/internal/config"
	"github.com/zkfriendly/zone/internal/llm"
)

func TestRenderLLMStatus(t *testing.T) {
	s := newStyles()
	llm.ResetStats()

	off := renderLLMStatus(s, config.Config{LMStudioEnabled: false})
	if !strings.Contains(off, "llm") || !strings.Contains(off, "off") {
		t.Fatalf("expected off status: %q", off)
	}

	llm.ResetStats()
	cfg := config.Config{LMStudioEnabled: true, LMStudioModel: "qwen/qwen3-test"}
	on := renderLLMStatus(s, cfg)
	if !strings.Contains(on, "qwen3-test") {
		t.Fatalf("expected model in status: %q", on)
	}
}

func TestDashboardFooterShowsLLMStats(t *testing.T) {
	app, _ := newTestApp(t)
	sizeApp(app)
	out := app.View().Content
	if !strings.Contains(out, "llm") {
		t.Fatalf("expected llm stats in footer:\n%s", out)
	}
}

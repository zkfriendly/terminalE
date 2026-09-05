package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestLLMConfigMigration(t *testing.T) {
	for _, tt := range []struct {
		name, input, provider string
		enabled               bool
	}{
		{"defaults", `{}`, ProviderCodex, true},
		{"legacy enabled", `{"lm_studio_enabled":true}`, ProviderCodex, true},
		{"legacy disabled", `{"lm_studio_enabled":false}`, ProviderCodex, false},
		{"new flag wins", `{"lm_studio_enabled":true,"llm_enabled":false}`, ProviderCodex, false},
		{"new enabled wins", `{"lm_studio_enabled":false,"llm_enabled":true}`, ProviderCodex, true},
		{"explicit local", `{"llm_provider":"local"}`, ProviderLocal, true},
		{"normalized", `{"llm_provider":" LOCAL "}`, ProviderLocal, true},
		{"empty provider", `{"llm_provider":" "}`, ProviderCodex, true},
		{"unknown preserved", `{"llm_provider":"typo"}`, "typo", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			if err := json.Unmarshal([]byte(tt.input), &cfg); err != nil {
				t.Fatal(err)
			}
			cfg.normalize()
			if cfg.LLMEnabled != tt.enabled || cfg.LLMProvider != tt.provider {
				t.Fatalf("unexpected config: %+v", cfg)
			}
		})
	}
}

func TestLLMConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	if base, err := os.UserConfigDir(); err != nil || base != dir {
		t.Skip("config persistence test requires an isolated user config directory")
	}
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	legacy := `{"lm_studio_enabled":false,"lm_studio_url":"http://localhost:1234","lm_studio_model":"my-local-model","work_minutes":25}`
	if err := os.WriteFile(p, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLMEnabled || cfg.LLMProvider != ProviderCodex || cfg.WorkMinutes != 25 {
		t.Fatalf("migration: %+v", cfg)
	}
	cfg.LLMProvider = ProviderLocal
	cfg.CodexModel = "selected-model"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.LLMEnabled || reloaded.LLMProvider != ProviderLocal || reloaded.CodexModel != "selected-model" || reloaded.LMStudioURL != "http://localhost:1234" || reloaded.LMStudioModel != "my-local-model" {
		t.Fatalf("round trip: %+v", reloaded)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "lm_studio_enabled") || !strings.Contains(string(raw), `"llm_enabled": false`) {
		t.Fatalf("legacy flag persisted: %s", raw)
	}
}

func TestNormalizeLocalLLMURL(t *testing.T) {
	tests := map[string]string{
		"11434":                   "http://127.0.0.1:11434",
		"127.0.0.1:11434":         "http://127.0.0.1:11434",
		"http://127.0.0.1:11434":  "http://127.0.0.1:11434",
		"https://example.invalid": "https://example.invalid",
	}
	for in, want := range tests {
		if got := normalizeLocalLLMURL(in); got != want {
			t.Fatalf("normalizeLocalLLMURL(%q) = %q, want %q", in, got, want)
		}
	}
}

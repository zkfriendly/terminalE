package config

import "testing"

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

package llm

import (
	"context"
	"strings"
	"time"

	"github.com/zkfriendly/zone/internal/config"
)

// Provider is the transport boundary. Note prompts and parsers belong to Client,
// so adding a backend does not require duplicating any feature logic.
type Provider interface {
	Complete(context.Context, Request) (string, error)
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Messages    []Message
	Temperature float64
}

type Client struct {
	provider Provider
}

// NewClient snapshots the selected settings before an asynchronous TUI command
// starts. There is deliberately no fallback between local and cloud providers.
func NewClient(cfg config.Config) (*Client, error) {
	if err := cfg.ValidateLLM(); err != nil {
		return nil, err
	}
	var provider Provider
	switch cfg.EffectiveLLMProvider() {
	case config.ProviderCodex:
		command := strings.TrimSpace(cfg.CodexCommand)
		if command == "" {
			command = "codex"
		}
		provider = codexProvider{command: command, model: strings.TrimSpace(cfg.CodexModel)}
	case config.ProviderLocal:
		provider = localProvider{baseURL: strings.TrimRight(cfg.LMStudioURL, "/"), model: cfg.LMStudioModel}
	}
	return &Client{provider: provider}, nil
}

func (c *Client) chat(system, user string, temperature float64) (string, error) {
	return c.chatMulti([]Message{{Role: "system", Content: system}, {Role: "user", Content: user}}, temperature)
}

func (c *Client) chatMulti(messages []Message, temperature float64) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	return c.provider.Complete(ctx, Request{Messages: messages, Temperature: temperature})
}

type localProvider struct {
	baseURL, model string
}

func (p localProvider) Complete(ctx context.Context, req Request) (string, error) {
	model, err := resolveModel(ctx, p.baseURL, p.model)
	if err != nil {
		return "", err
	}
	return localChatMulti(ctx, p.baseURL, model, req.Messages, req.Temperature)
}

// StatusLabel keeps a previously resolved local model out of the Codex status.
func StatusLabel(cfg config.Config) string {
	switch cfg.EffectiveLLMProvider() {
	case config.ProviderCodex:
		return "codex · " + truncateModel(cfg.CodexModel)
	case config.ProviderLocal:
		return "local · " + ModelLabel(cfg.LMStudioModel)
	default:
		return cfg.EffectiveLLMProvider()
	}
}

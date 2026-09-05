package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
}

type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type ollamaTagsResponse struct {
	Models []struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	} `json:"models"`
}

type ollamaChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Options  struct {
		Temperature float64 `json:"temperature"`
	} `json:"options"`
}

type ollamaChatResponse struct {
	Message Message `json:"message"`
}

var (
	resolvedModel   string
	resolvedModels  = map[string]string{}
	resolvedModelMu sync.Mutex
)

func localChatMulti(ctx context.Context, baseURL, modelID string, messages []Message, temperature float64) (string, error) {
	text, err := openAIChatMulti(ctx, baseURL, modelID, messages, temperature)
	if err == nil {
		return text, nil
	}
	ollamaText, ollamaErr := ollamaChatMulti(ctx, baseURL, modelID, messages, temperature)
	if ollamaErr == nil {
		return ollamaText, nil
	}
	return "", fmt.Errorf("local llm chat failed: openai-compatible: %v; ollama: %v", err, ollamaErr)
}

func openAIChatMulti(ctx context.Context, baseURL, modelID string, messages []Message, temperature float64) (string, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model:       modelID,
		Messages:    messages,
		Temperature: temperature,
	})
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := postJSON(ctx, client, baseURL+"/v1/chat/completions", reqBody)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%d: %s", resp.StatusCode, trimErr(string(raw)))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", err
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	msg := cr.Choices[0].Message
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		text = strings.TrimSpace(msg.ReasoningContent)
	}
	if text == "" {
		return "", fmt.Errorf("model returned no text")
	}
	return text, nil
}

func ollamaChatMulti(ctx context.Context, baseURL, modelID string, messages []Message, temperature float64) (string, error) {
	req := ollamaChatRequest{
		Model:    modelID,
		Messages: messages,
		Stream:   false,
	}
	req.Options.Temperature = temperature

	reqBody, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := postJSON(ctx, client, baseURL+"/api/chat", reqBody)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%d: %s", resp.StatusCode, trimErr(string(raw)))
	}

	var cr ollamaChatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", err
	}
	text := strings.TrimSpace(cr.Message.Content)
	if text == "" {
		return "", fmt.Errorf("model returned no text")
	}
	return text, nil
}

// resolveModel picks the configured model or auto-detects the first local chat
// model (skipping embedding models).
func resolveModel(ctx context.Context, baseURL, configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}

	resolvedModelMu.Lock()
	if m := resolvedModels[baseURL]; m != "" {
		resolvedModelMu.Unlock()
		return m, nil
	}
	resolvedModelMu.Unlock()

	if picked, err := resolveOpenAIModel(ctx, baseURL); err == nil {
		cacheResolvedModel(baseURL, picked)
		return picked, nil
	} else if picked, ollamaErr := resolveOllamaModel(ctx, baseURL); ollamaErr == nil {
		cacheResolvedModel(baseURL, picked)
		return picked, nil
	} else {
		return "", fmt.Errorf("local llm models: openai-compatible: %v; ollama: %v", err, ollamaErr)
	}
}

func resolveOpenAIModel(ctx context.Context, baseURL string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := getJSON(ctx, client, baseURL+"/v1/models")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%d: %s", resp.StatusCode, trimErr(string(raw)))
	}

	var mr modelsResponse
	if err := json.Unmarshal(raw, &mr); err != nil {
		return "", err
	}

	var picked string
	for _, m := range mr.Data {
		id := strings.ToLower(m.ID)
		if id == "" || strings.Contains(id, "embed") {
			continue
		}
		picked = m.ID
		break
	}
	if picked == "" {
		return "", fmt.Errorf("no chat model found")
	}
	return picked, nil
}

func resolveOllamaModel(ctx context.Context, baseURL string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := getJSON(ctx, client, baseURL+"/api/tags")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%d: %s", resp.StatusCode, trimErr(string(raw)))
	}

	var tr ollamaTagsResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return "", err
	}
	for _, m := range tr.Models {
		name := m.Name
		if name == "" {
			name = m.Model
		}
		id := strings.ToLower(name)
		if id == "" || strings.Contains(id, "embed") {
			continue
		}
		return name, nil
	}
	return "", fmt.Errorf("no chat model found (run `ollama pull <model>`)")
}

func cacheResolvedModel(baseURL, model string) {
	resolvedModelMu.Lock()
	resolvedModels[baseURL] = model
	resolvedModel = model
	resolvedModelMu.Unlock()
}

// ResetModelCache clears auto-detected models (for tests).
func ResetModelCache() {
	resolvedModelMu.Lock()
	resolvedModel = ""
	resolvedModels = map[string]string{}
	resolvedModelMu.Unlock()
}

func postJSON(ctx context.Context, client *http.Client, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return client.Do(req)
}

func getJSON(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

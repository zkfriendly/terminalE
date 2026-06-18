package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// NoteMeta is a generated title and emoji for a session note.
type NoteMeta struct {
	Title string `json:"title"`
	Emoji string `json:"emoji"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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

var jsonBlockRE = regexp.MustCompile(`(?s)\{[^{}]*"emoji"\s*:\s*"[^"]*"\s*,\s*"title"\s*:\s*"[^"]*"\s*\}`)
var jsonBlockAltRE = regexp.MustCompile(`(?s)\{[^{}]*"title"\s*:\s*"[^"]*"\s*,\s*"emoji"\s*:\s*"[^"]*"\s*\}`)

var (
	resolvedModel   string
	resolvedModelMu sync.Mutex
)

// EnrichNote asks a local LM Studio server to suggest an emoji and short title.
func EnrichNote(baseURL, model, body string) (NoteMeta, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return NoteMeta{}, fmt.Errorf("lm studio url is empty")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return NoteMeta{}, fmt.Errorf("note body is empty")
	}
	if len(body) > 4000 {
		body = body[:4000]
	}

	modelID, err := resolveModel(baseURL, model)
	if err != nil {
		return NoteMeta{}, err
	}

	prompt := `Label this focus-session note. Reply with ONLY one JSON object on a single line.
Use keys "emoji" (one emoji) and "title" (3-6 words). No markdown, no explanation.

Note:
` + body

	reqBody, err := json.Marshal(chatRequest{
		Model: modelID,
		Messages: []chatMessage{
			{Role: "system", Content: "You label notes. Output only raw JSON."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.2,
		MaxTokens:   2048,
	})
	if err != nil {
		return NoteMeta{}, err
	}

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Post(baseURL+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return NoteMeta{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return NoteMeta{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return NoteMeta{}, fmt.Errorf("lm studio %d: %s", resp.StatusCode, trimErr(string(raw)))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return NoteMeta{}, err
	}
	if len(cr.Choices) == 0 {
		return NoteMeta{}, fmt.Errorf("lm studio: empty response")
	}

	msg := cr.Choices[0].Message
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		text = strings.TrimSpace(msg.ReasoningContent)
	}
	if text == "" {
		return NoteMeta{}, fmt.Errorf("lm studio: model returned no text")
	}

	return parseNoteMeta(text)
}

// resolveModel picks the configured model or auto-detects the first loaded
// chat model from LM Studio (skipping embedding models).
func resolveModel(baseURL, configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}

	resolvedModelMu.Lock()
	if resolvedModel != "" {
		m := resolvedModel
		resolvedModelMu.Unlock()
		return m, nil
	}
	resolvedModelMu.Unlock()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseURL + "/v1/models")
	if err != nil {
		return "", fmt.Errorf("lm studio models: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("lm studio models %d: %s", resp.StatusCode, trimErr(string(raw)))
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
		return "", fmt.Errorf("lm studio: no chat model loaded (start a model in LM Studio)")
	}

	resolvedModelMu.Lock()
	resolvedModel = picked
	resolvedModelMu.Unlock()
	return picked, nil
}

func parseNoteMeta(s string) (NoteMeta, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	candidates := append(jsonBlockRE.FindAllString(s, -1), jsonBlockAltRE.FindAllString(s, -1)...)
	if len(candidates) == 0 {
		candidates = []string{s}
	}

	var lastErr error
	for i := len(candidates) - 1; i >= 0; i-- {
		meta, err := decodeNoteMeta(candidates[i])
		if err != nil {
			lastErr = err
			continue
		}
		if isPlaceholderMeta(meta) {
			continue
		}
		return meta, nil
	}
	if lastErr != nil {
		return NoteMeta{}, lastErr
	}
	return NoteMeta{}, fmt.Errorf("parse note meta: no valid JSON (got: %s)", trimErr(s))
}

func decodeNoteMeta(s string) (NoteMeta, error) {
	var meta NoteMeta
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &meta); err != nil {
		return NoteMeta{}, fmt.Errorf("parse note meta: %w (got: %s)", err, trimErr(s))
	}
	meta.Title = strings.TrimSpace(meta.Title)
	meta.Emoji = strings.TrimSpace(meta.Emoji)
	if meta.Title == "" {
		return NoteMeta{}, fmt.Errorf("empty title in response")
	}
	if meta.Emoji == "" {
		meta.Emoji = "📝"
	}
	if len([]rune(meta.Title)) > 60 {
		meta.Title = string([]rune(meta.Title)[:60])
	}
	if rs := []rune(meta.Emoji); len(rs) > 0 {
		meta.Emoji = string(rs[:1])
	}
	return meta, nil
}

func isPlaceholderMeta(m NoteMeta) bool {
	if strings.Contains(m.Title, "<") && strings.Contains(m.Title, ">") {
		return true
	}
	if strings.Contains(m.Emoji, "<") && strings.Contains(m.Emoji, ">") {
		return true
	}
	return false
}

func trimErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

// ResetModelCache clears auto-detected model (for tests).
func ResetModelCache() { resolvedModelMu.Lock(); resolvedModel = ""; resolvedModelMu.Unlock() }

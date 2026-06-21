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
	Model           string        `json:"model"`
	Messages        []chatMessage `json:"messages"`
	Temperature     float64       `json:"temperature"`
	MaxTokens       int           `json:"max_tokens"`
	ReasoningTokens int           `json:"reasoning_tokens,omitempty"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Note LLM calls only need a short JSON answer, but reasoning models need room
// to think. Use LM Studio's unlimited completion (-1) and max reasoning effort.
const noteReasoningEffort = "high"

func noteChatRequest(modelID, system, user string, temperature float64) ([]byte, error) {
	return json.Marshal(chatRequest{
		Model: modelID,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature:     temperature,
		MaxTokens:       -1,
		ReasoningEffort: noteReasoningEffort,
	})
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
var actionablesBlockRE = regexp.MustCompile(`(?s)\{[^{}]*"has_actionables"\s*:\s*(true|false)\s*\}`)
var actionablesTemplateRE = regexp.MustCompile(`(?s)(?:\{"has_actionables"\s*:\s*true\s*\}\s*or\s*\{"has_actionables"\s*:\s*false\s*\}|` + "`" + `\{"has_actionables"\s*:\s*true\s*\}` + "`" + `\s*or\s*` + "`" + `\{"has_actionables"\s*:\s*false\s*\}` + "`" + `)`)

// ActionablesScanVersion bumps when the detection prompt changes so stale scans are redone.
const ActionablesScanVersion = 4

// ActionablesExtractVersion bumps when the extraction prompt changes so stale caches are redone.
const ActionablesExtractVersion = 1

var tasksBlockRE = regexp.MustCompile(`(?s)\{[^{}]*"tasks"\s*:\s*\[[^\]]*\]\s*\}`)

var (
	resolvedModel   string
	resolvedModelMu sync.Mutex
)

// EnrichNote asks a local LM Studio server to suggest an emoji and short title.
func EnrichNote(baseURL, model, body string) (NoteMeta, error) {
	start := time.Now()
	recordBegin()
	fail := func(err error) (NoteMeta, error) {
		recordFailure(time.Since(start))
		return NoteMeta{}, err
	}

	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return fail(fmt.Errorf("lm studio url is empty"))
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return fail(fmt.Errorf("note body is empty"))
	}
	if len(body) > 4000 {
		body = body[:4000]
	}

	modelID, err := resolveModel(baseURL, model)
	if err != nil {
		return fail(err)
	}

	prompt := `Label this focus-session note. Reply with ONLY one JSON object on a single line.
Use keys "emoji" (one emoji) and "title" (3-6 words). No markdown, no explanation.

Note:
` + body

	reqBody, err := noteChatRequest(modelID,
		"You label notes. Output only raw JSON.",
		prompt,
		0.2,
	)
	if err != nil {
		return fail(err)
	}

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Post(baseURL+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fail(err)
	}
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("lm studio %d: %s", resp.StatusCode, trimErr(string(raw))))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return fail(err)
	}
	if len(cr.Choices) == 0 {
		return fail(fmt.Errorf("lm studio: empty response"))
	}

	msg := cr.Choices[0].Message
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		text = strings.TrimSpace(msg.ReasoningContent)
	}
	if text == "" {
		return fail(fmt.Errorf("lm studio: model returned no text"))
	}

	meta, err := parseNoteMeta(text)
	if err != nil {
		return fail(err)
	}
	recordSuccess(time.Since(start))
	return meta, nil
}

// DetectActionables asks a local LM Studio server whether a note contains clear tasks.
func DetectActionables(baseURL, model, body string) (bool, error) {
	start := time.Now()
	recordBegin()
	fail := func(err error) (bool, error) {
		recordFailure(time.Since(start))
		return false, err
	}

	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return fail(fmt.Errorf("lm studio url is empty"))
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return fail(fmt.Errorf("note body is empty"))
	}
	if len(body) > 4000 {
		body = body[:4000]
	}

	modelID, err := resolveModel(baseURL, model)
	if err != nil {
		return fail(err)
	}

	prompt := actionablesPrompt(body)

	reqBody, err := noteChatRequest(modelID,
		"You classify focus-session notes for extractable tasks. Output only raw JSON.",
		prompt,
		0.1,
	)
	if err != nil {
		return fail(err)
	}

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Post(baseURL+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fail(err)
	}
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("lm studio %d: %s", resp.StatusCode, trimErr(string(raw))))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return fail(err)
	}
	if len(cr.Choices) == 0 {
		return fail(fmt.Errorf("lm studio: empty response"))
	}

	msg := cr.Choices[0].Message
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		text = strings.TrimSpace(msg.ReasoningContent)
	}
	if text == "" {
		return fail(fmt.Errorf("lm studio: model returned no text"))
	}

	has, err := parseActionablesResult(text)
	if err != nil {
		return fail(err)
	}
	recordSuccess(time.Since(start))
	return has, nil
}

// ExtractActionables pulls concrete task strings from a note body.
func ExtractActionables(baseURL, model, body string) ([]string, error) {
	start := time.Now()
	recordBegin()
	fail := func(err error) ([]string, error) {
		recordFailure(time.Since(start))
		return nil, err
	}

	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return fail(fmt.Errorf("lm studio url is empty"))
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return fail(fmt.Errorf("note body is empty"))
	}
	if len(body) > 4000 {
		body = body[:4000]
	}

	modelID, err := resolveModel(baseURL, model)
	if err != nil {
		return fail(err)
	}

	prompt := extractActionablesPrompt(body)
	reqBody, err := noteChatRequest(modelID,
		"You extract actionable tasks from notes. Output only raw JSON.",
		prompt,
		0.1,
	)
	if err != nil {
		return fail(err)
	}

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Post(baseURL+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fail(err)
	}
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("lm studio %d: %s", resp.StatusCode, trimErr(string(raw))))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return fail(err)
	}
	if len(cr.Choices) == 0 {
		return fail(fmt.Errorf("lm studio: empty response"))
	}

	msg := cr.Choices[0].Message
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		text = strings.TrimSpace(msg.ReasoningContent)
	}
	if text == "" {
		return fail(fmt.Errorf("lm studio: model returned no text"))
	}

	tasks, err := parseExtractedTasks(text)
	if err != nil {
		return fail(err)
	}
	recordSuccess(time.Since(start))
	return tasks, nil
}

func extractActionablesPrompt(body string) string {
	return `Extract every clear, concrete action item from this focus-session note.
Reply with ONLY one JSON object: {"tasks": ["short imperative task", ...]}.
Each task should be 5–15 words, specific enough to act on. Skip mood, status updates, and vague ideas.

Note:
` + body
}

func parseExtractedTasks(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	type result struct {
		Tasks []string `json:"tasks"`
	}

	tryDecode := func(candidate string) ([]string, bool) {
		var r result
		if err := json.Unmarshal([]byte(strings.TrimSpace(candidate)), &r); err != nil {
			return nil, false
		}
		var out []string
		for _, t := range r.Tasks {
			t = strings.TrimSpace(t)
			if t != "" {
				out = append(out, t)
			}
		}
		return out, true
	}

	candidates := tasksBlockRE.FindAllString(s, -1)
	if len(candidates) == 0 {
		candidates = []string{s}
	}
	for i := len(candidates) - 1; i >= 0; i-- {
		if tasks, ok := tryDecode(candidates[i]); ok {
			return tasks, nil
		}
	}
	return nil, fmt.Errorf("parse extracted tasks: no valid JSON (got: %s)", trimErr(s))
}

func actionablesPrompt(body string) string {
	return `Classify whether this focus-session note contains extractable action items — work the user intends to do on a project (build, fix, implement, change something).

Reply with ONLY one JSON object: {"has_actionables": true} or {"has_actionables": false}.

Answer TRUE when the note includes ANY of:
- Explicit todos, checklists, or bullet lists of tasks
- A bug or problem plus a described fix ("we need to fix X", "there is a bug in Y")
- Feature or product work the user wants to build, even in casual prose ("I want to add...", "now I want to...", "I should be able to...")
- Brainstorming that names concrete things to implement (multiple features, UI changes, workflows)

Answer FALSE when the note is ONLY:
- Mood, satisfaction, or session commentary with no new work ("this is fun", "never been happier")
- A status update about trying something or continuing work, with no new tasks ("I tried X and gave up", "I can peacefully continue now")
- A research or curiosity question without a stated build/fix task ("what hardware would I need?")
- Placeholder, empty, or meta notes ("this is a note")

Examples — TRUE:
- "todo: finish the refactor for buffer abstraction"
- "I want to add a feature: when I have a note, use the LLM to extract actionables and turn them into tasks"
- "realised a bug in the editor — text overflows with no indicator; we need to fix that so users know to scroll"
- "everyday todo: kiss fazi 2 times"

Examples — FALSE:
- "beautiful, now that I wrote everything down I can peacefully continue with GLM"
- "I tried the model and gave up because it was too expensive"
- "what hardware would I need to run this locally?"
- "This is fun"

When the note mixes reflection with concrete build/fix work, answer TRUE if there is at least one extractable task. Prefer TRUE when the note describes specific software or product changes.

Note:
` + body
}

func parseActionablesResult(s string) (bool, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	// Reasoning models may put the answer on its own last line.
	for _, line := range reversedLines(s) {
		line = strings.TrimSpace(strings.Trim(line, "`"))
		if has, ok := decodeActionables(line); ok {
			return has, nil
		}
	}

	candidateIdx := actionablesBlockRE.FindAllStringIndex(s, -1)
	if len(candidateIdx) == 0 {
		if has, ok := decodeActionables(s); ok {
			return has, nil
		}
		return false, fmt.Errorf("parse actionables: no valid JSON (got: %s)", trimErr(s))
	}
	templateLoc := actionablesTemplateRE.FindStringIndex(s)

	var lastErr error
	for i := len(candidateIdx) - 1; i >= 0; i-- {
		start, end := candidateIdx[i][0], candidateIdx[i][1]
		if templateLoc != nil && start >= templateLoc[0] && end <= templateLoc[1] {
			continue
		}
		if isActionablesPromptEcho(s, start, end) {
			continue
		}
		if has, ok := decodeActionables(s[start:end]); ok {
			return has, nil
		}
		lastErr = fmt.Errorf("invalid candidate %q", s[start:end])
	}
	if lastErr != nil {
		return false, fmt.Errorf("parse actionables: %w (got: %s)", lastErr, trimErr(s))
	}
	return false, fmt.Errorf("parse actionables: no valid JSON (got: %s)", trimErr(s))
}

func decodeActionables(s string) (bool, bool) {
	type result struct {
		HasActionables bool `json:"has_actionables"`
	}
	var r result
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &r); err != nil {
		return false, false
	}
	return r.HasActionables, true
}

func isActionablesPromptEcho(s string, start, end int) bool {
	lineStart := strings.LastIndex(s[:start], "\n")
	if lineStart == -1 {
		lineStart = 0
	} else {
		lineStart++
	}
	lineEndRel := strings.Index(s[end:], "\n")
	lineEnd := len(s)
	if lineEndRel >= 0 {
		lineEnd = end + lineEndRel
	}
	line := strings.ToLower(s[lineStart:lineEnd])
	for _, marker := range []string{
		"reply with only",
		"output format",
		"must be only raw json",
		"check constraints",
	} {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}

func reversedLines(s string) []string {
	lines := strings.Split(s, "\n")
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return lines
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

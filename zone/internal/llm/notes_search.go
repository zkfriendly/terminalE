package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// NoteSnippet is one note passed to the search/chat LLM.
type NoteSnippet struct {
	ID          int64
	Title       string
	Emoji       string
	Body        string
	TaskTitle   string
	ProjectName string
	CreatedAt   time.Time
}

// ChatTurn is one message in a notes search follow-up conversation.
type ChatTurn struct {
	Role    string // "user" or "assistant"
	Content string
}

const maxNoteBodyChars = 1200
const maxNotesInContext = 24

// FormatNoteSnippets builds a plain-text corpus for the LLM from search hits.
func FormatNoteSnippets(notes []NoteSnippet) string {
	if len(notes) == 0 {
		return "(no matching notes)"
	}
	if len(notes) > maxNotesInContext {
		notes = notes[:maxNotesInContext]
	}
	var b strings.Builder
	for i, n := range notes {
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		label := noteSnippetLabel(n)
		b.WriteString(fmt.Sprintf("[%d] %s (%s)\n", i+1, label, n.CreatedAt.Format("2006-01-02 15:04")))
		if n.TaskTitle != "" || n.ProjectName != "" {
			b.WriteString(fmt.Sprintf("Context: %s", n.TaskTitle))
			if n.ProjectName != "" {
				b.WriteString(" · " + n.ProjectName)
			}
			b.WriteString("\n")
		}
		body := strings.TrimSpace(n.Body)
		if len(body) > maxNoteBodyChars {
			body = body[:maxNoteBodyChars] + "…"
		}
		b.WriteString(body)
	}
	return b.String()
}

func noteSnippetLabel(n NoteSnippet) string {
	if n.Title != "" {
		return n.Emoji + " " + n.Title
	}
	body := strings.TrimSpace(strings.ReplaceAll(n.Body, "\n", " "))
	if len(body) > 60 {
		body = body[:60] + "…"
	}
	return body
}

// AnswerNotesQuestion answers using the note corpus and optional chat history.
func AnswerNotesQuestion(baseURL, model, question, notesContext string, history []ChatTurn) (string, error) {
	start := time.Now()
	recordBegin()
	fail := func(err error) (string, error) {
		recordFailure(time.Since(start))
		return "", err
	}

	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return fail(fmt.Errorf("local llm url is empty"))
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return fail(fmt.Errorf("question is empty"))
	}

	modelID, err := resolveModel(baseURL, model)
	if err != nil {
		return fail(err)
	}

	system := `You summarize focus-session notes. Use ONLY the notes below.
Reply in at most 2–3 short sentences, OR up to 4 one-line bullets. Keep it brief.
Lead with the direct answer. No preamble, no question recap, no section headers.
Cite note numbers like [1] at most once. Plain text only.
If the notes lack enough information, say so in one short sentence.
Do not invent facts.`

	messages := []chatMessage{
		{Role: "system", Content: system + "\n\n--- Matching notes ---\n\n" + notesContext},
	}
	for _, h := range history {
		role := h.Role
		if role != "user" && role != "assistant" {
			continue
		}
		messages = append(messages, chatMessage{Role: role, Content: h.Content})
	}
	messages = append(messages, chatMessage{Role: "user", Content: question})

	text, err := localChatMulti(baseURL, modelID, messages, 0.2)
	if err != nil {
		return fail(err)
	}
	recordSuccess(time.Since(start))
	return strings.TrimSpace(text), nil
}

func localChatMulti(baseURL, modelID string, messages []chatMessage, temperature float64) (string, error) {
	text, err := openAIChatMulti(baseURL, modelID, messages, temperature)
	if err == nil {
		return text, nil
	}
	ollamaText, ollamaErr := ollamaChatMulti(baseURL, modelID, messages, temperature)
	if ollamaErr == nil {
		return ollamaText, nil
	}
	return "", fmt.Errorf("local llm chat failed: openai-compatible: %v; ollama: %v", err, ollamaErr)
}

func openAIChatMulti(baseURL, modelID string, messages []chatMessage, temperature float64) (string, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model:       modelID,
		Messages:    messages,
		Temperature: temperature,
	})
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Post(baseURL+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
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

func ollamaChatMulti(baseURL, modelID string, messages []chatMessage, temperature float64) (string, error) {
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
	resp, err := client.Post(baseURL+"/api/chat", "application/json", bytes.NewReader(reqBody))
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

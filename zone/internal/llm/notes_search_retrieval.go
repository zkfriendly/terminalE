package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// SearchExpansion is LLM-generated retrieval context for a notes question.
type SearchExpansion struct {
	Keywords []string
	Hyde     string // hypothetical note text (HyDE-style) for semantic matching
}

var searchExpansionRE = regexp.MustCompile(`(?s)\{[^{}]*"keywords"\s*:\s*\[[^\]]*\][^{}]*\}`)
var rerankIDsRE = regexp.MustCompile(`(?s)\{[^{}]*"ids"\s*:\s*\[[^\]]*\]\s*\}`)

// ExpandNotesSearchQuery asks the LLM for keyword variants and a hypothetical matching note.
func (c *Client) ExpandNotesSearchQuery(question string) (SearchExpansion, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return SearchExpansion{}, fmt.Errorf("question is empty")
	}

	prompt := `The user is searching their focus-session notes. Question:
` + question + `

Reply with ONLY one JSON object:
{"keywords":["term1","term2"],"hyde":"one sentence a matching note might contain"}

keywords: 3–8 concrete search terms — synonyms, related concepts, project names, acronyms (not filler words).
hyde: one short hypothetical note sentence that would answer the question (for semantic matching).`

	text, err := c.chat(
		"You expand note search queries. Output only raw JSON.",
		prompt,
		0.2,
	)
	if err != nil {
		return SearchExpansion{}, err
	}
	return parseSearchExpansion(text)
}

// RerankNotesForQuery asks the LLM which notes best match the question.
// Returns note IDs in relevance order (most relevant first).
func (c *Client) RerankNotesForQuery(question string, catalog []NoteSnippet, limit int) ([]int64, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("question is empty")
	}
	if len(catalog) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}

	prompt := `Question: ` + question + `

Notes catalog:
` + FormatNoteCatalog(catalog) + `

Reply with ONLY one JSON object:
{"ids":[note_id,...]}

List note IDs from the catalog (the id= numbers, or [n] entry numbers), best first. Include notes related by meaning even when exact words differ. Omit irrelevant notes. Max ` + fmt.Sprintf("%d", limit) + ` ids.`

	text, err := c.chat(
		"You rank notes by relevance to a question. Output only raw JSON.",
		prompt,
		0.1,
	)
	if err != nil {
		return nil, err
	}
	ids, err := parseRerankedNoteIDs(text)
	if err != nil {
		return nil, err
	}
	return mapRerankIDs(ids, catalog), nil
}

const maxCatalogNotes = 80
const maxCatalogBodyChars = 220

// FormatNoteCatalog builds a compact index for LLM reranking.
func FormatNoteCatalog(notes []NoteSnippet) string {
	if len(notes) > maxCatalogNotes {
		notes = notes[:maxCatalogNotes]
	}
	var b strings.Builder
	for i, n := range notes {
		label := noteSnippetLabel(n)
		ctx := n.TaskTitle
		if n.ProjectName != "" {
			if ctx != "" {
				ctx += " · "
			}
			ctx += n.ProjectName
		}
		b.WriteString(fmt.Sprintf("[%d] id=%d | %s", i+1, n.ID, label))
		if ctx != "" {
			b.WriteString(" | " + ctx)
		}
		b.WriteString(" | " + n.CreatedAt.Format("2006-01-02"))
		b.WriteByte('\n')
		body := strings.TrimSpace(strings.ReplaceAll(n.Body, "\n", " "))
		if len(body) > maxCatalogBodyChars {
			body = body[:maxCatalogBodyChars] + "…"
		}
		b.WriteString(body)
		b.WriteByte('\n')
		b.WriteByte('\n')
	}
	return b.String()
}

func parseSearchExpansion(s string) (SearchExpansion, error) {
	type raw struct {
		Keywords []string `json:"keywords"`
		Hyde     string   `json:"hyde"`
	}
	decode := func(candidate string) (SearchExpansion, error) {
		var r raw
		if err := json.Unmarshal([]byte(strings.TrimSpace(candidate)), &r); err != nil {
			return SearchExpansion{}, err
		}
		var keywords []string
		seen := map[string]bool{}
		for _, k := range r.Keywords {
			k = strings.ToLower(strings.TrimSpace(k))
			if len(k) < 2 || seen[k] {
				continue
			}
			seen[k] = true
			keywords = append(keywords, k)
		}
		return SearchExpansion{
			Keywords: keywords,
			Hyde:     strings.TrimSpace(r.Hyde),
		}, nil
	}

	s = stripJSONFence(s)
	candidates := searchExpansionRE.FindAllString(s, -1)
	if len(candidates) == 0 {
		candidates = []string{s}
	}
	var lastErr error
	for i := len(candidates) - 1; i >= 0; i-- {
		exp, err := decode(candidates[i])
		if err != nil {
			lastErr = err
			continue
		}
		return exp, nil
	}
	if lastErr != nil {
		return SearchExpansion{}, fmt.Errorf("parse search expansion: %w", lastErr)
	}
	return SearchExpansion{}, fmt.Errorf("parse search expansion: no valid JSON")
}

func parseRerankedNoteIDs(s string) ([]int64, error) {
	type raw struct {
		IDs []int64 `json:"ids"`
	}
	decode := func(candidate string) ([]int64, error) {
		var r raw
		if err := json.Unmarshal([]byte(strings.TrimSpace(candidate)), &r); err != nil {
			return nil, err
		}
		return r.IDs, nil
	}

	s = stripJSONFence(s)
	candidates := rerankIDsRE.FindAllString(s, -1)
	if len(candidates) == 0 {
		candidates = []string{s}
	}
	var lastErr error
	for i := len(candidates) - 1; i >= 0; i-- {
		ids, err := decode(candidates[i])
		if err != nil {
			lastErr = err
			continue
		}
		return ids, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("parse reranked ids: %w", lastErr)
	}
	return nil, fmt.Errorf("parse reranked ids: no valid JSON")
}

// mapRerankIDs resolves catalog [n] indices to real note ids when the model returns entry numbers.
func mapRerankIDs(raw []int64, catalog []NoteSnippet) []int64 {
	if len(raw) == 0 || len(catalog) == 0 {
		return raw
	}
	byID := map[int64]bool{}
	for _, n := range catalog {
		byID[n.ID] = true
	}
	var out []int64
	seen := map[int64]bool{}
	for _, v := range raw {
		id := v
		if !byID[v] && v >= 1 && v <= int64(len(catalog)) {
			id = catalog[v-1].ID
		}
		if !byID[id] || seen[id] {
			continue
		}
		out = append(out, id)
		seen[id] = true
	}
	return out
}

func stripJSONFence(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

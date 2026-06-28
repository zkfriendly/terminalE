package store

import (
	"sort"
	"strings"
	"unicode"
)

var searchStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "but": true,
	"about": true, "what": true, "which": true, "who": true, "whom": true,
	"where": true, "when": true, "why": true, "how": true, "do": true, "does": true,
	"did": true, "i": true, "me": true, "my": true, "we": true, "our": true,
	"you": true, "your": true, "know": true, "is": true, "are": true, "was": true,
	"were": true, "be": true, "been": true, "being": true, "have": true, "has": true,
	"had": true, "all": true, "any": true, "some": true, "this": true, "that": true,
	"these": true, "those": true, "from": true, "with": true, "for": true, "to": true,
	"of": true, "in": true, "on": true, "at": true, "by": true, "it": true, "its": true,
	"can": true, "could": true, "would": true, "should": true, "tell": true, "show": true,
	"find": true, "notes": true, "note": true, "regarding": true, "related": true,
}

// SearchNotes finds notes whose body, title, task, or project match the query terms.
// Natural-language questions are tokenized (stop words removed); results rank by match count.
func (s *Store) SearchNotes(query string, limit int) ([]GlobalNote, error) {
	terms := extractSearchTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}

	// Pull a broad candidate set, then rank in Go.
	candidates, err := s.ListAllNotes(2000)
	if err != nil {
		return nil, err
	}

	type scored struct {
		n     GlobalNote
		score int
	}
	var hits []scored
	for _, n := range candidates {
		if sc := scoreNote(n, terms); sc > 0 {
			hits = append(hits, scored{n: n, score: sc})
		}
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].n.CreatedAt.After(hits[j].n.CreatedAt)
	})

	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]GlobalNote, len(hits))
	for i, h := range hits {
		out[i] = h.n
	}
	return out, nil
}

// SearchNotesWithTerms finds notes matching any of the given terms.
func (s *Store) SearchNotesWithTerms(terms []string, limit int) ([]GlobalNote, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}

	candidates, err := s.ListAllNotes(2000)
	if err != nil {
		return nil, err
	}

	type scored struct {
		n     GlobalNote
		score int
	}
	var hits []scored
	for _, n := range candidates {
		if sc := scoreNote(n, terms); sc > 0 {
			hits = append(hits, scored{n: n, score: sc})
		}
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].n.CreatedAt.After(hits[j].n.CreatedAt)
	})

	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]GlobalNote, len(hits))
	for i, h := range hits {
		out[i] = h.n
	}
	return out, nil
}

const rrfK = 60

// RRFMergeNotes merges ranked note lists with reciprocal rank fusion.
func RRFMergeNotes(lists [][]GlobalNote, limit int) []GlobalNote {
	if limit <= 0 {
		limit = 50
	}
	scores := map[int64]float64{}
	notes := map[int64]GlobalNote{}
	for _, list := range lists {
		for rank, n := range list {
			id := n.ID
			notes[id] = n
			scores[id] += 1.0 / float64(rrfK+rank+1)
		}
	}
	type scored struct {
		n     GlobalNote
		score float64
	}
	var merged []scored
	for id, score := range scores {
		merged = append(merged, scored{n: notes[id], score: score})
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].score != merged[j].score {
			return merged[i].score > merged[j].score
		}
		return merged[i].n.CreatedAt.After(merged[j].n.CreatedAt)
	})
	if len(merged) > limit {
		merged = merged[:limit]
	}
	out := make([]GlobalNote, len(merged))
	for i, h := range merged {
		out[i] = h.n
	}
	return out
}

// OrderNotesByIDs returns notes ordered by ids, then any remaining candidates.
func OrderNotesByIDs(candidates []GlobalNote, ids []int64, limit int) []GlobalNote {
	if limit <= 0 {
		limit = 50
	}
	byID := map[int64]GlobalNote{}
	for _, n := range candidates {
		byID[n.ID] = n
	}
	seen := map[int64]bool{}
	var out []GlobalNote
	for _, id := range ids {
		if seen[id] {
			continue
		}
		if n, ok := byID[id]; ok {
			out = append(out, n)
			seen[id] = true
		}
		if len(out) >= limit {
			return out
		}
	}
	for _, n := range candidates {
		if seen[n.ID] {
			continue
		}
		out = append(out, n)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func extractSearchTerms(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	var b strings.Builder
	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	raw := strings.Fields(b.String())
	seen := map[string]bool{}
	var terms []string
	for _, w := range raw {
		if len(w) < 2 || searchStopWords[w] {
			continue
		}
		if seen[w] {
			continue
		}
		seen[w] = true
		terms = append(terms, w)
	}
	if len(terms) == 0 {
		// Fall back to the longest raw token so "whir" still works inside a question.
		longest := ""
		for _, w := range raw {
			if len(w) > len(longest) {
				longest = w
			}
		}
		if len(longest) >= 2 {
			return []string{longest}
		}
	}
	return terms
}

func scoreNote(n GlobalNote, terms []string) int {
	hay := strings.ToLower(strings.Join([]string{
		n.Body, n.Title, n.TaskTitle, n.ProjectName,
	}, "\n"))
	score := 0
	for _, term := range terms {
		if strings.Contains(hay, term) {
			score++
			// Boost title/project hits.
			if strings.Contains(strings.ToLower(n.Title), term) {
				score++
			}
			if strings.Contains(strings.ToLower(n.ProjectName), term) {
				score++
			}
		}
	}
	return score
}

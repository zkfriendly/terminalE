package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/zkfriendly/zone/internal/config"
	"github.com/zkfriendly/zone/internal/llm"
	"github.com/zkfriendly/zone/internal/store"
)

const notesSearchResultLimit = 50

func searchNotesCmd(st *store.Store, cfg *config.Config, reqID int, question string, history []llm.ChatTurn) tea.Cmd {
	return func() tea.Msg {
		if err := noteLabelStatusErr(cfg); err != nil {
			return notesSearchAnswerMsg{reqID: reqID, question: question, err: err}
		}

		keywordHits := preliminaryNotesSearch(st, question)
		results := aiEnhanceSearch(st, cfg, question, keywordHits, notesSearchResultLimit)
		if len(results) == 0 {
			results = keywordHits
		}
		if len(keywordHits) > 0 && len(results) > 0 && !notesOverlap(results, keywordHits) {
			results = keywordHits
		}

		ctx := llm.FormatNoteSnippets(noteSnippetsFromGlobal(results))
		answer, ansErr := llm.AnswerNotesQuestion(cfg.LMStudioURL, cfg.LMStudioModel, question, ctx, history)
		msg := notesSearchAnswerMsg{
			reqID:    reqID,
			question: question,
			answer:   answer,
			results:  results,
		}
		if ansErr != nil {
			msg.err = ansErr
			if answer == "" && len(results) > 0 {
				msg.answer = "Could not generate a summary. Matching notes are listed below."
			}
		}
		return msg
	}
}

func aiSearchNotes(st *store.Store, cfg *config.Config, question string, limit int) ([]store.GlobalNote, error) {
	keywordHits, err := st.SearchNotes(question, limit)
	if err != nil {
		return nil, err
	}

	enhanced := aiEnhanceSearch(st, cfg, question, keywordHits, limit)
	if len(enhanced) == 0 {
		return keywordHits, nil
	}
	if len(keywordHits) > 0 && !notesOverlap(enhanced, keywordHits) {
		return keywordHits, nil
	}
	return enhanced, nil
}

func aiEnhanceSearch(st *store.Store, cfg *config.Config, question string, keywordHits []store.GlobalNote, limit int) []store.GlobalNote {
	baseURL := cfg.LMStudioURL
	model := cfg.LMStudioModel

	expansion, expandErr := llm.ExpandNotesSearchQuery(baseURL, model, question)

	var lists [][]store.GlobalNote
	appendList := func(list []store.GlobalNote) {
		if len(list) > 0 {
			lists = append(lists, list)
		}
	}
	appendList(keywordHits)

	if expandErr == nil {
		if len(expansion.Keywords) > 0 {
			if hits, err := st.SearchNotesWithTerms(expansion.Keywords, 80); err == nil {
				appendList(hits)
			}
		}
		if expansion.Hyde != "" {
			if hits, err := st.SearchNotes(expansion.Hyde, 80); err == nil {
				appendList(hits)
			}
		}
	}

	candidates := store.RRFMergeNotes(lists, 60)
	if len(candidates) == 0 {
		return keywordHits
	}

	snippets := noteSnippetsFromGlobal(candidates)
	ids, rerankErr := llm.RerankNotesForQuery(baseURL, model, question, snippets, limit)
	if rerankErr != nil || len(ids) == 0 {
		return mergeKeywordFirst(keywordHits, candidates, limit)
	}

	reranked := store.OrderNotesByIDs(candidates, ids, limit)
	if len(reranked) == 0 {
		return mergeKeywordFirst(keywordHits, candidates, limit)
	}
	return reranked
}

func mergeKeywordFirst(keywordHits, candidates []store.GlobalNote, limit int) []store.GlobalNote {
	if len(keywordHits) == 0 {
		return trimNotes(candidates, limit)
	}
	merged := store.OrderNotesByIDs(candidates, noteIDs(keywordHits), limit)
	if len(merged) == 0 {
		return trimNotes(keywordHits, limit)
	}
	return merged
}

func noteIDs(notes []store.GlobalNote) []int64 {
	ids := make([]int64, len(notes))
	for i, n := range notes {
		ids[i] = n.ID
	}
	return ids
}

func notesOverlap(a, b []store.GlobalNote) bool {
	seen := map[int64]bool{}
	for _, n := range a {
		seen[n.ID] = true
	}
	for _, n := range b {
		if seen[n.ID] {
			return true
		}
	}
	return false
}

func trimNotes(notes []store.GlobalNote, limit int) []store.GlobalNote {
	if limit <= 0 || len(notes) <= limit {
		return notes
	}
	return notes[:limit]
}

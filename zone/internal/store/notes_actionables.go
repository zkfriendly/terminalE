package store

import (
	"encoding/json"
	"fmt"

	"github.com/zkfriendly/zone/internal/llm"
)

// LoadSessionNoteActionables returns cached extracted tasks when still valid.
func (s *Store) LoadSessionNoteActionables(id int64) ([]string, bool, error) {
	n, err := s.GetSessionNote(id)
	if err != nil {
		return nil, false, err
	}
	if n.NeedsActionableExtract() || n.ActionablesJSON == "" {
		return nil, false, nil
	}
	var tasks []string
	if err := json.Unmarshal([]byte(n.ActionablesJSON), &tasks); err != nil {
		return nil, false, fmt.Errorf("decode actionables: %w", err)
	}
	return tasks, true, nil
}

// SaveSessionNoteActionables stores extracted tasks for a note.
func (s *Store) SaveSessionNoteActionables(id int64, tasks []string) error {
	data, err := json.Marshal(tasks)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`UPDATE session_notes
		 SET actionables_json = ?, actionables_extracted_at = strftime('%s','now'),
		     actionables_extract_version = ?
		 WHERE id = ?`,
		string(data), llm.ActionablesExtractVersion, id,
	)
	return err
}

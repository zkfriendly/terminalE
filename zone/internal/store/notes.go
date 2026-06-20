package store

import (
	"database/sql"
	"time"

	"github.com/zkfriendly/zone/internal/llm"
)

// SessionNote is a free-form note taken during a focus session.
type SessionNote struct {
	ID                   int64
	SessionID            int64
	Body                 string
	Title                string // LLM-generated short label
	Emoji                string // LLM-generated emoji
	CreatedAt            time.Time
	UpdatedAt            time.Time
	ActionablesScannedAt   *time.Time
	HasActionables         bool
	ActionablesScanVersion int
}

// NeedsActionableScan reports whether the note body changed since the last scan,
// or the scan used an older prompt version.
func (n SessionNote) NeedsActionableScan() bool {
	if n.ActionablesScannedAt == nil {
		return true
	}
	if n.ActionablesScanVersion < llm.ActionablesScanVersion {
		return true
	}
	return n.ActionablesScannedAt.Before(n.UpdatedAt)
}

// GlobalNote is a session note with session context for cross-session browsing.
type GlobalNote struct {
	SessionNote
	SessionStarted time.Time
	TaskTitle      string
	ProjectName    string
}

func scanSessionNote(row interface {
	Scan(dest ...any) error
}) (SessionNote, error) {
	var n SessionNote
	var created, updated int64
	var scanned sql.NullInt64
	if err := row.Scan(
		&n.ID, &n.SessionID, &n.Body, &n.Title, &n.Emoji,
		&created, &updated, &scanned, &n.HasActionables, &n.ActionablesScanVersion,
	); err != nil {
		return SessionNote{}, err
	}
	n.CreatedAt = toTime(created)
	n.UpdatedAt = toTime(updated)
	n.ActionablesScannedAt = toTimePtr(scanned)
	return n, nil
}

const sessionNoteCols = `id, session_id, body, title, emoji, created_at, updated_at, actionables_scanned_at, has_actionables, actionables_scan_version`

// AddSessionNote appends a note to a focus session.
func (s *Store) AddSessionNote(sessionID int64, body string) (SessionNote, error) {
	res, err := s.db.Exec(
		`INSERT INTO session_notes (session_id, body, updated_at) VALUES (?, ?, strftime('%s','now'))`,
		sessionID, body,
	)
	if err != nil {
		return SessionNote{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetSessionNote(id)
}

// GetSessionNote fetches a note by id.
func (s *Store) GetSessionNote(id int64) (SessionNote, error) {
	row := s.db.QueryRow(
		`SELECT `+sessionNoteCols+` FROM session_notes WHERE id = ?`, id,
	)
	return scanSessionNote(row)
}

// UpdateSessionNote replaces the body of an existing note. Generated title/emoji
// are kept so edits do not trigger re-labeling. Actionable scan state is cleared.
func (s *Store) UpdateSessionNote(id int64, body string) (SessionNote, error) {
	_, err := s.db.Exec(
		`UPDATE session_notes
		 SET body = ?, updated_at = strftime('%s','now'),
		     actionables_scanned_at = NULL, has_actionables = 0, actionables_scan_version = 0
		 WHERE id = ?`,
		body, id,
	)
	if err != nil {
		return SessionNote{}, err
	}
	return s.GetSessionNote(id)
}

// SetSessionNoteMeta stores the LLM-generated title and emoji for a note.
func (s *Store) SetSessionNoteMeta(id int64, title, emoji string) (SessionNote, error) {
	_, err := s.db.Exec(
		`UPDATE session_notes SET title = ?, emoji = ? WHERE id = ?`,
		title, emoji, id,
	)
	if err != nil {
		return SessionNote{}, err
	}
	return s.GetSessionNote(id)
}

// SetSessionNoteActionableScan stores the result of an actionable-item scan.
func (s *Store) SetSessionNoteActionableScan(id int64, hasActionables bool) (SessionNote, error) {
	has := 0
	if hasActionables {
		has = 1
	}
	_, err := s.db.Exec(
		`UPDATE session_notes
		 SET has_actionables = ?, actionables_scanned_at = strftime('%s','now'),
		     actionables_scan_version = ?
		 WHERE id = ?`,
		has, llm.ActionablesScanVersion, id,
	)
	if err != nil {
		return SessionNote{}, err
	}
	return s.GetSessionNote(id)
}

// DeleteSessionNote permanently removes a note.
func (s *Store) DeleteSessionNote(id int64) error {
	_, err := s.db.Exec(`DELETE FROM session_notes WHERE id = ?`, id)
	return err
}

// ListSessionNotes returns all notes for a session, newest first.
func (s *Store) ListSessionNotes(sessionID int64) ([]SessionNote, error) {
	rows, err := s.db.Query(
		`SELECT `+sessionNoteCols+` FROM session_notes
		 WHERE session_id = ? ORDER BY created_at DESC, id DESC`, sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionNote
	for rows.Next() {
		n, err := scanSessionNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ListAllNotes returns notes across every session, newest first, with session context.
func (s *Store) ListAllNotes(limit int) ([]GlobalNote, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`
		SELECT sn.id, sn.session_id, sn.body, sn.title, sn.emoji,
		       sn.created_at, sn.updated_at, sn.actionables_scanned_at, sn.has_actionables,
		       sn.actionables_scan_version,
		       s.started_at,
		       COALESCE(t.title, ''), COALESCE(p.name, '')
		FROM session_notes sn
		JOIN sessions s ON s.id = sn.session_id
		LEFT JOIN tasks t ON t.id = s.task_id
		LEFT JOIN projects p ON p.id = t.project_id
		ORDER BY sn.created_at DESC, sn.id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GlobalNote
	for rows.Next() {
		var n GlobalNote
		var created, updated int64
		var scanned sql.NullInt64
		var started int64
		if err := rows.Scan(
			&n.ID, &n.SessionID, &n.Body, &n.Title, &n.Emoji,
			&created, &updated, &scanned, &n.HasActionables, &n.ActionablesScanVersion,
			&started, &n.TaskTitle, &n.ProjectName,
		); err != nil {
			return nil, err
		}
		n.CreatedAt = toTime(created)
		n.UpdatedAt = toTime(updated)
		n.ActionablesScannedAt = toTimePtr(scanned)
		n.SessionStarted = toTime(started)
		out = append(out, n)
	}
	return out, rows.Err()
}

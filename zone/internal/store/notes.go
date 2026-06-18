package store

import "time"

// SessionNote is a free-form note taken during a focus session.
type SessionNote struct {
	ID        int64
	SessionID int64
	Body      string
	Title     string // LLM-generated short label
	Emoji     string // LLM-generated emoji
	CreatedAt time.Time
}

// AddSessionNote appends a note to a focus session.
func (s *Store) AddSessionNote(sessionID int64, body string) (SessionNote, error) {
	res, err := s.db.Exec(
		`INSERT INTO session_notes (session_id, body) VALUES (?, ?)`,
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
	var n SessionNote
	var created int64
	err := s.db.QueryRow(
		`SELECT id, session_id, body, title, emoji, created_at FROM session_notes WHERE id = ?`, id,
	).Scan(&n.ID, &n.SessionID, &n.Body, &n.Title, &n.Emoji, &created)
	if err != nil {
		return SessionNote{}, err
	}
	n.CreatedAt = toTime(created)
	return n, nil
}

// UpdateSessionNote replaces the body of an existing note. Generated title/emoji
// are kept so edits do not trigger re-labeling.
func (s *Store) UpdateSessionNote(id int64, body string) (SessionNote, error) {
	_, err := s.db.Exec(
		`UPDATE session_notes SET body = ? WHERE id = ?`,
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

// ListSessionNotes returns all notes for a session, newest first.
func (s *Store) ListSessionNotes(sessionID int64) ([]SessionNote, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, body, title, emoji, created_at FROM session_notes
		 WHERE session_id = ? ORDER BY created_at DESC, id DESC`, sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionNote
	for rows.Next() {
		var n SessionNote
		var created int64
		if err := rows.Scan(
			&n.ID, &n.SessionID, &n.Body, &n.Title, &n.Emoji, &created,
		); err != nil {
			return nil, err
		}
		n.CreatedAt = toTime(created)
		out = append(out, n)
	}
	return out, rows.Err()
}

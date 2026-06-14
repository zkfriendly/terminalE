package store

import "time"

// Entry kinds.
const (
	KindWork  = "work"
	KindBreak = "break"
)

// StartEntry opens a live (open-ended) entry, e.g. standalone tracking. The
// returned id is closed later with EndEntry.
func (s *Store) StartEntry(taskID int64, sessionID *int64, kind string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO entries (task_id, session_id, kind, started_at) VALUES (?, ?, ?, ?)`,
		taskID, nullableID(sessionID), kind, unix(time.Now()),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// EndEntry closes a live entry at the current time.
func (s *Store) EndEntry(id int64) error {
	_, err := s.db.Exec(`UPDATE entries SET ended_at = ? WHERE id = ?`, unix(time.Now()), id)
	return err
}

// AddEntry records an already-completed entry with explicit start/end times.
// Used by the focus engine when a block finishes (duration is authoritative).
func (s *Store) AddEntry(taskID int64, sessionID *int64, kind string, start, end time.Time) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO entries (task_id, session_id, kind, started_at, ended_at) VALUES (?, ?, ?, ?, ?)`,
		taskID, nullableID(sessionID), kind, unix(start), unix(end),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// TaskWorkSeconds returns total completed work-seconds per task id.
func (s *Store) TaskWorkSeconds() (map[int64]int, error) {
	rows, err := s.db.Query(`
		SELECT task_id, COALESCE(SUM(ended_at - started_at), 0)
		FROM entries
		WHERE kind = 'work' AND ended_at IS NOT NULL
		GROUP BY task_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]int)
	for rows.Next() {
		var id int64
		var sec int
		if err := rows.Scan(&id, &sec); err != nil {
			return nil, err
		}
		out[id] = sec
	}
	return out, rows.Err()
}

func nullableID(id *int64) any {
	if id == nil {
		return nil
	}
	return *id
}

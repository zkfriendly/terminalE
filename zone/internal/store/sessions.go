package store

import (
	"database/sql"
	"time"
)

// Session statuses.
const (
	SessionActive    = "active"
	SessionCompleted = "completed"
	SessionAbandoned = "abandoned"
)

// CreateSession opens a new focus session for a task.
func (s *Store) CreateSession(taskID int64, workSec, breakSec, totalSec, prepareSec int) (Session, error) {
	res, err := s.db.Exec(
		`INSERT INTO sessions (task_id, work_sec, break_sec, total_sec, prepare_sec, status, cur_phase, cur_remaining)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		taskID, workSec, breakSec, totalSec, prepareSec, SessionActive, "prepare", 0,
	)
	if err != nil {
		return Session{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetSession(id)
}

// GetSession fetches a session by id.
func (s *Store) GetSession(id int64) (Session, error) {
	var sess Session
	var started int64
	var ended sql.NullInt64
	err := s.db.QueryRow(`
		SELECT id, task_id, work_sec, break_sec, total_sec, prepare_sec, started_at, ended_at, status
		FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &sess.TaskID, &sess.WorkSec, &sess.BreakSec, &sess.TotalSec, &sess.PrepareSec, &started, &ended, &sess.Status)
	if err != nil {
		return Session{}, err
	}
	sess.StartedAt = toTime(started)
	sess.EndedAt = toTimePtr(ended)
	return sess, nil
}

// Runtime is the persisted live state of a focus session, used to resume it.
type Runtime struct {
	Phase     string
	Remaining int
	Cycle     int
	Accrued   int
	Running   bool
}

// ActiveSession returns the most recent session whose status is still active.
func (s *Store) ActiveSession() (Session, bool, error) {
	var id int64
	err := s.db.QueryRow(
		`SELECT id FROM sessions WHERE status = ? ORDER BY started_at DESC LIMIT 1`,
		SessionActive,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	sess, err := s.GetSession(id)
	if err != nil {
		return Session{}, false, err
	}
	return sess, true, nil
}

// SaveRuntime persists the live engine state for a session.
func (s *Store) SaveRuntime(id int64, rt Runtime) error {
	_, err := s.db.Exec(`
		UPDATE sessions
		SET cur_phase = ?, cur_remaining = ?, cur_cycle = ?, accrued_sec = ?, running = ?, updated_at = ?
		WHERE id = ?`,
		rt.Phase, rt.Remaining, rt.Cycle, rt.Accrued, rt.Running, time.Now().Unix(), id,
	)
	return err
}

// LoadRuntime reads the persisted live engine state for a session.
func (s *Store) LoadRuntime(id int64) (Runtime, error) {
	var rt Runtime
	err := s.db.QueryRow(
		`SELECT cur_phase, cur_remaining, cur_cycle, accrued_sec, running FROM sessions WHERE id = ?`, id,
	).Scan(&rt.Phase, &rt.Remaining, &rt.Cycle, &rt.Accrued, &rt.Running)
	return rt, err
}

// EndSession closes a session with the given terminal status.
func (s *Store) EndSession(id int64, status string) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET ended_at = ?, status = ? WHERE id = ?`,
		unix(time.Now()), status, id,
	)
	return err
}

// RecentSessions returns the most recent sessions joined with task/project names.
func (s *Store) RecentSessions(limit int) ([]SessionSummary, error) {
	rows, err := s.db.Query(`
		SELECT s.id, s.started_at, s.ended_at, s.status, s.work_sec, s.break_sec, s.total_sec,
		       t.title, p.name, p.color,
		       COALESCE((SELECT SUM(e.ended_at - e.started_at) FROM entries e
		                 WHERE e.session_id = s.id AND e.kind = 'work' AND e.ended_at IS NOT NULL), 0)
		FROM sessions s
		JOIN tasks t    ON t.id = s.task_id
		JOIN projects p ON p.id = t.project_id
		ORDER BY s.started_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionSummary
	for rows.Next() {
		var ss SessionSummary
		var started int64
		var ended sql.NullInt64
		if err := rows.Scan(
			&ss.ID, &started, &ended, &ss.Status, &ss.WorkSec, &ss.BreakSec, &ss.TotalSec,
			&ss.TaskTitle, &ss.ProjectName, &ss.ProjectColor, &ss.WorkedSec,
		); err != nil {
			return nil, err
		}
		ss.StartedAt = toTime(started)
		ss.EndedAt = toTimePtr(ended)
		out = append(out, ss)
	}
	return out, rows.Err()
}

// SessionSummary is a session enriched for display.
type SessionSummary struct {
	ID           int64
	StartedAt    time.Time
	EndedAt      *time.Time
	Status       string
	WorkSec      int
	BreakSec     int
	TotalSec     int
	WorkedSec    int
	TaskTitle    string
	ProjectName  string
	ProjectColor string
}

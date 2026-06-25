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

// CreateSession opens a new general focus session. taskID is the optional
// starting "current" task (nil = start with no task).
func (s *Store) CreateSession(taskID *int64, workSec, breakSec, totalSec, prepareSec int) (Session, error) {
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

// SetSessionTask changes a session's current task (nil clears it).
func (s *Store) SetSessionTask(id int64, taskID *int64) error {
	_, err := s.db.Exec(`UPDATE sessions SET task_id = ? WHERE id = ?`, taskID, id)
	return err
}

// GetSession fetches a session by id.
func (s *Store) GetSession(id int64) (Session, error) {
	var sess Session
	var started int64
	var ended, taskID sql.NullInt64
	err := s.db.QueryRow(`
		SELECT id, task_id, work_sec, break_sec, total_sec, prepare_sec, started_at, ended_at, status
		FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &taskID, &sess.WorkSec, &sess.BreakSec, &sess.TotalSec, &sess.PrepareSec, &started, &ended, &sess.Status)
	if err != nil {
		return Session{}, err
	}
	sess.TaskID = toInt64Ptr(taskID)
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
	// SegCredited is how many seconds of the current work block have already been
	// written as entries. Persisting it lets a restarted daemon resume crediting
	// from where it left off instead of re-recording the whole block (which would
	// create overlapping entries and inflate focus time).
	SegCredited int
	Running     bool
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

// LastEndedSession returns the most recently ended (non-active) session, if any.
// Useful for offering to resume a session that was ended early.
func (s *Store) LastEndedSession() (Session, bool, error) {
	var id int64
	err := s.db.QueryRow(
		`SELECT id FROM sessions WHERE status != ? ORDER BY started_at DESC LIMIT 1`,
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

// ReopenSession marks a previously ended session active again and clears its end
// time, so the daemon can resume it from its persisted runtime.
func (s *Store) ReopenSession(id int64) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET status = ?, ended_at = NULL WHERE id = ?`,
		SessionActive, id,
	)
	return err
}

// SaveRuntime persists the live engine state for a session.
func (s *Store) SaveRuntime(id int64, rt Runtime) error {
	_, err := s.db.Exec(`
		UPDATE sessions
		SET cur_phase = ?, cur_remaining = ?, cur_cycle = ?, accrued_sec = ?,
		    seg_credited = ?, running = ?, updated_at = ?
		WHERE id = ?`,
		rt.Phase, rt.Remaining, rt.Cycle, rt.Accrued, rt.SegCredited, rt.Running, time.Now().Unix(), id,
	)
	return err
}

// LoadRuntime reads the persisted live engine state for a session.
func (s *Store) LoadRuntime(id int64) (Runtime, error) {
	var rt Runtime
	err := s.db.QueryRow(
		`SELECT cur_phase, cur_remaining, cur_cycle, accrued_sec, seg_credited, running
		 FROM sessions WHERE id = ?`, id,
	).Scan(&rt.Phase, &rt.Remaining, &rt.Cycle, &rt.Accrued, &rt.SegCredited, &rt.Running)
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
		       COALESCE(t.id, 0), COALESCE(t.title, ''), COALESCE(p.color, ''),
		       COALESCE((SELECT SUM(e.ended_at - e.started_at) FROM entries e
		                 WHERE e.session_id = s.id AND e.kind = 'work' AND e.ended_at IS NOT NULL), 0),
		       COALESCE(s.ended_at, strftime('%s','now')) - s.started_at,
		       COALESCE((SELECT COUNT(*) FROM session_notes sn WHERE sn.session_id = s.id), 0)
		FROM sessions s
		LEFT JOIN tasks t    ON t.id = s.task_id
		LEFT JOIN projects p ON p.id = t.project_id
		ORDER BY s.started_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionSummary
	var taskIDs []int64
	for rows.Next() {
		var ss SessionSummary
		var started int64
		var taskID int64
		var ended sql.NullInt64
		if err := rows.Scan(
			&ss.ID, &started, &ended, &ss.Status, &ss.WorkSec, &ss.BreakSec, &ss.TotalSec,
			&taskID, &ss.TaskTitle, &ss.ProjectColor, &ss.WorkedSec, &ss.WallSec, &ss.NoteCount,
		); err != nil {
			return nil, err
		}
		ss.StartedAt = toTime(started)
		ss.EndedAt = toTimePtr(ended)
		out = append(out, ss)
		taskIDs = append(taskIDs, taskID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i, taskID := range taskIDs {
		if taskID != 0 {
			out[i].ProjectName = s.taskParentPath(taskID)
		}
	}
	return out, nil
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
	WorkedSec    int // active focus time (work entries), excludes pauses/breaks
	WallSec      int // wall-clock time from start to end (or now), includes everything
	NoteCount    int
	TaskTitle    string
	ProjectName  string
	ProjectColor string
}

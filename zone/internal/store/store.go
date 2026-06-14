// Package store provides typed access to zone's data: projects, tasks, focus
// sessions, time entries, and aggregate stats. All durations are seconds and all
// timestamps are time.Time (converted to/from unix epoch seconds on the wire).
package store

import (
	"database/sql"
	"time"
)

// Store wraps a *sql.DB with domain methods.
type Store struct {
	db *sql.DB
}

// New returns a Store backed by db.
func New(db *sql.DB) *Store { return &Store{db: db} }

// Project is a top-level grouping of tasks.
type Project struct {
	ID        int64
	Name      string
	Color     string
	Archived  bool
	CreatedAt time.Time
}

// Task is a unit of work belonging to a project.
type Task struct {
	ID          int64
	ProjectID   int64
	Title       string
	Status      string
	Archived    bool
	CreatedAt   time.Time
	ProjectName string // populated by joins where convenient
}

// Session is a pomodoro focus session targeting a task.
type Session struct {
	ID         int64
	TaskID     int64
	WorkSec    int
	BreakSec   int
	TotalSec   int
	PrepareSec int
	StartedAt  time.Time
	EndedAt    *time.Time
	Status     string // active | completed | abandoned
}

// Entry is a tracked span of time (work or break) optionally tied to a session.
type Entry struct {
	ID        int64
	TaskID    int64
	SessionID *int64
	Kind      string // work | break
	StartedAt time.Time
	EndedAt   *time.Time
	Note      string
}

func unix(t time.Time) int64 { return t.Unix() }

func toTime(sec int64) time.Time { return time.Unix(sec, 0) }

func toTimePtr(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := toTime(n.Int64)
	return &t
}

func toInt64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

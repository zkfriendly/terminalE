package store

import "time"

// WorkSecondsSince returns total completed work-seconds since t (inclusive).
func (s *Store) WorkSecondsSince(t time.Time) (int, error) {
	var sec int
	err := s.db.QueryRow(`
		SELECT COALESCE(SUM(ended_at - started_at), 0)
		FROM entries
		WHERE kind = 'work' AND ended_at IS NOT NULL AND started_at >= ?`,
		unix(t),
	).Scan(&sec)
	return sec, err
}

// TodayWorkSeconds returns completed work-seconds since local midnight.
func (s *Store) TodayWorkSeconds() (int, error) {
	return s.WorkSecondsSince(startOfDay(time.Now()))
}

// ProjectStat is a per-project aggregate of work time.
type ProjectStat struct {
	ProjectID   int64
	ProjectName string
	Color       string
	WorkSec     int
}

// ProjectBreakdown returns work-seconds per project since t, busiest first.
func (s *Store) ProjectBreakdown(t time.Time) ([]ProjectStat, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.name, p.color, COALESCE(SUM(e.ended_at - e.started_at), 0) AS worked
		FROM entries e
		JOIN tasks t    ON t.id = e.task_id
		JOIN projects p ON p.id = t.project_id
		WHERE e.kind = 'work' AND e.ended_at IS NOT NULL AND e.started_at >= ?
		GROUP BY p.id
		HAVING worked > 0
		ORDER BY worked DESC`, unix(t))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProjectStat
	for rows.Next() {
		var ps ProjectStat
		if err := rows.Scan(&ps.ProjectID, &ps.ProjectName, &ps.Color, &ps.WorkSec); err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, rows.Err()
}

// TaskStat is a per-task aggregate of work time.
type TaskStat struct {
	TaskID      int64
	TaskTitle   string
	ProjectName string
	Color       string
	WorkSec     int
}

// TaskBreakdown returns work-seconds per task since t, busiest first, capped at limit.
func (s *Store) TaskBreakdown(t time.Time, limit int) ([]TaskStat, error) {
	rows, err := s.db.Query(`
		SELECT tk.id, tk.title, p.name, p.color, COALESCE(SUM(e.ended_at - e.started_at), 0) AS worked
		FROM entries e
		JOIN tasks tk   ON tk.id = e.task_id
		JOIN projects p ON p.id = tk.project_id
		WHERE e.kind = 'work' AND e.ended_at IS NOT NULL AND e.started_at >= ?
		GROUP BY tk.id
		HAVING worked > 0
		ORDER BY worked DESC
		LIMIT ?`, unix(t), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TaskStat
	var taskIDs []int64
	for rows.Next() {
		var ts TaskStat
		if err := rows.Scan(&ts.TaskID, &ts.TaskTitle, &ts.ProjectName, &ts.Color, &ts.WorkSec); err != nil {
			return nil, err
		}
		out = append(out, ts)
		taskIDs = append(taskIDs, ts.TaskID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i, taskID := range taskIDs {
		out[i].ProjectName = s.taskParentPath(taskID)
	}
	return out, nil
}

// Streak returns the number of consecutive days (ending today or yesterday) that
// have at least minSeconds of completed work. A gap before today still counts the
// run if it ended yesterday, so an in-progress day with no time yet doesn't reset.
func (s *Store) Streak(minSeconds int) (int, error) {
	// Pull per-day work totals (local days) for the last ~370 days.
	since := startOfDay(time.Now().AddDate(0, 0, -370))
	rows, err := s.db.Query(`
		SELECT started_at, ended_at FROM entries
		WHERE kind = 'work' AND ended_at IS NOT NULL AND started_at >= ?`, unix(since))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	perDay := make(map[string]int)
	for rows.Next() {
		var st, en int64
		if err := rows.Scan(&st, &en); err != nil {
			return 0, err
		}
		day := startOfDay(toTime(st)).Format("2006-01-02")
		perDay[day] += int(en - st)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	count := 0
	cursor := startOfDay(time.Now())
	// Allow today to be empty: if today has no qualifying time, start counting from yesterday.
	if perDay[cursor.Format("2006-01-02")] < minSeconds {
		cursor = cursor.AddDate(0, 0, -1)
	}
	for {
		if perDay[cursor.Format("2006-01-02")] >= minSeconds {
			count++
			cursor = cursor.AddDate(0, 0, -1)
			continue
		}
		break
	}
	return count, nil
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

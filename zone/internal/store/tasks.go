package store

import (
	"database/sql"
	"strings"
)

// CreateRootTask inserts a top-level task.
func (s *Store) CreateRootTask(title string) (Task, error) {
	p, err := s.CreateProject(title, "")
	if err != nil {
		return Task{}, err
	}
	return s.GetTask(p.RootTaskID)
}

// CreateChildTask inserts a task below another task.
func (s *Store) CreateChildTask(parentID int64, title string) (Task, error) {
	parent, err := s.GetTask(parentID)
	if err != nil {
		return Task{}, err
	}
	res, err := s.db.Exec(
		`INSERT INTO tasks (project_id, parent_id, title) VALUES (?, ?, ?)`,
		parent.ProjectID, parentID, title,
	)
	if err != nil {
		return Task{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetTask(id)
}

// CreateTask inserts a task under a project's root task. Kept for compatibility.
func (s *Store) CreateTask(projectID int64, title string) (Task, error) {
	p, err := s.GetProject(projectID)
	if err != nil {
		return Task{}, err
	}
	if p.RootTaskID == 0 {
		return Task{}, sql.ErrNoRows
	}
	return s.CreateChildTask(p.RootTaskID, title)
}

// GetTask fetches a single task by id.
func (s *Store) GetTask(id int64) (Task, error) {
	row := s.db.QueryRow(`
		SELECT t.id, t.project_id, t.parent_id, t.title, t.status, t.archived,
		       t.created_at, p.color
		FROM tasks t JOIN projects p ON p.id = t.project_id
		WHERE t.id = ?`, id)
	t, err := s.scanTask(row)
	if err != nil {
		return Task{}, err
	}
	s.enrichTask(&t)
	return t, nil
}

// ListTasks returns the direct children of a project's root task.
func (s *Store) ListTasks(projectID int64, includeArchived bool) ([]Task, error) {
	p, err := s.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	if p.RootTaskID == 0 {
		return nil, nil
	}
	return s.ListChildTasks(p.RootTaskID, includeArchived)
}

// ListChildTasks returns direct children of a task.
func (s *Store) ListChildTasks(parentID int64, includeArchived bool) ([]Task, error) {
	q := `
		SELECT t.id, t.project_id, t.parent_id, t.title, t.status, t.archived,
		       t.created_at, p.color
		FROM tasks t JOIN projects p ON p.id = t.project_id
		WHERE t.parent_id = ?`
	if !includeArchived {
		q += ` AND t.archived = 0`
	}
	q += ` ORDER BY t.archived ASC, t.id ASC`
	return s.scanTasks(q, parentID)
}

// ListAllTasks returns every task in tree order. Used by the in-session task switcher.
func (s *Store) ListAllTasks(includeArchived bool) ([]Task, error) {
	return s.ListTaskTree(includeArchived)
}

// ListTaskTree returns all tasks depth-first, with Depth and ProjectName set for display.
func (s *Store) ListTaskTree(includeArchived bool) ([]Task, error) {
	q := `
		SELECT t.id, t.project_id, t.parent_id, t.title, t.status, t.archived,
		       t.created_at, p.color
		FROM tasks t JOIN projects p ON p.id = t.project_id`
	if !includeArchived {
		q += ` WHERE t.archived = 0 AND p.archived = 0`
	}
	q += ` ORDER BY t.id ASC`

	tasks, err := s.scanTasks(q)
	if err != nil {
		return nil, err
	}
	children := map[int64][]Task{}
	var roots []Task
	for _, t := range tasks {
		if t.ParentID == nil {
			roots = append(roots, t)
			continue
		}
		children[*t.ParentID] = append(children[*t.ParentID], t)
	}

	var out []Task
	var walk func(Task, int, []string)
	walk = func(t Task, depth int, parents []string) {
		t.Depth = depth
		t.ProjectName = strings.Join(parents, " / ")
		t.HasChildren = len(children[t.ID]) > 0
		out = append(out, t)
		nextParents := append(append([]string{}, parents...), t.Title)
		for _, child := range children[t.ID] {
			walk(child, depth+1, nextParents)
		}
	}
	for _, root := range roots {
		walk(root, 0, nil)
	}
	return out, nil
}

// RenameTask updates a task's title.
func (s *Store) RenameTask(id int64, title string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE tasks SET title = ? WHERE id = ?`, title, id); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`UPDATE projects SET name = ? WHERE root_task_id = ?`, title, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// SetTaskStatus updates a task's status (e.g. open, done).
func (s *Store) SetTaskStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE tasks SET status = ? WHERE id = ?`, status, id)
	return err
}

// SetTaskArchived toggles a task's archived flag. Descendants follow the parent.
func (s *Store) SetTaskArchived(id int64, archived bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
		WITH RECURSIVE subtree(id) AS (
			SELECT id FROM tasks WHERE id = ?
			UNION ALL
			SELECT t.id FROM tasks t JOIN subtree s ON t.parent_id = s.id
		)
		UPDATE tasks SET archived = ? WHERE id IN (SELECT id FROM subtree)`,
		id, archived,
	); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`UPDATE projects SET archived = ? WHERE root_task_id = ?`, archived, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) scanTasks(q string, args ...any) ([]Task, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}

	var out []Task
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		s.enrichTask(&out[i])
	}
	return out, nil
}

func (s *Store) scanTask(row interface {
	Scan(dest ...any) error
}) (Task, error) {
	var t Task
	var created int64
	var parent sql.NullInt64
	if err := row.Scan(
		&t.ID, &t.ProjectID, &parent, &t.Title, &t.Status, &t.Archived,
		&created, &t.Color,
	); err != nil {
		return Task{}, err
	}
	t.ParentID = toInt64Ptr(parent)
	t.CreatedAt = toTime(created)
	return t, nil
}

func (s *Store) enrichTask(t *Task) {
	t.ProjectName = s.taskParentPath(t.ID)
	var childCount int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE parent_id = ? AND archived = 0`, t.ID).Scan(&childCount)
	t.HasChildren = childCount > 0
}

func (s *Store) taskParentPath(id int64) string {
	rows, err := s.db.Query(`
		WITH RECURSIVE ancestors(id, parent_id, title, depth) AS (
			SELECT p.id, p.parent_id, p.title, 0
			FROM tasks child JOIN tasks p ON p.id = child.parent_id
			WHERE child.id = ?
			UNION ALL
			SELECT p.id, p.parent_id, p.title, ancestors.depth + 1
			FROM tasks p JOIN ancestors ON p.id = ancestors.parent_id
		)
		SELECT title FROM ancestors ORDER BY depth DESC`, id)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err == nil {
			parts = append(parts, title)
		}
	}
	return strings.Join(parts, " / ")
}

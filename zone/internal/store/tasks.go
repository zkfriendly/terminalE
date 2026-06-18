package store

// CreateTask inserts a task under a project.
func (s *Store) CreateTask(projectID int64, title string) (Task, error) {
	res, err := s.db.Exec(`INSERT INTO tasks (project_id, title) VALUES (?, ?)`, projectID, title)
	if err != nil {
		return Task{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetTask(id)
}

// GetTask fetches a single task (with its project name) by id.
func (s *Store) GetTask(id int64) (Task, error) {
	var t Task
	var created int64
	err := s.db.QueryRow(`
		SELECT t.id, t.project_id, t.title, t.status, t.archived, t.created_at, p.name
		FROM tasks t JOIN projects p ON p.id = t.project_id
		WHERE t.id = ?`, id,
	).Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.Archived, &created, &t.ProjectName)
	if err != nil {
		return Task{}, err
	}
	t.CreatedAt = toTime(created)
	return t, nil
}

// ListTasks returns the tasks of a project, optionally including archived ones.
func (s *Store) ListTasks(projectID int64, includeArchived bool) ([]Task, error) {
	q := `
		SELECT t.id, t.project_id, t.title, t.status, t.archived, t.created_at, p.name
		FROM tasks t JOIN projects p ON p.id = t.project_id
		WHERE t.project_id = ?`
	if !includeArchived {
		q += ` AND t.archived = 0`
	}
	q += ` ORDER BY t.archived ASC, t.id ASC`

	rows, err := s.db.Query(q, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Task
	for rows.Next() {
		var t Task
		var created int64
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.Archived, &created, &t.ProjectName); err != nil {
			return nil, err
		}
		t.CreatedAt = toTime(created)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListAllTasks returns every task across all projects (with project names),
// ordered by project then task. Used by the in-session task switcher.
func (s *Store) ListAllTasks(includeArchived bool) ([]Task, error) {
	q := `
		SELECT t.id, t.project_id, t.title, t.status, t.archived, t.created_at, p.name
		FROM tasks t JOIN projects p ON p.id = t.project_id`
	if !includeArchived {
		q += ` WHERE t.archived = 0 AND p.archived = 0`
	}
	q += ` ORDER BY p.name ASC, t.id ASC`

	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Task
	for rows.Next() {
		var t Task
		var created int64
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.Archived, &created, &t.ProjectName); err != nil {
			return nil, err
		}
		t.CreatedAt = toTime(created)
		out = append(out, t)
	}
	return out, rows.Err()
}

// RenameTask updates a task's title.
func (s *Store) RenameTask(id int64, title string) error {
	_, err := s.db.Exec(`UPDATE tasks SET title = ? WHERE id = ?`, title, id)
	return err
}

// SetTaskStatus updates a task's status (e.g. open, done).
func (s *Store) SetTaskStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE tasks SET status = ? WHERE id = ?`, status, id)
	return err
}

// SetTaskArchived toggles a task's archived flag.
func (s *Store) SetTaskArchived(id int64, archived bool) error {
	_, err := s.db.Exec(`UPDATE tasks SET archived = ? WHERE id = ?`, archived, id)
	return err
}

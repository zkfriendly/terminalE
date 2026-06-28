package store

// projectPalette is cycled through when creating projects without an explicit color.
var projectPalette = []string{
	"#7aa2f7", "#9ece6a", "#e0af68", "#bb9af7",
	"#f7768e", "#7dcfff", "#ff9e64", "#73daca",
}

// CreateProject inserts a root task. Project rows are kept as the internal
// container so existing sessions, stats, and colors migrate cleanly.
func (s *Store) CreateProject(name, color string) (Project, error) {
	if color == "" {
		var count int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&count)
		color = projectPalette[count%len(projectPalette)]
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Project{}, err
	}
	res, err := tx.Exec(`INSERT INTO projects (name, color) VALUES (?, ?)`, name, color)
	if err != nil {
		tx.Rollback()
		return Project{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		tx.Rollback()
		return Project{}, err
	}
	taskRes, err := tx.Exec(`INSERT INTO tasks (project_id, parent_id, title) VALUES (?, NULL, ?)`, id, name)
	if err != nil {
		tx.Rollback()
		return Project{}, err
	}
	rootID, err := taskRes.LastInsertId()
	if err != nil {
		tx.Rollback()
		return Project{}, err
	}
	if _, err := tx.Exec(`UPDATE projects SET root_task_id = ? WHERE id = ?`, rootID, id); err != nil {
		tx.Rollback()
		return Project{}, err
	}
	if err := tx.Commit(); err != nil {
		return Project{}, err
	}
	return s.GetProject(id)
}

// GetProject fetches a single project by id.
func (s *Store) GetProject(id int64) (Project, error) {
	var p Project
	var created int64
	err := s.db.QueryRow(
		`SELECT id, name, color, archived, created_at, COALESCE(root_task_id, 0) FROM projects WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.Color, &p.Archived, &created, &p.RootTaskID)
	if err != nil {
		return Project{}, err
	}
	p.CreatedAt = toTime(created)
	return p, nil
}

// ListProjects returns projects ordered by creation, optionally including archived ones.
func (s *Store) ListProjects(includeArchived bool) ([]Project, error) {
	q := `SELECT id, name, color, archived, created_at, COALESCE(root_task_id, 0) FROM projects`
	if !includeArchived {
		q += ` WHERE archived = 0`
	}
	q += ` ORDER BY archived ASC, id ASC`

	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Project
	for rows.Next() {
		var p Project
		var created int64
		if err := rows.Scan(&p.ID, &p.Name, &p.Color, &p.Archived, &created, &p.RootTaskID); err != nil {
			return nil, err
		}
		p.CreatedAt = toTime(created)
		out = append(out, p)
	}
	return out, rows.Err()
}

// RenameProject updates a project's name.
func (s *Store) RenameProject(id int64, name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE projects SET name = ? WHERE id = ?`, name, id); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`UPDATE tasks SET title = ? WHERE id = (SELECT root_task_id FROM projects WHERE id = ?)`, name, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// SetProjectArchived toggles a project's archived flag.
func (s *Store) SetProjectArchived(id int64, archived bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE projects SET archived = ? WHERE id = ?`, archived, id); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`UPDATE tasks SET archived = ? WHERE id = (SELECT root_task_id FROM projects WHERE id = ?)`, archived, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

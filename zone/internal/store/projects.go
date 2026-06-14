package store

// projectPalette is cycled through when creating projects without an explicit color.
var projectPalette = []string{
	"#7aa2f7", "#9ece6a", "#e0af68", "#bb9af7",
	"#f7768e", "#7dcfff", "#ff9e64", "#73daca",
}

// CreateProject inserts a project. If color is empty, one is picked from the palette.
func (s *Store) CreateProject(name, color string) (Project, error) {
	if color == "" {
		var count int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&count)
		color = projectPalette[count%len(projectPalette)]
	}
	res, err := s.db.Exec(`INSERT INTO projects (name, color) VALUES (?, ?)`, name, color)
	if err != nil {
		return Project{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetProject(id)
}

// GetProject fetches a single project by id.
func (s *Store) GetProject(id int64) (Project, error) {
	var p Project
	var created int64
	err := s.db.QueryRow(
		`SELECT id, name, color, archived, created_at FROM projects WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.Color, &p.Archived, &created)
	if err != nil {
		return Project{}, err
	}
	p.CreatedAt = toTime(created)
	return p, nil
}

// ListProjects returns projects ordered by creation, optionally including archived ones.
func (s *Store) ListProjects(includeArchived bool) ([]Project, error) {
	q := `SELECT id, name, color, archived, created_at FROM projects`
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
		if err := rows.Scan(&p.ID, &p.Name, &p.Color, &p.Archived, &created); err != nil {
			return nil, err
		}
		p.CreatedAt = toTime(created)
		out = append(out, p)
	}
	return out, rows.Err()
}

// RenameProject updates a project's name.
func (s *Store) RenameProject(id int64, name string) error {
	_, err := s.db.Exec(`UPDATE projects SET name = ? WHERE id = ?`, name, id)
	return err
}

// SetProjectArchived toggles a project's archived flag.
func (s *Store) SetProjectArchived(id int64, archived bool) error {
	_, err := s.db.Exec(`UPDATE projects SET archived = ? WHERE id = ?`, archived, id)
	return err
}

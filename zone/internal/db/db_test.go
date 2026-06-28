package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// legacySchema matches a pre-nested-tasks database before parent_id exists.
const legacySchema = `
CREATE TABLE projects (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    color      TEXT    NOT NULL DEFAULT '',
    archived   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
CREATE TABLE tasks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title      TEXT    NOT NULL,
    status     TEXT    NOT NULL DEFAULT 'open',
    archived   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
INSERT INTO projects (id, name) VALUES (1, 'alpha'), (2, 'beta');
INSERT INTO tasks (project_id, title) VALUES (1, 'child-a'), (1, 'child-b'), (2, 'solo');
`

func TestOpenMigratesLegacyNestedTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zone.db")

	raw, err := sql.Open("sqlite", fmt.Sprintf("file:%s", path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(legacySchema); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	raw.Close()

	database, err := Open(path)
	if err != nil {
		t.Fatalf("open legacy db for migration: %v", err)
	}
	defer database.Close()

	var roots, children, linked int
	if err := database.QueryRow(`SELECT COUNT(*) FROM tasks WHERE parent_id IS NULL`).Scan(&roots); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM tasks WHERE parent_id IS NOT NULL`).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM projects WHERE root_task_id IS NOT NULL`).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if roots != 2 {
		t.Fatalf("expected 2 root tasks (one per project), got %d", roots)
	}
	if children != 3 {
		t.Fatalf("expected 3 child tasks, got %d", children)
	}
	if linked != 2 {
		t.Fatalf("expected 2 projects linked to root tasks, got %d", linked)
	}

	var childA int64
	if err := database.QueryRow(
		`SELECT t.id FROM tasks t JOIN tasks root ON t.parent_id = root.id
		 JOIN projects p ON p.root_task_id = root.id
		 WHERE p.name = 'alpha' AND t.title = 'child-a'`,
	).Scan(&childA); err != nil {
		t.Fatalf("child-a not nested under alpha root: %v", err)
	}
}

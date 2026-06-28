// One-off helper: open the real zone database and apply pending migrations.
// Usage: go run ./cmd/migrate
package main

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/zkfriendly/zone/internal/db"
)

func main() {
	path, err := db.DefaultPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve db path:", err)
		os.Exit(1)
	}
	database, err := db.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := report(database); err != nil {
		fmt.Fprintln(os.Stderr, "verify:", err)
		os.Exit(1)
	}
	fmt.Println("migration ok:", path)
}

func report(database *sql.DB) error {
	type row struct {
		label string
		n     int
	}
	queries := []struct {
		label string
		q     string
	}{
		{"projects", `SELECT COUNT(*) FROM projects`},
		{"tasks", `SELECT COUNT(*) FROM tasks`},
		{"root tasks", `SELECT COUNT(*) FROM tasks WHERE parent_id IS NULL`},
		{"child tasks", `SELECT COUNT(*) FROM tasks WHERE parent_id IS NOT NULL`},
		{"projects with root_task_id", `SELECT COUNT(*) FROM projects WHERE root_task_id IS NOT NULL`},
		{"sessions", `SELECT COUNT(*) FROM sessions`},
		{"entries", `SELECT COUNT(*) FROM entries`},
		{"session_notes", `SELECT COUNT(*) FROM session_notes`},
	}
	for _, item := range queries {
		var n int
		if err := database.QueryRow(item.q).Scan(&n); err != nil {
			return fmt.Errorf("%s: %w", item.label, err)
		}
		fmt.Printf("  %-28s %d\n", item.label+":", n)
	}
	return nil
}

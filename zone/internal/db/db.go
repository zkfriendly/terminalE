// Package db opens the local SQLite database and applies the schema.
package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/zkfriendly/zone/internal/config"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// DefaultPath returns the on-disk location of the zone database.
func DefaultPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "zone.db"), nil
}

// Open opens (and migrates) the database at path. A path of ":memory:" yields an
// in-memory database, handy for tests.
func Open(path string) (*sql.DB, error) {
	dsn := path
	if path != ":memory:" {
		// Per-connection pragmas via the DSN so they apply to every pooled conn.
		dsn = fmt.Sprintf(
			"file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)",
			path,
		)
	}

	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// SQLite has a single writer; serialize through one connection to avoid
	// SQLITE_BUSY under contention.
	database.SetMaxOpenConns(1)

	if _, err := database.Exec(schema); err != nil {
		database.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(database); err != nil {
		database.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return database, nil
}

// migrate applies additive column changes for databases created by older versions.
// ALTER TABLE ADD COLUMN is run best-effort; "duplicate column" errors are ignored.
func migrate(database *sql.DB) error {
	alters := []string{
		`ALTER TABLE sessions ADD COLUMN prepare_sec INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN cur_phase TEXT NOT NULL DEFAULT 'work'`,
		`ALTER TABLE sessions ADD COLUMN cur_remaining INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN cur_cycle INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN accrued_sec INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN running INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE sessions ADD COLUMN updated_at INTEGER NOT NULL DEFAULT 0`,
	}
	for _, q := range alters {
		if _, err := database.Exec(q); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}

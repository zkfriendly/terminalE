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
		`ALTER TABLE session_notes ADD COLUMN title TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE session_notes ADD COLUMN emoji TEXT NOT NULL DEFAULT ''`,
	}
	for _, q := range alters {
		if _, err := database.Exec(q); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return makeSessionTaskNullable(database)
}

// makeSessionTaskNullable rebuilds the sessions table so task_id is nullable (a
// session is general; its task is the "current" task and can be cleared). Older
// databases created task_id as NOT NULL; this migrates them in place once.
func makeSessionTaskNullable(database *sql.DB) error {
	notNull, err := columnIsNotNull(database, "sessions", "task_id")
	if err != nil || !notNull {
		return err
	}

	// FKs must be off during a table rebuild, otherwise DROP TABLE fires the
	// ON DELETE actions of referencing rows (it would null out entries.session_id).
	if _, err := database.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer database.Exec(`PRAGMA foreign_keys=ON`)

	tx, err := database.Begin()
	if err != nil {
		return err
	}
	stmts := []string{
		`CREATE TABLE sessions_new (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id       INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
			work_sec      INTEGER NOT NULL,
			break_sec     INTEGER NOT NULL,
			total_sec     INTEGER NOT NULL,
			prepare_sec   INTEGER NOT NULL DEFAULT 0,
			started_at    INTEGER NOT NULL DEFAULT (strftime('%s','now')),
			ended_at      INTEGER,
			status        TEXT    NOT NULL DEFAULT 'active',
			cur_phase     TEXT    NOT NULL DEFAULT 'work',
			cur_remaining INTEGER NOT NULL DEFAULT 0,
			cur_cycle     INTEGER NOT NULL DEFAULT 0,
			accrued_sec   INTEGER NOT NULL DEFAULT 0,
			running       INTEGER NOT NULL DEFAULT 1,
			updated_at    INTEGER NOT NULL DEFAULT 0
		)`,
		`INSERT INTO sessions_new
			SELECT id, task_id, work_sec, break_sec, total_sec, prepare_sec,
			       started_at, ended_at, status, cur_phase, cur_remaining,
			       cur_cycle, accrued_sec, running, updated_at
			FROM sessions`,
		`DROP TABLE sessions`,
		`ALTER TABLE sessions_new RENAME TO sessions`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_task ON sessions(task_id)`,
	}
	for _, q := range stmts {
		if _, err := tx.Exec(q); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// columnIsNotNull reports whether a column is declared NOT NULL.
func columnIsNotNull(database *sql.DB, table, column string) (bool, error) {
	rows, err := database.Query(`SELECT name, "notnull" FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var notnull int
		if err := rows.Scan(&name, &notnull); err != nil {
			return false, err
		}
		if name == column {
			return notnull == 1, nil
		}
	}
	return false, rows.Err()
}

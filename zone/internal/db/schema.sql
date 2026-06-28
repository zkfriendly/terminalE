-- zone schema. All timestamps are unix epoch seconds (INTEGER).

CREATE TABLE IF NOT EXISTS projects (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    color      TEXT    NOT NULL DEFAULT '',
    archived   INTEGER NOT NULL DEFAULT 0,
    root_task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE TABLE IF NOT EXISTS tasks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    parent_id  INTEGER REFERENCES tasks(id) ON DELETE CASCADE,
    title      TEXT    NOT NULL,
    status     TEXT    NOT NULL DEFAULT 'open',
    archived   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks(project_id);
CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_id);

CREATE TABLE IF NOT EXISTS sessions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    -- The session's *current* task (nullable: a session is general, and the user
    -- can switch tasks during it). Time is attributed to whichever task is current.
    task_id       INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
    work_sec      INTEGER NOT NULL,
    break_sec     INTEGER NOT NULL,
    total_sec     INTEGER NOT NULL,
    prepare_sec   INTEGER NOT NULL DEFAULT 0,
    started_at    INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    ended_at      INTEGER,
    status        TEXT    NOT NULL DEFAULT 'active',
    -- Runtime state, persisted by the focus daemon so a session can be resumed.
    cur_phase     TEXT    NOT NULL DEFAULT 'work',
    cur_remaining INTEGER NOT NULL DEFAULT 0,
    cur_cycle     INTEGER NOT NULL DEFAULT 0,
    accrued_sec   INTEGER NOT NULL DEFAULT 0,
    -- Seconds of the current work block already written as entries, so a restarted
    -- daemon resumes crediting instead of re-recording the whole block.
    seg_credited  INTEGER NOT NULL DEFAULT 0,
    running       INTEGER NOT NULL DEFAULT 1,
    updated_at    INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
CREATE INDEX IF NOT EXISTS idx_sessions_task ON sessions(task_id);

CREATE TABLE IF NOT EXISTS entries (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    session_id INTEGER REFERENCES sessions(id) ON DELETE SET NULL,
    kind       TEXT    NOT NULL DEFAULT 'work',
    started_at INTEGER NOT NULL,
    ended_at   INTEGER,
    note       TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_entries_task    ON entries(task_id);
CREATE INDEX IF NOT EXISTS idx_entries_started ON entries(started_at);
CREATE INDEX IF NOT EXISTS idx_entries_kind    ON entries(kind);

CREATE TABLE IF NOT EXISTS session_notes (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id            INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    body                  TEXT    NOT NULL,
    title                 TEXT    NOT NULL DEFAULT '',
    emoji                 TEXT    NOT NULL DEFAULT '',
    created_at            INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    updated_at            INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    actionables_scanned_at INTEGER,
    has_actionables       INTEGER NOT NULL DEFAULT 0,
    actionables_scan_version INTEGER NOT NULL DEFAULT 0,
    actionables_json      TEXT    NOT NULL DEFAULT '',
    actionables_extracted_at INTEGER,
    actionables_extract_version INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_session_notes_session ON session_notes(session_id);

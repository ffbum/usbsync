package db

import "database/sql"

const schemaSQL = `
CREATE TABLE IF NOT EXISTS device_meta (
    device_id            TEXT PRIMARY KEY,
    schema_version       INTEGER NOT NULL,
    created_at           TEXT NOT NULL,
    active_machine_limit INTEGER NOT NULL DEFAULT 2,
    workspace_generation INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS machine_registry (
    machine_id      TEXT PRIMARY KEY,
    display_name    TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'active',
    first_seen_at   TEXT NOT NULL,
    last_seen_at    TEXT NOT NULL,
    last_work_root  TEXT
);

CREATE TABLE IF NOT EXISTS blobs (
    blob_id    TEXT NOT NULL,
    chunk_idx  INTEGER NOT NULL,
    content    BLOB NOT NULL,
    PRIMARY KEY (blob_id, chunk_idx)
);

CREATE TABLE IF NOT EXISTS entries (
    path_key         TEXT PRIMARY KEY,
    display_path     TEXT NOT NULL,
    kind             TEXT NOT NULL,
    size             INTEGER,
    mtime_ns         INTEGER,
    content_md5      TEXT,
    blob_id          TEXT,
    chunks           INTEGER DEFAULT 0,
    deleted          INTEGER NOT NULL DEFAULT 0,
    last_revision    INTEGER NOT NULL,
    last_machine_id  TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS change_log (
    revision      INTEGER PRIMARY KEY AUTOINCREMENT,
    machine_id    TEXT NOT NULL,
    op            TEXT NOT NULL,
    path_key      TEXT NOT NULL,
    display_path  TEXT NOT NULL,
    kind          TEXT NOT NULL,
    base_revision INTEGER NOT NULL,
    size          INTEGER,
    mtime_ns      INTEGER,
    content_md5   TEXT,
    blob_id       TEXT,
    created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS machine_state (
    machine_id                 TEXT PRIMARY KEY,
    last_seen_revision         INTEGER NOT NULL DEFAULT 0,
    last_sync_at               TEXT,
    last_backup_at             TEXT,
    last_workspace_generation  INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS sync_sessions (
    session_id   TEXT PRIMARY KEY,
    machine_id   TEXT NOT NULL,
    phase        TEXT NOT NULL,
    status       TEXT NOT NULL,
    started_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp  TEXT NOT NULL,
    machine_id TEXT NOT NULL,
    level      TEXT NOT NULL,
    action     TEXT NOT NULL,
    path_key   TEXT,
    detail     TEXT NOT NULL
);
`

func InitSchema(db *sql.DB) error {
	_, err := db.Exec(schemaSQL)
	return err
}

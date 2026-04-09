package db

import (
	"database/sql"
	"fmt"
)

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
    ctime_ns         INTEGER,
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
    ctime_ns      INTEGER,
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
	if _, err := db.Exec(schemaSQL); err != nil {
		return err
	}

	return applySchemaMigrations(db)
}

func applySchemaMigrations(db *sql.DB) error {
	for _, migration := range []struct {
		table      string
		column     string
		alterTable string
	}{
		{
			table:      "entries",
			column:     "ctime_ns",
			alterTable: `ALTER TABLE entries ADD COLUMN ctime_ns INTEGER`,
		},
		{
			table:      "change_log",
			column:     "ctime_ns",
			alterTable: `ALTER TABLE change_log ADD COLUMN ctime_ns INTEGER`,
		},
	} {
		hasColumn, err := tableHasColumn(db, migration.table, migration.column)
		if err != nil {
			return err
		}
		if hasColumn {
			continue
		}
		if _, err := db.Exec(migration.alterTable); err != nil {
			return err
		}
	}

	return nil
}

func tableHasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info("%s");`, table))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}

	if err := rows.Err(); err != nil {
		return false, err
	}

	return false, nil
}

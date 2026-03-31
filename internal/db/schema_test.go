package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestInitCreatesCoreTables(t *testing.T) {
	db := openTempDB(t)

	if err := InitSchema(db); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	for _, table := range []string{
		"device_meta",
		"machine_registry",
		"blobs",
		"entries",
		"change_log",
		"machine_state",
		"sync_sessions",
		"sync_log",
	} {
		if !hasTable(t, db, table) {
			t.Fatalf("missing table: %s", table)
		}
	}
}

func TestInitCreatesWorkspaceGenerationFields(t *testing.T) {
	db := openTempDB(t)

	if err := InitSchema(db); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	for _, tc := range []struct {
		table  string
		column string
	}{
		{table: "device_meta", column: "workspace_generation"},
		{table: "machine_state", column: "last_workspace_generation"},
	} {
		if !hasColumn(t, db, tc.table, tc.column) {
			t.Fatalf("missing column %s.%s", tc.table, tc.column)
		}
	}
}

func TestOpenStoreCreatesDatabaseFileAndUsesRollbackJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "USBSync.db")

	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if !hasTable(t, store.DB, "device_meta") {
		t.Fatal("expected schema to be initialized")
	}

	var journalMode string
	if err := store.DB.QueryRow(`PRAGMA journal_mode;`).Scan(&journalMode); err != nil {
		t.Fatalf("query journal mode: %v", err)
	}
	if journalMode != "delete" {
		t.Fatalf("unexpected journal mode: %s", journalMode)
	}
}

func openTempDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open temp db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func hasTable(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()

	var name string
	err := db.QueryRow(`
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name = ?
	`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("query table %s: %v", table, err)
	}

	return name == table
}

func hasColumn(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()

	rows, err := db.Query(`PRAGMA table_info(` + table + `);`)
	if err != nil {
		t.Fatalf("table info %s: %v", table, err)
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
			t.Fatalf("scan table info %s: %v", table, err)
		}
		if name == column {
			return true
		}
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table info %s: %v", table, err)
	}

	return false
}

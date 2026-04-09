package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRetireMachineMarksMachineAsRetired(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if _, err := store.DB.Exec(`
		INSERT INTO machine_registry(machine_id, display_name, status, first_seen_at, last_seen_at)
		VALUES('m1', 'Office', 'active', '2026-03-30T00:00:00Z', '2026-03-30T00:00:00Z')
	`); err != nil {
		t.Fatalf("seed machine: %v", err)
	}

	if err := store.RetireMachine("m1"); err != nil {
		t.Fatalf("retire machine: %v", err)
	}

	var status string
	if err := store.DB.QueryRow(`SELECT status FROM machine_registry WHERE machine_id = 'm1'`).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "retired" {
		t.Fatalf("unexpected status: %s", status)
	}
}

func TestAppendWorkspaceResetRecordsChangeAndGeneration(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if _, err := store.DB.Exec(`
		INSERT INTO device_meta(device_id, schema_version, created_at, active_machine_limit, workspace_generation)
		VALUES('device-1', 1, '2026-03-30T00:00:00Z', 2, 1)
	`); err != nil {
		t.Fatalf("seed device meta: %v", err)
	}

	if err := store.AppendWorkspaceReset("m1", `D:\new-work`, 7, 2); err != nil {
		t.Fatalf("append workspace reset: %v", err)
	}

	var (
		op          string
		pathKey     string
		displayPath string
	)
	if err := store.DB.QueryRow(`
		SELECT op, path_key, display_path
		FROM change_log
		ORDER BY revision DESC
		LIMIT 1
	`).Scan(&op, &pathKey, &displayPath); err != nil {
		t.Fatalf("query change log: %v", err)
	}

	if op != "workspace_reset" {
		t.Fatalf("unexpected op: %s", op)
	}
	if pathKey != WorkspaceResetPathKey {
		t.Fatalf("unexpected path key: %s", pathKey)
	}
	if displayPath != `D:\new-work` {
		t.Fatalf("unexpected display path: %s", displayPath)
	}

	var generation int64
	if err := store.DB.QueryRow(`SELECT workspace_generation FROM device_meta WHERE device_id = 'device-1'`).Scan(&generation); err != nil {
		t.Fatalf("query generation: %v", err)
	}
	if generation != 2 {
		t.Fatalf("unexpected generation: %d", generation)
	}
}

func openTempStore(t *testing.T) *Store {
	t.Helper()

	store, err := OpenStore(filepath.Join(t.TempDir(), "USBSync.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	return store
}

func TestCheckIntegrityPassesForHealthyDatabase(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if err := store.CheckIntegrity(); err != nil {
		t.Fatalf("check integrity: %v", err)
	}
}

func TestValidateDatabaseFileRejectsCorruptedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "USBSync.db")
	if err := os.WriteFile(path, []byte("this is not sqlite"), 0o644); err != nil {
		t.Fatalf("write corrupted db: %v", err)
	}

	if err := ValidateDatabaseFile(path); err == nil {
		t.Fatal("expected corrupted database error")
	}
}

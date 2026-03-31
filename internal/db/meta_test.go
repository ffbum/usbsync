package db

import (
	"database/sql"
	"testing"
)

func TestInitializeDeviceMetaCreatesSingleRow(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if err := store.InitializeDeviceMeta("device-1", 1, "2026-03-30T00:00:00Z", 2); err != nil {
		t.Fatalf("initialize device meta: %v", err)
	}

	meta, err := store.GetDeviceMeta()
	if err != nil {
		t.Fatalf("get device meta: %v", err)
	}
	if meta.DeviceID != "device-1" {
		t.Fatalf("unexpected device id: %s", meta.DeviceID)
	}
	if meta.WorkspaceGeneration != 1 {
		t.Fatalf("unexpected workspace generation: %d", meta.WorkspaceGeneration)
	}
	gen, err := store.GetWorkspaceGeneration()
	if err != nil {
		t.Fatalf("get workspace generation: %v", err)
	}
	if gen != 1 {
		t.Fatalf("unexpected workspace generation from helper: %d", gen)
	}

	var count int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM device_meta`).Scan(&count); err != nil {
		t.Fatalf("count device meta: %v", err)
	}
	if count != 1 {
		t.Fatalf("unexpected row count: %d", count)
	}
}

func TestGetWorkspaceGenerationReturnsCurrentValue(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if err := store.InitializeDeviceMeta("device-1", 1, "2026-03-30T00:00:00Z", 2); err != nil {
		t.Fatalf("initialize device meta: %v", err)
	}
	if err := store.AppendWorkspaceReset("m1", `D:\new-work`, 7, 3); err != nil {
		t.Fatalf("append workspace reset: %v", err)
	}

	gen, err := store.GetWorkspaceGeneration()
	if err != nil {
		t.Fatalf("get workspace generation: %v", err)
	}
	if gen != 3 {
		t.Fatalf("unexpected generation: %d", gen)
	}
}

func TestInitializeDeviceMetaDoesNotOverwriteExistingRow(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if err := store.InitializeDeviceMeta("device-1", 1, "2026-03-30T00:00:00Z", 2); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if err := store.InitializeDeviceMeta("device-2", 2, "2026-03-30T01:00:00Z", 3); err != nil {
		t.Fatalf("second init: %v", err)
	}

	meta, err := store.GetDeviceMeta()
	if err != nil {
		t.Fatalf("get device meta: %v", err)
	}
	if meta.DeviceID != "device-1" {
		t.Fatalf("unexpected device id after second init: %s", meta.DeviceID)
	}
	if meta.ActiveMachineLimit != 2 {
		t.Fatalf("unexpected active machine limit: %d", meta.ActiveMachineLimit)
	}
}

func TestUpsertMachineAndStateUpdatesExistingRows(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if err := store.UpsertMachine("m1", "Office", "D:\\work", "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	if err := store.UpdateMachineState("m1", 7, "2026-03-30T00:01:00Z", "2026-03-30T00:02:00Z", 4); err != nil {
		t.Fatalf("update machine state: %v", err)
	}

	machines, err := store.ListMachines()
	if err != nil {
		t.Fatalf("list machines: %v", err)
	}
	if len(machines) != 1 {
		t.Fatalf("unexpected machine count: %d", len(machines))
	}
	if machines[0].DisplayName != "Office" {
		t.Fatalf("unexpected display name: %s", machines[0].DisplayName)
	}

	state, err := store.GetMachineState("m1")
	if err != nil {
		t.Fatalf("get machine state: %v", err)
	}
	if state.LastSeenRevision != 7 {
		t.Fatalf("unexpected revision: %d", state.LastSeenRevision)
	}
	if state.LastWorkspaceGeneration != 4 {
		t.Fatalf("unexpected workspace generation: %d", state.LastWorkspaceGeneration)
	}
}

func TestGetLatestRevisionReturnsHighestRevision(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if _, err := store.DB.Exec(`
		INSERT INTO change_log(machine_id, op, path_key, display_path, kind, base_revision, created_at)
		VALUES
			('m1', 'add', 'docs/a.txt', 'docs/a.txt', 'file', 0, '2026-03-30T00:00:00Z'),
			('m1', 'modify', 'docs/a.txt', 'docs/a.txt', 'file', 1, '2026-03-30T00:01:00Z')
	`); err != nil {
		t.Fatalf("seed change log: %v", err)
	}

	revision, err := store.GetLatestRevision()
	if err != nil {
		t.Fatalf("get latest revision: %v", err)
	}
	if revision != 2 {
		t.Fatalf("unexpected latest revision: %d", revision)
	}
}

func TestGetLatestRevisionIsZeroWhenNoChanges(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	revision, err := store.GetLatestRevision()
	if err != nil {
		t.Fatalf("get latest revision: %v", err)
	}
	if revision != 0 {
		t.Fatalf("unexpected latest revision: %d", revision)
	}
}

func TestLoadDeviceMetaReturnsNotFoundWhenMissing(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if _, err := store.GetDeviceMeta(); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
	if _, err := store.GetWorkspaceGeneration(); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows from generation helper, got %v", err)
	}
}

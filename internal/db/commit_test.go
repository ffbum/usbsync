package db

import (
	"testing"

	syncpreview "usbsync/internal/sync"
)

func TestStoreBlobChunksPersistsChunks(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	err := store.StoreBlobChunks("blob-1", [][]byte{
		[]byte("hello"),
		[]byte("world"),
	})
	if err != nil {
		t.Fatalf("store blob chunks: %v", err)
	}

	var count int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM blobs WHERE blob_id = 'blob-1'`).Scan(&count); err != nil {
		t.Fatalf("count blobs: %v", err)
	}
	if count != 2 {
		t.Fatalf("unexpected chunk count: %d", count)
	}
}

func TestCommitLocalFileChangeWritesEntryChangeLogAndMachineState(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if err := store.InitializeDeviceMeta("device-1", 1, "2026-03-30T00:00:00Z", 2); err != nil {
		t.Fatalf("initialize device meta: %v", err)
	}
	if err := store.UpsertMachine("m1", "Office", `D:\work`, "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	revision, err := store.CommitLocalChange("m1", syncpreview.LocalChange{
		Op:           "add",
		PathKey:      "docs/a.txt",
		DisplayPath:  "docs/a.txt",
		Kind:         "file",
		Size:         5,
		CtimeNS:      9,
		MtimeNS:      10,
		MD5:          "md5-1",
		BaseRevision: 0,
	}, BlobWrite{
		BlobID: "md5-1",
		Chunks: [][]byte{[]byte("hello")},
	}, "2026-03-30T00:01:00Z")
	if err != nil {
		t.Fatalf("commit local change: %v", err)
	}
	if revision != 1 {
		t.Fatalf("unexpected revision: %d", revision)
	}

	records, err := store.ListEntries()
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("unexpected entry count: %d", len(records))
	}
	if records[0].ContentMD5 != "md5-1" {
		t.Fatalf("unexpected md5: %s", records[0].ContentMD5)
	}
	if records[0].CtimeNS != 9 {
		t.Fatalf("unexpected ctime: %d", records[0].CtimeNS)
	}
	if records[0].Deleted {
		t.Fatal("expected active entry")
	}

	changes, err := store.ListChangesAfter(0)
	if err != nil {
		t.Fatalf("list changes: %v", err)
	}
	if len(changes) != 1 || changes[0].Op != "add" {
		t.Fatalf("unexpected changes: %#v", changes)
	}
	if changes[0].CtimeNS != 9 {
		t.Fatalf("unexpected change ctime: %d", changes[0].CtimeNS)
	}

	state, err := store.GetMachineState("m1")
	if err != nil {
		t.Fatalf("get machine state: %v", err)
	}
	if state.LastSeenRevision != 1 {
		t.Fatalf("unexpected machine revision: %d", state.LastSeenRevision)
	}
}

func TestCommitLocalDeleteMarksEntryDeleted(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if err := store.InitializeDeviceMeta("device-1", 1, "2026-03-30T00:00:00Z", 2); err != nil {
		t.Fatalf("initialize device meta: %v", err)
	}
	if err := store.UpsertMachine("m1", "Office", `D:\work`, "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO entries(path_key, display_path, kind, size, mtime_ns, deleted, last_revision, last_machine_id, updated_at)
		VALUES('docs/a.txt', 'docs/a.txt', 'file', 5, 10, 0, 3, 'm1', '2026-03-30T00:00:00Z')
	`); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	revision, err := store.CommitLocalChange("m1", syncpreview.LocalChange{
		Op:           "delete",
		PathKey:      "docs/a.txt",
		DisplayPath:  "docs/a.txt",
		Kind:         "file",
		BaseRevision: 3,
	}, BlobWrite{}, "2026-03-30T00:02:00Z")
	if err != nil {
		t.Fatalf("commit delete: %v", err)
	}
	if revision != 1 {
		t.Fatalf("unexpected revision: %d", revision)
	}

	records, err := store.ListEntries()
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(records) != 1 || !records[0].Deleted {
		t.Fatalf("expected deleted record, got %#v", records)
	}
}

func TestCommitLocalFileChangeUsesBlobIDAsContentHashWhenMissing(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if err := store.InitializeDeviceMeta("device-1", 1, "2026-03-30T00:00:00Z", 2); err != nil {
		t.Fatalf("initialize device meta: %v", err)
	}
	if err := store.UpsertMachine("m1", "Office", `D:\work`, "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	_, err := store.CommitLocalChange("m1", syncpreview.LocalChange{
		Op:           "add",
		PathKey:      "docs/a.txt",
		DisplayPath:  "docs/a.txt",
		Kind:         "file",
		Size:         5,
		CtimeNS:      9,
		MtimeNS:      10,
		BaseRevision: 0,
	}, BlobWrite{
		BlobID: "md5-blob",
		Chunks: [][]byte{[]byte("hello")},
	}, "2026-03-30T00:01:00Z")
	if err != nil {
		t.Fatalf("commit local change: %v", err)
	}

	records, err := store.ListEntries()
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("unexpected entry count: %d", len(records))
	}
	if records[0].ContentMD5 != "md5-blob" {
		t.Fatalf("unexpected content hash: %s", records[0].ContentMD5)
	}

	changes, err := store.ListChangesAfter(0)
	if err != nil {
		t.Fatalf("list changes: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("unexpected change count: %d", len(changes))
	}
	if changes[0].ContentMD5 != "md5-blob" {
		t.Fatalf("unexpected change hash: %s", changes[0].ContentMD5)
	}
}

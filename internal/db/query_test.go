package db

import "testing"

func TestListEntriesReturnsDeletionFlagsAndRevision(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if _, err := store.DB.Exec(`
		INSERT INTO entries(path_key, display_path, kind, size, ctime_ns, mtime_ns, deleted, last_revision, last_machine_id, updated_at)
		VALUES
			('docs/a.txt', 'docs/a.txt', 'file', 5, 9, 10, 0, 3, 'm1', '2026-03-30T00:00:00Z'),
			('docs/b.txt', 'docs/b.txt', 'file', 0, 0, 0, 1, 4, 'm1', '2026-03-30T00:00:00Z')
	`); err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	records, err := store.ListEntries()
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("unexpected entry count: %d", len(records))
	}
	if records[0].PathKey != "docs/a.txt" || records[1].PathKey != "docs/b.txt" {
		t.Fatalf("unexpected order: %#v", records)
	}
	if records[1].Deleted != true {
		t.Fatal("expected deleted flag on second record")
	}
	if records[0].LastRevision != 3 {
		t.Fatalf("unexpected revision: %d", records[0].LastRevision)
	}
	if records[0].CtimeNS != 9 {
		t.Fatalf("unexpected ctime: %d", records[0].CtimeNS)
	}
}

func TestListEntriesFallsBackToBlobIDWhenContentHashMissing(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if _, err := store.DB.Exec(`
		INSERT INTO entries(path_key, display_path, kind, size, mtime_ns, content_md5, blob_id, deleted, last_revision, last_machine_id, updated_at)
		VALUES('docs/a.txt', 'docs/a.txt', 'file', 5, 10, '', 'hash-from-blob', 0, 3, 'm1', '2026-03-30T00:00:00Z')
	`); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	records, err := store.ListEntries()
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("unexpected entry count: %d", len(records))
	}
	if records[0].ContentMD5 != "hash-from-blob" {
		t.Fatalf("unexpected content hash: %s", records[0].ContentMD5)
	}
}

func TestListEntriesAtRevisionReturnsSnapshotForThatPointInTime(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if _, err := store.DB.Exec(`
		INSERT INTO change_log(revision, machine_id, op, path_key, display_path, kind, base_revision, size, ctime_ns, content_md5, blob_id, created_at)
		VALUES
			(1, 'm1', 'mkdir', 'docs', 'docs', 'dir', 0, 0, 0, '', '', '2026-03-30T00:00:00Z'),
			(2, 'm1', 'add', 'docs/a.txt', 'docs/a.txt', 'file', 0, 5, 20, 'hash-a-v1', 'hash-a-v1', '2026-03-30T00:00:00Z'),
			(3, 'm2', 'modify', 'docs/a.txt', 'docs/a.txt', 'file', 2, 5, 30, 'hash-a-v2', 'hash-a-v2', '2026-03-30T00:01:00Z'),
			(4, 'm2', 'delete', 'docs/a.txt', 'docs/a.txt', 'file', 3, NULL, NULL, '', '', '2026-03-30T00:02:00Z')
	`); err != nil {
		t.Fatalf("seed changes: %v", err)
	}

	records, err := store.ListEntriesAtRevision(3)
	if err != nil {
		t.Fatalf("list entries at revision: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("unexpected snapshot count: %d", len(records))
	}
	if records[1].PathKey != "docs/a.txt" {
		t.Fatalf("unexpected file record: %#v", records)
	}
	if records[1].Deleted {
		t.Fatalf("expected file to exist at revision 3, got %#v", records[1])
	}
	if records[1].ContentMD5 != "hash-a-v2" {
		t.Fatalf("unexpected content hash at revision 3: %s", records[1].ContentMD5)
	}
	if records[1].CtimeNS != 30 {
		t.Fatalf("unexpected ctime at revision 3: %d", records[1].CtimeNS)
	}

	records, err = store.ListEntriesAtRevision(4)
	if err != nil {
		t.Fatalf("list entries at revision 4: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("unexpected snapshot count at revision 4: %d", len(records))
	}
	if !records[1].Deleted {
		t.Fatalf("expected deleted file at revision 4, got %#v", records[1])
	}
}

func TestListChangesAfterJoinsDisplayName(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if err := store.UpsertMachine("m1", "Office PC", `D:\work`, "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO change_log(revision, machine_id, op, path_key, display_path, kind, base_revision, ctime_ns, created_at)
		VALUES
			(1, 'm1', 'add', 'docs/a.txt', 'docs/a.txt', 'file', 0, 11, '2026-03-30T00:00:00Z'),
			(2, 'm1', 'modify', 'docs/a.txt', 'docs/a.txt', 'file', 1, 22, '2026-03-30T00:01:00Z')
	`); err != nil {
		t.Fatalf("seed changes: %v", err)
	}

	records, err := store.ListChangesAfter(1)
	if err != nil {
		t.Fatalf("list changes after: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("unexpected change count: %d", len(records))
	}
	if records[0].MachineName != "Office PC" {
		t.Fatalf("unexpected machine name: %s", records[0].MachineName)
	}
	if records[0].Revision != 2 {
		t.Fatalf("unexpected revision: %d", records[0].Revision)
	}
	if records[0].CtimeNS != 22 {
		t.Fatalf("unexpected ctime: %d", records[0].CtimeNS)
	}
}

func TestReadBlobReassemblesChunksInOrder(t *testing.T) {
	store := openTempStore(t)
	defer store.Close()

	if err := store.StoreBlobChunks("blob-1", [][]byte{
		[]byte("hello "),
		[]byte("world"),
	}); err != nil {
		t.Fatalf("store chunks: %v", err)
	}

	data, err := store.ReadBlob("blob-1")
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("unexpected blob content: %s", string(data))
	}
}

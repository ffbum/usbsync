package db

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
	syncpreview "usbsync/internal/sync"
)

const WorkspaceResetPathKey = "__workspace__"

type Store struct {
	Path string
	DB   *sql.DB
}

type DeviceMeta struct {
	DeviceID            string
	SchemaVersion       int64
	CreatedAt           string
	ActiveMachineLimit  int64
	WorkspaceGeneration int64
}

type MachineState struct {
	MachineID               string
	LastSeenRevision        int64
	LastSyncAt              string
	LastBackupAt            string
	LastWorkspaceGeneration int64
}

type EntryRecord struct {
	PathKey      string
	DisplayPath  string
	Kind         string
	Size         int64
	MtimeNS      int64
	ContentMD5   string
	Deleted      bool
	LastRevision int64
}

type ChangeRecord struct {
	Revision     int64
	MachineID    string
	MachineName  string
	Op           string
	PathKey      string
	DisplayPath  string
	Kind         string
	BaseRevision int64
	Size         int64
	MtimeNS      int64
	ContentMD5   string
	BlobID       string
}

type BlobWrite struct {
	BlobID string
	Chunks [][]byte
}

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	store := &Store{
		Path: path,
		DB:   db,
	}

	if err := store.configure(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := InitSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}

	return s.DB.Close()
}

func (s *Store) configure() error {
	s.DB.SetMaxOpenConns(1)

	for _, pragma := range []string{
		`PRAGMA journal_mode=DELETE;`,
		`PRAGMA busy_timeout=5000;`,
		`PRAGMA foreign_keys=ON;`,
	} {
		if _, err := s.DB.Exec(pragma); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) InitializeDeviceMeta(deviceID string, schemaVersion int64, createdAt string, activeMachineLimit int64) error {
	_, err := s.DB.Exec(`
		INSERT OR IGNORE INTO device_meta (
			device_id, schema_version, created_at, active_machine_limit, workspace_generation
		) VALUES (?, ?, ?, ?, 1)
	`, deviceID, schemaVersion, createdAt, activeMachineLimit)
	return err
}

func (s *Store) GetDeviceMeta() (DeviceMeta, error) {
	var meta DeviceMeta
	err := s.DB.QueryRow(`
		SELECT device_id, schema_version, created_at, active_machine_limit, workspace_generation
		FROM device_meta
		LIMIT 1
	`).Scan(
		&meta.DeviceID,
		&meta.SchemaVersion,
		&meta.CreatedAt,
		&meta.ActiveMachineLimit,
		&meta.WorkspaceGeneration,
	)
	return meta, err
}

func (s *Store) GetWorkspaceGeneration() (int64, error) {
	var generation int64
	err := s.DB.QueryRow(`
		SELECT workspace_generation
		FROM device_meta
		LIMIT 1
	`).Scan(&generation)
	return generation, err
}

func (s *Store) UpsertMachine(machineID, displayName, lastWorkRoot, seenAt string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec(`
		INSERT INTO machine_registry (
			machine_id, display_name, status, first_seen_at, last_seen_at, last_work_root
		) VALUES (?, ?, 'active', ?, ?, ?)
		ON CONFLICT(machine_id) DO UPDATE SET
			display_name = excluded.display_name,
			status = 'active',
			last_seen_at = excluded.last_seen_at,
			last_work_root = excluded.last_work_root
	`, machineID, displayName, seenAt, seenAt, lastWorkRoot); err != nil {
		return err
	}

	if _, err = tx.Exec(`
		INSERT OR IGNORE INTO machine_state (
			machine_id, last_seen_revision, last_sync_at, last_backup_at, last_workspace_generation
		) VALUES (?, 0, NULL, NULL, 1)
	`, machineID); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) UpdateMachineState(machineID string, lastSeenRevision int64, lastSyncAt, lastBackupAt string, lastWorkspaceGeneration int64) error {
	_, err := s.DB.Exec(`
		INSERT INTO machine_state (
			machine_id, last_seen_revision, last_sync_at, last_backup_at, last_workspace_generation
		) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(machine_id) DO UPDATE SET
			last_seen_revision = excluded.last_seen_revision,
			last_sync_at = excluded.last_sync_at,
			last_backup_at = excluded.last_backup_at,
			last_workspace_generation = excluded.last_workspace_generation
	`, machineID, lastSeenRevision, nullableString(lastSyncAt), nullableString(lastBackupAt), lastWorkspaceGeneration)
	return err
}

func (s *Store) GetMachineState(machineID string) (MachineState, error) {
	var state MachineState
	err := s.DB.QueryRow(`
		SELECT machine_id, last_seen_revision, COALESCE(last_sync_at, ''), COALESCE(last_backup_at, ''), last_workspace_generation
		FROM machine_state
		WHERE machine_id = ?
	`, machineID).Scan(
		&state.MachineID,
		&state.LastSeenRevision,
		&state.LastSyncAt,
		&state.LastBackupAt,
		&state.LastWorkspaceGeneration,
	)
	return state, err
}

func (s *Store) GetLatestRevision() (int64, error) {
	var revision sql.NullInt64
	if err := s.DB.QueryRow(`SELECT MAX(revision) FROM change_log`).Scan(&revision); err != nil {
		return 0, err
	}
	if !revision.Valid {
		return 0, nil
	}

	return revision.Int64, nil
}

func (s *Store) ListEntries() ([]EntryRecord, error) {
	rows, err := s.DB.Query(`
		SELECT
			path_key,
			display_path,
			kind,
			COALESCE(size, 0),
			COALESCE(mtime_ns, 0),
			COALESCE(NULLIF(content_md5, ''), NULLIF(blob_id, ''), ''),
			deleted,
			last_revision
		FROM entries
		ORDER BY path_key
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]EntryRecord, 0)
	for rows.Next() {
		var (
			record  EntryRecord
			deleted int64
		)
		if err := rows.Scan(
			&record.PathKey,
			&record.DisplayPath,
			&record.Kind,
			&record.Size,
			&record.MtimeNS,
			&record.ContentMD5,
			&deleted,
			&record.LastRevision,
		); err != nil {
			return nil, err
		}
		record.Deleted = deleted != 0
		records = append(records, record)
	}

	return records, rows.Err()
}

func (s *Store) ListEntriesAtRevision(revision int64) ([]EntryRecord, error) {
	if revision <= 0 {
		return []EntryRecord{}, nil
	}

	rows, err := s.DB.Query(`
		SELECT
			c.path_key,
			c.display_path,
			c.kind,
			COALESCE(c.size, 0),
			COALESCE(c.mtime_ns, 0),
			COALESCE(NULLIF(c.content_md5, ''), NULLIF(c.blob_id, ''), ''),
			CASE WHEN c.op = 'delete' THEN 1 ELSE 0 END AS deleted,
			c.revision
		FROM change_log c
		INNER JOIN (
			SELECT path_key, MAX(revision) AS revision
			FROM change_log
			WHERE revision <= ?
			GROUP BY path_key
		) latest ON latest.path_key = c.path_key AND latest.revision = c.revision
		ORDER BY c.path_key
	`, revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]EntryRecord, 0)
	for rows.Next() {
		var (
			record  EntryRecord
			deleted int64
		)
		if err := rows.Scan(
			&record.PathKey,
			&record.DisplayPath,
			&record.Kind,
			&record.Size,
			&record.MtimeNS,
			&record.ContentMD5,
			&deleted,
			&record.LastRevision,
		); err != nil {
			return nil, err
		}
		record.Deleted = deleted != 0
		records = append(records, record)
	}

	return records, rows.Err()
}

func (s *Store) ListChangesAfter(lastSeenRevision int64) ([]ChangeRecord, error) {
	rows, err := s.DB.Query(`
		SELECT
			c.revision,
			c.machine_id,
			COALESCE(m.display_name, c.machine_id),
			c.op,
			c.path_key,
			c.display_path,
			c.kind,
			c.base_revision,
			COALESCE(c.size, 0),
			COALESCE(c.mtime_ns, 0),
			COALESCE(NULLIF(c.content_md5, ''), NULLIF(c.blob_id, ''), ''),
			COALESCE(c.blob_id, '')
		FROM change_log c
		LEFT JOIN machine_registry m ON m.machine_id = c.machine_id
		WHERE c.revision > ?
		ORDER BY c.revision
	`, lastSeenRevision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]ChangeRecord, 0)
	for rows.Next() {
		var record ChangeRecord
		if err := rows.Scan(
			&record.Revision,
			&record.MachineID,
			&record.MachineName,
			&record.Op,
			&record.PathKey,
			&record.DisplayPath,
			&record.Kind,
			&record.BaseRevision,
			&record.Size,
			&record.MtimeNS,
			&record.ContentMD5,
			&record.BlobID,
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

func (s *Store) ReadBlob(blobID string) ([]byte, error) {
	rows, err := s.DB.Query(`
		SELECT content
		FROM blobs
		WHERE blob_id = ?
		ORDER BY chunk_idx
	`, blobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	data := make([]byte, 0)
	found := false
	for rows.Next() {
		found = true
		var chunk []byte
		if err := rows.Scan(&chunk); err != nil {
			return nil, err
		}
		data = append(data, chunk...)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, sql.ErrNoRows
	}

	return data, nil
}

type MachineRecord struct {
	MachineID    string
	DisplayName  string
	Status       string
	LastWorkRoot string
}

func (s *Store) RetireMachine(machineID string) error {
	result, err := s.DB.Exec(`
		UPDATE machine_registry
		SET status = 'retired'
		WHERE machine_id = ?
	`, machineID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) ListMachines() ([]MachineRecord, error) {
	rows, err := s.DB.Query(`
		SELECT machine_id, display_name, status, COALESCE(last_work_root, '')
		FROM machine_registry
		ORDER BY display_name, machine_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]MachineRecord, 0)
	for rows.Next() {
		var record MachineRecord
		if err := rows.Scan(&record.MachineID, &record.DisplayName, &record.Status, &record.LastWorkRoot); err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

func (s *Store) AppendWorkspaceReset(machineID, displayPath string, baseRevision, workspaceGeneration int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.Exec(`
		UPDATE device_meta
		SET workspace_generation = ?
	`, workspaceGeneration)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return errors.New("device_meta is not initialized")
	}

	_, err = tx.Exec(`
		INSERT INTO change_log (
			machine_id, op, path_key, display_path, kind, base_revision, created_at
		) VALUES (?, 'workspace_reset', ?, ?, 'dir', ?, ?)
	`, machineID, WorkspaceResetPathKey, displayPath, baseRevision, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}

	err = tx.Commit()
	return err
}

func (s *Store) StoreBlobChunks(blobID string, chunks [][]byte) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if err = storeBlobChunksTx(tx, blobID, chunks); err != nil {
		return err
	}

	err = tx.Commit()
	return err
}

func (s *Store) CommitLocalChange(machineID string, change syncpreview.LocalChange, blob BlobWrite, seenAt string) (int64, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if blob.BlobID != "" {
		if err = storeBlobChunksTx(tx, blob.BlobID, blob.Chunks); err != nil {
			return 0, err
		}
	}

	chunks := len(blob.Chunks)
	if change.Kind == "dir" {
		chunks = 0
	}
	contentHash := normalizedContentHash(change, blob)

	result, err := tx.Exec(`
		INSERT INTO change_log (
			machine_id, op, path_key, display_path, kind, base_revision,
			size, mtime_ns, content_md5, blob_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		machineID,
		normalizeChangeOp(change),
		change.PathKey,
		change.DisplayPath,
		change.Kind,
		change.BaseRevision,
		nullableInt64(change.Size, change.Op == "delete" || change.Kind == "dir"),
		nullableInt64(change.MtimeNS, change.Op == "delete"),
		nullableString(contentHash),
		nullableString(blob.BlobID),
		seenAt,
	)
	if err != nil {
		return 0, err
	}

	revision, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	if err = upsertEntryTx(tx, machineID, revision, change, blob, chunks, seenAt); err != nil {
		return 0, err
	}

	workspaceGeneration, err := getWorkspaceGenerationTx(tx)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	if workspaceGeneration == 0 {
		workspaceGeneration = 1
	}

	previousBackupAt, err := getMachineBackupAtTx(tx, machineID)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}

	if _, err = tx.Exec(`
		INSERT INTO machine_state (
			machine_id, last_seen_revision, last_sync_at, last_backup_at, last_workspace_generation
		) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(machine_id) DO UPDATE SET
			last_seen_revision = excluded.last_seen_revision,
			last_sync_at = excluded.last_sync_at,
			last_backup_at = excluded.last_backup_at,
			last_workspace_generation = excluded.last_workspace_generation
	`, machineID, revision, nullableString(seenAt), nullableString(previousBackupAt), workspaceGeneration); err != nil {
		return 0, err
	}

	err = tx.Commit()
	return revision, err
}

func storeBlobChunksTx(tx *sql.Tx, blobID string, chunks [][]byte) error {
	for idx, chunk := range chunks {
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO blobs (blob_id, chunk_idx, content)
			VALUES (?, ?, ?)
		`, blobID, idx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func upsertEntryTx(tx *sql.Tx, machineID string, revision int64, change syncpreview.LocalChange, blob BlobWrite, chunks int, seenAt string) error {
	deleted := 0
	if change.Op == "delete" {
		deleted = 1
		chunks = 0
	}
	contentHash := normalizedContentHash(change, blob)

	_, err := tx.Exec(`
		INSERT INTO entries (
			path_key, display_path, kind, size, mtime_ns, content_md5, blob_id, chunks,
			deleted, last_revision, last_machine_id, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path_key) DO UPDATE SET
			display_path = excluded.display_path,
			kind = excluded.kind,
			size = excluded.size,
			mtime_ns = excluded.mtime_ns,
			content_md5 = excluded.content_md5,
			blob_id = excluded.blob_id,
			chunks = excluded.chunks,
			deleted = excluded.deleted,
			last_revision = excluded.last_revision,
			last_machine_id = excluded.last_machine_id,
			updated_at = excluded.updated_at
	`,
		change.PathKey,
		change.DisplayPath,
		change.Kind,
		nullableInt64(change.Size, deleted == 1 || change.Kind == "dir"),
		nullableInt64(change.MtimeNS, deleted == 1),
		nullableString(contentHash),
		nullableString(blob.BlobID),
		chunks,
		deleted,
		revision,
		machineID,
		seenAt,
	)
	return err
}

func normalizeChangeOp(change syncpreview.LocalChange) string {
	if change.Op == "add" && change.Kind == "dir" {
		return "mkdir"
	}

	return change.Op
}

func normalizedContentHash(change syncpreview.LocalChange, blob BlobWrite) string {
	if change.Kind != "file" || change.Op == "delete" {
		return change.MD5
	}
	if change.MD5 != "" {
		return change.MD5
	}
	return blob.BlobID
}

func nullableInt64(value int64, asNull bool) any {
	if asNull {
		return nil
	}
	return value
}

func getWorkspaceGenerationTx(tx *sql.Tx) (int64, error) {
	var generation int64
	err := tx.QueryRow(`
		SELECT workspace_generation
		FROM device_meta
		LIMIT 1
	`).Scan(&generation)
	return generation, err
}

func getMachineBackupAtTx(tx *sql.Tx, machineID string) (string, error) {
	var backupAt sql.NullString
	err := tx.QueryRow(`
		SELECT last_backup_at
		FROM machine_state
		WHERE machine_id = ?
	`, machineID).Scan(&backupAt)
	if err != nil {
		return "", err
	}
	if !backupAt.Valid {
		return "", nil
	}
	return backupAt.String, nil
}

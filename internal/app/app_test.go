package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"usbsync/internal/config"
	"usbsync/internal/db"
	"usbsync/internal/fileindex"
	"usbsync/internal/machine"
	syncpreview "usbsync/internal/sync"
	"usbsync/internal/ui"
	"usbsync/internal/usb"
)

func TestDefaultBuildInfoHasVersion(t *testing.T) {
	info := DefaultBuildInfo()
	if info.Version == "" {
		t.Fatal("expected version to be non-empty")
	}
}

func TestLoadStartupStateLoadsSavedMachineConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "machine.json")
	cfg := config.DefaultMachineConfig()
	cfg.DisplayName = "Office"
	cfg.LastWorkRoot = `D:\work`

	if err := config.SaveMachineConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	dbPath := filepath.Join(exeDir, usb.DatabaseFileName)
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	if err := os.WriteFile(dbPath, []byte("db"), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}

	state, err := LoadStartupState(cfgPath, usb.DriveProbe{
		ExePath:     exePath,
		IsRemovable: true,
		VolumeID:    "vol-123",
	})
	if err != nil {
		t.Fatalf("load startup state: %v", err)
	}

	if state.MachineConfig.DisplayName != "Office" {
		t.Fatalf("unexpected display name: %s", state.MachineConfig.DisplayName)
	}
	if state.MachineConfig.LastWorkRoot != `D:\work` {
		t.Fatalf("unexpected work root: %s", state.MachineConfig.LastWorkRoot)
	}
	if state.Drive.DBPath != dbPath {
		t.Fatalf("unexpected db path: %s", state.Drive.DBPath)
	}
}

func TestDefaultMachineConfigPathUsesExecutableDirectory(t *testing.T) {
	path, err := defaultMachineConfigPath(
		func() (string, error) { return filepath.Join(`E:\usb`, "USBSync.exe"), nil },
		func() (string, error) { return "Office PC", nil },
	)
	if err != nil {
		t.Fatalf("default machine config path: %v", err)
	}

	expected := filepath.Join(`E:\usb`, "usbsync.json")
	if path != expected {
		t.Fatalf("unexpected config path: %s", path)
	}
}

func TestBuildMainViewModelEnablesInitializeWhenDatabaseMissing(t *testing.T) {
	vm := buildMainViewModel(StartupState{
		MachineConfig: config.MachineConfig{
			BackupDir: "C:\\.usbsync\\backup",
		},
		Drive: usb.DriveContext{
			DatabaseExists: false,
			RootPath:       `E:\`,
			VolumeName:     "WORKUSB",
		},
	}, nil)

	if !vm.InitializeEnabled {
		t.Fatal("expected initialize button to be enabled")
	}
	if vm.SyncEnabled {
		t.Fatal("expected sync button to be disabled")
	}
	if vm.DriveStatus != "当前 U 盘：E:  卷名：WORKUSB" {
		t.Fatalf("unexpected drive status: %s", vm.DriveStatus)
	}
}

func TestBuildMainViewModelRequiresReinitializeWhenWorkRootChanged(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "USBSync.db")
	store, err := db.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.InitializeDeviceMeta("device-1", 1, "2026-03-30T00:00:00Z", 2); err != nil {
		t.Fatalf("initialize meta: %v", err)
	}
	if err := store.UpsertMachine("machine-1", "Office", `D:\old-work`, "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	store.Close()

	vm := buildMainViewModel(StartupState{
		MachineConfig: config.MachineConfig{
			MachineID:    "machine-1",
			LastWorkRoot: `D:\new-work`,
			BackupDir:    "C:\\.usbsync\\backup",
		},
		Drive: usb.DriveContext{
			DatabaseExists: true,
			DBPath:         dbPath,
			RootPath:       `E:\`,
			VolumeName:     "WORKUSB",
		},
	}, nil)

	if !vm.InitializeEnabled {
		t.Fatal("expected initialize button enabled after work root change")
	}
	if vm.SyncEnabled {
		t.Fatal("expected sync button disabled after work root change")
	}
	if vm.Results.Status != "工作文件夹已更换，请重新初始化当前 U 盘" {
		t.Fatalf("unexpected status: %s", vm.Results.Status)
	}
}

func TestBuildRuntimeViewModelProvidesActionHandlers(t *testing.T) {
	controller := newRuntimeController(
		filepath.Join(t.TempDir(), "machine.json"),
		nil,
		func() time.Time { return time.Unix(0, 0) },
		func() (string, error) { return "Office", nil },
	)

	vm := buildRuntimeViewModel(StartupState{
		MachineConfig: config.MachineConfig{
			LastWorkRoot: `D:\work`,
			DisplayName:  "Office",
			BackupDir:    `C:\.usbsync\backup`,
		},
		Drive: usb.DriveContext{
			DatabaseExists: false,
		},
	}, nil, controller)

	if vm.OnInitialize == nil {
		t.Fatal("expected initialize handler")
	}
	if vm.OnSync == nil {
		t.Fatal("expected sync handler")
	}
	if vm.OnDraftChanged == nil {
		t.Fatal("expected draft handler")
	}
}

func TestInitializeCurrentUSBWritesDatabaseAndMachineConfig(t *testing.T) {
	workRoot := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatalf("mkdir work root: %v", err)
	}

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}

	drive, err := usb.BuildDriveContext(usb.DriveProbe{
		ExePath:     exePath,
		IsRemovable: true,
		VolumeID:    "vol-123",
	})
	if err != nil {
		t.Fatalf("build drive context: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "machine.json")
	request := InitializeCurrentUSBRequest{
		MachineConfigPath: cfgPath,
		MachineProfile:    "test-host",
		Drive:             drive,
		MachineID:         "machine-1",
		DisplayName:       "Office",
		WorkRoot:          workRoot,
		BackupDir:         filepath.Join(t.TempDir(), "backup"),
		SchemaVersion:     1,
	}

	result, err := InitializeCurrentUSB(request)
	if err != nil {
		t.Fatalf("initialize current usb: %v", err)
	}

	if result.DatabasePath != drive.DBPath {
		t.Fatalf("unexpected database path: %s", result.DatabasePath)
	}

	cfg, err := config.LoadMachineConfigForMachine(cfgPath, "test-host")
	if err != nil {
		t.Fatalf("load saved config: %v", err)
	}
	if cfg.MachineID != "machine-1" {
		t.Fatalf("unexpected machine id: %s", cfg.MachineID)
	}
	if cfg.DisplayName != "Office" {
		t.Fatalf("unexpected display name: %s", cfg.DisplayName)
	}
	if cfg.LastWorkRoot != workRoot {
		t.Fatalf("unexpected work root: %s", cfg.LastWorkRoot)
	}

	store, err := db.OpenStore(drive.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

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

	machines, err := store.ListMachines()
	if err != nil {
		t.Fatalf("list machines: %v", err)
	}
	if len(machines) != 1 {
		t.Fatalf("unexpected machine count: %d", len(machines))
	}
	if machines[0].MachineID != "machine-1" {
		t.Fatalf("unexpected machine id in registry: %s", machines[0].MachineID)
	}

	state, err := store.GetMachineState("machine-1")
	if err != nil {
		t.Fatalf("get machine state: %v", err)
	}
	if state.LastSeenRevision != 0 {
		t.Fatalf("unexpected revision: %d", state.LastSeenRevision)
	}
}

func TestInitializeCurrentUSBAllowsMissingDatabaseBeforeInitialization(t *testing.T) {
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}

	drive, err := usb.BuildDriveContext(usb.DriveProbe{
		ExePath:     exePath,
		IsRemovable: true,
	})
	if err != nil {
		t.Fatalf("build drive context: %v", err)
	}
	if drive.DatabaseExists {
		t.Fatal("expected missing database")
	}

	cfgPath := filepath.Join(t.TempDir(), "machine.json")
	result, err := InitializeCurrentUSB(InitializeCurrentUSBRequest{
		MachineConfigPath: cfgPath,
		MachineProfile:    "test-host",
		Drive:             drive,
		MachineID:         "machine-2",
		DisplayName:       "Lab",
		WorkRoot:          filepath.Join(t.TempDir(), "work"),
		BackupDir:         filepath.Join(t.TempDir(), "backup"),
		DeviceID:          "device-2",
		SchemaVersion:     1,
	})
	if err != nil {
		t.Fatalf("initialize current usb: %v", err)
	}
	if result.DatabasePath != drive.DBPath {
		t.Fatalf("unexpected database path: %s", result.DatabasePath)
	}

	if _, err := os.Stat(drive.DBPath); err != nil {
		t.Fatalf("expected database file to exist: %v", err)
	}
}

func TestInitializeCurrentUSBReportsProgressEvents(t *testing.T) {
	workRoot := filepath.Join(t.TempDir(), "work")
	backupDir := filepath.Join(t.TempDir(), "backup")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatalf("mkdir work root: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	drive, err := usb.BuildDriveContext(usb.DriveProbe{
		ExePath:     exePath,
		IsRemovable: true,
	})
	if err != nil {
		t.Fatalf("build drive context: %v", err)
	}

	var events []syncpreview.Event
	_, err = InitializeCurrentUSB(InitializeCurrentUSBRequest{
		MachineConfigPath: filepath.Join(t.TempDir(), "usbsync.json"),
		MachineProfile:    "test-host",
		Drive:             drive,
		MachineID:         "machine-3",
		DisplayName:       "Office",
		WorkRoot:          workRoot,
		BackupDir:         backupDir,
		Progress: func(event syncpreview.Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("initialize current usb: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 progress events, got %d", len(events))
	}
}

func TestPreviewSyncLoadsDatabaseStateAndBuildsConflictPreview(t *testing.T) {
	workRoot := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(filepath.Join(workRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	reportPath := filepath.Join(workRoot, "docs", "report.txt")
	if err := os.WriteFile(reportPath, []byte("local change"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "USBSync.db")
	store, err := db.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.InitializeDeviceMeta("device-1", 1, "2026-03-30T00:00:00Z", 2); err != nil {
		t.Fatalf("initialize device meta: %v", err)
	}
	if err := store.UpsertMachine("remote-1", "Office PC", `D:\work`, "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert remote machine: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO entries(path_key, display_path, kind, size, mtime_ns, last_revision, last_machine_id, updated_at)
		VALUES('docs/report.txt', 'docs/report.txt', 'file', 1, 1, 3, 'remote-1', '2026-03-30T00:00:00Z')
	`); err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO change_log(revision, machine_id, op, path_key, display_path, kind, base_revision, created_at)
		VALUES(6, 'remote-1', 'modify', 'docs/report.txt', 'docs/report.txt', 'file', 3, '2026-03-30T00:01:00Z')
	`); err != nil {
		t.Fatalf("seed change log: %v", err)
	}

	preview, err := PreviewSync(PreviewSyncRequest{
		WorkRoot:         workRoot,
		DatabasePath:     dbPath,
		LastSeenRevision: 0,
	})
	if err != nil {
		t.Fatalf("preview sync: %v", err)
	}

	if len(preview.LocalChanges) == 0 {
		t.Fatal("expected local changes")
	}
	if len(preview.MergePreview) == 0 {
		t.Fatal("expected merge preview")
	}
	reportPreview := findMergePreview(preview.MergePreview, "docs/report.txt")
	if reportPreview == nil {
		t.Fatal("expected preview for docs/report.txt")
	}
	if reportPreview.Decision != syncpreview.DecisionConflict {
		t.Fatalf("unexpected merge decision: %s", reportPreview.Decision)
	}
}

func TestSyncCurrentUSBCommitsLocalChangesToDatabase(t *testing.T) {
	workRoot := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(filepath.Join(workRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workRoot, "docs", "new.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "USBSync.db")
	store, err := db.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.InitializeDeviceMeta("device-1", 1, "2026-03-30T00:00:00Z", 2); err != nil {
		t.Fatalf("initialize device meta: %v", err)
	}
	if err := store.UpsertMachine("m1", "Office", workRoot, "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	if err := store.UpdateMachineState("m1", 0, "", "", 1); err != nil {
		t.Fatalf("update machine state: %v", err)
	}
	store.Close()

	result, err := SyncCurrentUSB(SyncCurrentUSBRequest{
		WorkRoot:         workRoot,
		DatabasePath:     dbPath,
		MachineID:        "m1",
		LastSeenRevision: 0,
		SeenAt:           "2026-03-30T00:01:00Z",
	})
	if err != nil {
		t.Fatalf("sync current usb: %v", err)
	}

	if result.CommittedCount == 0 {
		t.Fatal("expected committed local changes")
	}

	store, err = db.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()

	entries, err := store.ListEntries()
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected stored entries after sync")
	}
}

func TestSyncCurrentUSBAppliesRemoteFileToLocalWorkRoot(t *testing.T) {
	workRoot := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatalf("mkdir work root: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "USBSync.db")
	store, err := db.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.InitializeDeviceMeta("device-1", 1, "2026-03-30T00:00:00Z", 2); err != nil {
		t.Fatalf("initialize device meta: %v", err)
	}
	if err := store.UpsertMachine("m1", "Local", workRoot, "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert local machine: %v", err)
	}
	if err := store.UpsertMachine("remote-1", "Office PC", `D:\work`, "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert remote machine: %v", err)
	}
	if err := store.UpdateMachineState("m1", 0, "", "", 1); err != nil {
		t.Fatalf("update machine state: %v", err)
	}
	const remoteBlobID = "39ee43d362145cff5c1cea14f0f39840"
	if err := store.StoreBlobChunks(remoteBlobID, [][]byte{[]byte("remote data")}); err != nil {
		t.Fatalf("store blob: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO entries(path_key, display_path, kind, deleted, last_revision, last_machine_id, updated_at)
		VALUES('docs', 'docs', 'dir', 0, 1, 'remote-1', '2026-03-30T00:00:00Z')
	`); err != nil {
		t.Fatalf("seed dir entry: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO entries(path_key, display_path, kind, size, mtime_ns, content_md5, blob_id, chunks, deleted, last_revision, last_machine_id, updated_at)
		VALUES('docs/a.txt', 'docs/a.txt', 'file', 11, 10, '', ?, 1, 0, 2, 'remote-1', '2026-03-30T00:00:00Z')
	`, remoteBlobID); err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO change_log(revision, machine_id, op, path_key, display_path, kind, base_revision, created_at)
		VALUES(1, 'remote-1', 'mkdir', 'docs', 'docs', 'dir', 0, '2026-03-30T00:00:30Z')
	`); err != nil {
		t.Fatalf("seed dir change log: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO change_log(revision, machine_id, op, path_key, display_path, kind, base_revision, size, mtime_ns, content_md5, blob_id, created_at)
		VALUES(2, 'remote-1', 'add', 'docs/a.txt', 'docs/a.txt', 'file', 0, 11, 10, '', ?, '2026-03-30T00:01:00Z')
	`, remoteBlobID); err != nil {
		t.Fatalf("seed change log: %v", err)
	}
	store.Close()

	result, err := SyncCurrentUSB(SyncCurrentUSBRequest{
		WorkRoot:         workRoot,
		DatabasePath:     dbPath,
		MachineID:        "m1",
		LastSeenRevision: 0,
		SeenAt:           "2026-03-30T00:02:00Z",
	})
	if err != nil {
		t.Fatalf("sync current usb: %v", err)
	}
	if result.CommittedCount != 0 {
		t.Fatalf("expected no local commits, got %d", result.CommittedCount)
	}

	data, err := os.ReadFile(filepath.Join(workRoot, "docs", "a.txt"))
	if err != nil {
		t.Fatalf("read applied file: %v", err)
	}
	if string(data) != "remote data" {
		t.Fatalf("unexpected file content: %s", string(data))
	}
}

func TestSyncCurrentUSBCreatesConflictCopyForDualModify(t *testing.T) {
	workRoot := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(filepath.Join(workRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	reportPath := filepath.Join(workRoot, "docs", "report.txt")
	if err := os.WriteFile(reportPath, []byte("local content"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "USBSync.db")
	store, err := db.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.InitializeDeviceMeta("device-1", 1, "2026-03-30T00:00:00Z", 2); err != nil {
		t.Fatalf("initialize device meta: %v", err)
	}
	if err := store.UpsertMachine("m1", "Local", workRoot, "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert local machine: %v", err)
	}
	if err := store.UpsertMachine("remote-1", "Office PC", `D:\work`, "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert remote machine: %v", err)
	}
	if err := store.UpdateMachineState("m1", 3, "", "", 1); err != nil {
		t.Fatalf("update machine state: %v", err)
	}
	if err := store.StoreBlobChunks("blob-remote", [][]byte{[]byte("remote version")}); err != nil {
		t.Fatalf("store blob: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO entries(path_key, display_path, kind, size, mtime_ns, content_md5, blob_id, chunks, deleted, last_revision, last_machine_id, updated_at)
		VALUES('docs/report.txt', 'docs/report.txt', 'file', 4, 1, 'old', 'old', 1, 0, 3, 'remote-1', '2026-03-30T00:00:00Z')
	`); err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO change_log(revision, machine_id, op, path_key, display_path, kind, base_revision, size, mtime_ns, content_md5, blob_id, created_at)
		VALUES(6, 'remote-1', 'modify', 'docs/report.txt', 'docs/report.txt', 'file', 3, 14, 10, 'blob-remote', 'blob-remote', '2026-03-30T00:01:00Z')
	`); err != nil {
		t.Fatalf("seed change log: %v", err)
	}
	store.Close()

	result, err := SyncCurrentUSB(SyncCurrentUSBRequest{
		WorkRoot:         workRoot,
		DatabasePath:     dbPath,
		MachineID:        "m1",
		LastSeenRevision: 3,
		SeenAt:           "2026-03-30T00:02:00Z",
	})
	if err != nil {
		t.Fatalf("sync current usb: %v", err)
	}
	if result.CommittedCount == 0 {
		t.Fatal("expected local change to be committed")
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read original file: %v", err)
	}
	if string(data) != "local content" {
		t.Fatalf("unexpected original file content: %s", string(data))
	}

	conflictPath := filepath.Join(workRoot, "docs", "report (conflict-office-pc-r6).txt")
	conflictData, err := os.ReadFile(conflictPath)
	if err != nil {
		t.Fatalf("read conflict file: %v", err)
	}
	if string(conflictData) != "remote version" {
		t.Fatalf("unexpected conflict file content: %s", string(conflictData))
	}
}

func TestSyncCurrentUSBReportsProgressEvents(t *testing.T) {
	workRoot := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(filepath.Join(workRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workRoot, "docs", "new.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "USBSync.db")
	store, err := db.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.InitializeDeviceMeta("device-1", 1, "2026-03-30T00:00:00Z", 2); err != nil {
		t.Fatalf("initialize device meta: %v", err)
	}
	if err := store.UpsertMachine("m1", "Office", workRoot, "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	if err := store.UpdateMachineState("m1", 0, "", "", 1); err != nil {
		t.Fatalf("update machine state: %v", err)
	}
	store.Close()

	var events []syncpreview.Event
	_, err = SyncCurrentUSB(SyncCurrentUSBRequest{
		WorkRoot:         workRoot,
		DatabasePath:     dbPath,
		MachineID:        "m1",
		LastSeenRevision: 0,
		SeenAt:           "2026-03-30T00:01:00Z",
		Progress: func(event syncpreview.Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("sync current usb: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected progress events")
	}
}

func TestSyncCurrentUSBReportsOperationTypeInProgress(t *testing.T) {
	workRoot := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(filepath.Join(workRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workRoot, "docs", "new.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "USBSync.db")
	store, err := db.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.InitializeDeviceMeta("device-1", 1, "2026-03-30T00:00:00Z", 2); err != nil {
		t.Fatalf("initialize device meta: %v", err)
	}
	if err := store.UpsertMachine("m1", "Office", workRoot, "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	if err := store.UpdateMachineState("m1", 0, "", "", 1); err != nil {
		t.Fatalf("update machine state: %v", err)
	}
	store.Close()

	result, err := SyncCurrentUSB(SyncCurrentUSBRequest{
		WorkRoot:         workRoot,
		DatabasePath:     dbPath,
		MachineID:        "m1",
		LastSeenRevision: 0,
		SeenAt:           "2026-03-30T00:01:00Z",
	})
	if err != nil {
		t.Fatalf("sync current usb: %v", err)
	}

	found := false
	for _, event := range result.Progress {
		if event.Phase == "commit" && event.Item == "docs/new.txt" {
			found = true
			if event.Status != "新增" {
				t.Fatalf("expected add status, got %s", event.Status)
			}
		}
	}
	if !found {
		t.Fatal("expected commit progress row for docs/new.txt")
	}
}

func TestInitializeActionCreatesBackupAndEnablesSync(t *testing.T) {
	workRoot := filepath.Join(t.TempDir(), "work")
	backupDir := filepath.Join(t.TempDir(), "backup")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatalf("mkdir work root: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "machine.json")
	controller := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			return usb.DriveProbe{
				ExePath:     exePath,
				IsRemovable: true,
				VolumeID:    "VOL001",
			}, nil
		},
		func() time.Time { return time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC) },
		func() (string, error) { return "Office", nil },
	)
	controller.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-office", Hostname: "Office"}, nil
	}

	result := controller.handleInitialize(ui.FormState{
		WorkRoot:    workRoot,
		DisplayName: "Office",
		BackupDir:   backupDir,
	}, nil)

	if !result.SyncEnabled {
		t.Fatal("expected sync button enabled after initialize")
	}
	if result.InitializeEnabled {
		t.Fatal("expected initialize button disabled after initialize")
	}
	if len(result.ProgressRows) == 0 {
		t.Fatal("expected initialize progress rows")
	}
	if len(result.Results.Failures) != 0 {
		t.Fatalf("unexpected failures: %#v", result.Results.Failures)
	}

	backupPath := filepath.Join(backupDir, latestBackupFileName)
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("expected latest backup: %v", err)
	}

	cfg, err := config.LoadMachineConfigForMachine(cfgPath, "hw-office")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.MachineID == "" {
		t.Fatal("expected machine id to be persisted")
	}
}

func TestSyncActionReturnsProgressAndRefreshesBackup(t *testing.T) {
	workRoot := filepath.Join(t.TempDir(), "work")
	backupDir := filepath.Join(t.TempDir(), "backup")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatalf("mkdir work root: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "machine.json")
	controller := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			return usb.DriveProbe{
				ExePath:     exePath,
				IsRemovable: true,
				VolumeID:    "VOL002",
			}, nil
		},
		func() time.Time { return time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC) },
		func() (string, error) { return "Office", nil },
	)
	controller.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-office", Hostname: "Office"}, nil
	}

	initResult := controller.handleInitialize(ui.FormState{
		WorkRoot:    workRoot,
		DisplayName: "Office",
		BackupDir:   backupDir,
	}, nil)
	if len(initResult.Results.Failures) != 0 {
		t.Fatalf("unexpected initialize failures: %#v", initResult.Results.Failures)
	}

	if err := os.MkdirAll(filepath.Join(workRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workRoot, "docs", "sync.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	firstSync := controller.handleSync(ui.FormState{
		WorkRoot:    workRoot,
		DisplayName: "Office",
		BackupDir:   backupDir,
	}, nil)
	if len(firstSync.Results.Failures) != 0 {
		t.Fatalf("unexpected sync failures: %#v", firstSync.Results.Failures)
	}
	if !strings.Contains(firstSync.Results.Status, "同步完成") {
		t.Fatalf("unexpected sync status: %s", firstSync.Results.Status)
	}
	if len(firstSync.ProgressRows) == 0 {
		t.Fatal("expected sync progress rows")
	}

	if err := os.WriteFile(filepath.Join(workRoot, "docs", "sync.txt"), []byte("hello again"), 0o644); err != nil {
		t.Fatalf("rewrite file: %v", err)
	}
	secondSync := controller.handleSync(ui.FormState{
		WorkRoot:    workRoot,
		DisplayName: "Office",
		BackupDir:   backupDir,
	}, nil)
	if len(secondSync.Results.Failures) != 0 {
		t.Fatalf("unexpected second sync failures: %#v", secondSync.Results.Failures)
	}

	if _, err := os.Stat(filepath.Join(backupDir, latestBackupFileName)); err != nil {
		t.Fatalf("expected latest backup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, prevBackupFileName)); err != nil {
		t.Fatalf("expected previous backup: %v", err)
	}
}

func TestBuildSyncSummaryIncludesAddModifyDeleteBreakdown(t *testing.T) {
	summary := buildSyncSummary(SyncCurrentUSBResult{
		LocalChanges: []syncpreview.LocalChange{
			{Op: "add", DisplayPath: "docs/new.txt"},
			{Op: "modify", DisplayPath: "docs/edit.txt"},
			{Op: "delete", DisplayPath: "docs/old.txt"},
		},
		CommittedCount: 3,
	})

	if !strings.Contains(summary.Status, "新增 1 项") {
		t.Fatalf("expected add breakdown, got %s", summary.Status)
	}
	if !strings.Contains(summary.Status, "修改 1 项") {
		t.Fatalf("expected modify breakdown, got %s", summary.Status)
	}
	if !strings.Contains(summary.Status, "删除 1 项") {
		t.Fatalf("expected delete breakdown, got %s", summary.Status)
	}
}

func TestSyncActionRequiresReinitializeAfterWorkRootChange(t *testing.T) {
	oldWorkRoot := filepath.Join(t.TempDir(), "old-work")
	newWorkRoot := filepath.Join(t.TempDir(), "new-work")
	backupDir := filepath.Join(t.TempDir(), "backup")
	for _, dir := range []string{oldWorkRoot, newWorkRoot, backupDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "usbsync.json")
	controller := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			return usb.DriveProbe{
				ExePath:     exePath,
				IsRemovable: true,
				VolumeID:    "VOL009",
			}, nil
		},
		func() time.Time { return time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC) },
		func() (string, error) { return "Office", nil },
	)
	controller.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-office", Hostname: "Office"}, nil
	}

	initResult := controller.handleInitialize(ui.FormState{
		WorkRoot:    oldWorkRoot,
		DisplayName: "Office",
		BackupDir:   backupDir,
	}, nil)
	if len(initResult.Results.Failures) != 0 {
		t.Fatalf("unexpected initialize failures: %#v", initResult.Results.Failures)
	}

	result := controller.handleSync(ui.FormState{
		WorkRoot:    newWorkRoot,
		DisplayName: "Office",
		BackupDir:   backupDir,
	}, nil)

	if !result.InitializeEnabled {
		t.Fatal("expected initialize enabled")
	}
	if result.SyncEnabled {
		t.Fatal("expected sync disabled")
	}
	if result.Results.Status != "工作文件夹已更换，请重新初始化当前 U 盘" {
		t.Fatalf("unexpected status: %s", result.Results.Status)
	}
}

func TestInitializeActionRebuildsDatabaseAfterWorkRootChange(t *testing.T) {
	oldWorkRoot := filepath.Join(t.TempDir(), "old-work")
	newWorkRoot := filepath.Join(t.TempDir(), "new-work")
	backupDir := filepath.Join(t.TempDir(), "backup")
	for _, dir := range []string{oldWorkRoot, newWorkRoot, backupDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "usbsync.json")
	controller := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			return usb.DriveProbe{
				ExePath:     exePath,
				IsRemovable: true,
				VolumeID:    "VOL010",
			}, nil
		},
		func() time.Time { return time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC) },
		func() (string, error) { return "Office", nil },
	)
	controller.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-office", Hostname: "Office"}, nil
	}

	initResult := controller.handleInitialize(ui.FormState{
		WorkRoot:    oldWorkRoot,
		DisplayName: "Office",
		BackupDir:   backupDir,
	}, nil)
	if len(initResult.Results.Failures) != 0 {
		t.Fatalf("unexpected initialize failures: %#v", initResult.Results.Failures)
	}

	if err := os.WriteFile(filepath.Join(oldWorkRoot, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	syncResult := controller.handleSync(ui.FormState{
		WorkRoot:    oldWorkRoot,
		DisplayName: "Office",
		BackupDir:   backupDir,
	}, nil)
	if len(syncResult.Results.Failures) != 0 {
		t.Fatalf("unexpected sync failures: %#v", syncResult.Results.Failures)
	}

	reinitResult := controller.handleInitialize(ui.FormState{
		WorkRoot:    newWorkRoot,
		DisplayName: "Office",
		BackupDir:   backupDir,
	}, nil)
	if len(reinitResult.Results.Failures) != 0 {
		t.Fatalf("unexpected reinitialize failures: %#v", reinitResult.Results.Failures)
	}

	drive, err := usb.BuildDriveContext(usb.DriveProbe{
		ExePath:     exePath,
		IsRemovable: true,
		VolumeID:    "VOL010",
	})
	if err != nil {
		t.Fatalf("build drive context: %v", err)
	}
	store, err := db.OpenStore(drive.DBPath)
	if err != nil {
		t.Fatalf("open rebuilt store: %v", err)
	}
	defer store.Close()

	latestRevision, err := store.GetLatestRevision()
	if err != nil {
		t.Fatalf("latest revision: %v", err)
	}
	if latestRevision != 0 {
		t.Fatalf("expected rebuilt database to start clean, got revision %d", latestRevision)
	}

	machines, err := store.ListMachines()
	if err != nil {
		t.Fatalf("list machines: %v", err)
	}
	if len(machines) != 1 || machines[0].LastWorkRoot != newWorkRoot {
		t.Fatalf("unexpected rebuilt machine state: %#v", machines)
	}

	if _, err := os.Stat(filepath.Join(backupDir, latestBackupFileName)); err != nil {
		t.Fatalf("expected old database backup: %v", err)
	}
}

func TestRefreshLocalBackupRotatesPreviousCopy(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "USBSync.db")
	backupDir := filepath.Join(t.TempDir(), "backup")
	if err := os.WriteFile(dbPath, []byte("first"), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}

	if err := refreshLocalBackup(dbPath, backupDir); err != nil {
		t.Fatalf("first backup: %v", err)
	}
	if err := os.WriteFile(dbPath, []byte("second"), 0o644); err != nil {
		t.Fatalf("rewrite db: %v", err)
	}
	if err := refreshLocalBackup(dbPath, backupDir); err != nil {
		t.Fatalf("second backup: %v", err)
	}

	latest, err := os.ReadFile(filepath.Join(backupDir, latestBackupFileName))
	if err != nil {
		t.Fatalf("read latest backup: %v", err)
	}
	prev, err := os.ReadFile(filepath.Join(backupDir, prevBackupFileName))
	if err != nil {
		t.Fatalf("read previous backup: %v", err)
	}
	if string(latest) != "second" {
		t.Fatalf("unexpected latest backup: %s", string(latest))
	}
	if string(prev) != "first" {
		t.Fatalf("unexpected previous backup: %s", string(prev))
	}
}

func TestDraftChangePersistsFormValuesWithoutInitialize(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "usbsync.json")
	workRoot := filepath.Join(t.TempDir(), "work")
	backupDir := filepath.Join(t.TempDir(), "backup")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatalf("mkdir work root: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}
	controller := newRuntimeController(
		cfgPath,
		nil,
		func() time.Time { return time.Unix(0, 0) },
		func() (string, error) { return "Office", nil },
	)
	controller.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-office", Hostname: "Office"}, nil
	}

	controller.handleDraftChanged(ui.FormState{
		WorkRoot:    workRoot,
		DisplayName: "Office-PC",
		BackupDir:   backupDir,
	})

	cfg, err := config.LoadMachineConfigForMachine(cfgPath, "hw-office")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.LastWorkRoot == "" || cfg.LastWorkRoot != filepath.Clean(cfg.LastWorkRoot) {
		t.Fatalf("unexpected work root: %s", cfg.LastWorkRoot)
	}
	if cfg.DisplayName != "Office-PC" {
		t.Fatalf("unexpected display name: %s", cfg.DisplayName)
	}
	if cfg.BackupDir == "" || cfg.BackupDir != filepath.Clean(cfg.BackupDir) {
		t.Fatalf("unexpected backup dir: %s", cfg.BackupDir)
	}
}

func TestEnsureDraftFileCreatesPortableConfigFile(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "usbsync.json")
	controller := newRuntimeController(
		cfgPath,
		nil,
		func() time.Time { return time.Unix(0, 0) },
		func() (string, error) { return "Office", nil },
	)
	controller.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-office", Hostname: "Office"}, nil
	}

	controller.ensureDraftFile(config.MachineConfig{
		DisplayName:       "Office",
		LastWorkRoot:      filepath.Join(t.TempDir(), "work"),
		BackupDir:         filepath.Join(t.TempDir(), "backup"),
		MachineConfigPath: cfgPath,
		StateDir:          filepath.Dir(cfgPath),
	})

	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected config file: %v", err)
	}

	cfg, err := config.LoadMachineConfigForMachine(cfgPath, "hw-office")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.DisplayName != "Office" {
		t.Fatalf("unexpected display name: %s", cfg.DisplayName)
	}
}

func TestDraftChangeDoesNotSaveInvalidFolderPath(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "usbsync.json")
	workRoot := filepath.Join(t.TempDir(), "work")
	backupDir := filepath.Join(t.TempDir(), "backup")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatalf("mkdir work root: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}

	controller := newRuntimeController(
		cfgPath,
		nil,
		func() time.Time { return time.Unix(0, 0) },
		func() (string, error) { return "Office", nil },
	)
	controller.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-office", Hostname: "Office"}, nil
	}

	controller.handleDraftChanged(ui.FormState{
		WorkRoot:    workRoot,
		DisplayName: "Office",
		BackupDir:   backupDir,
	})
	controller.handleDraftChanged(ui.FormState{
		WorkRoot:    `bad\path`,
		DisplayName: "Office",
		BackupDir:   backupDir,
	})

	cfg, err := config.LoadMachineConfigForMachine(cfgPath, "hw-office")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.LastWorkRoot != workRoot {
		t.Fatalf("invalid path should not overwrite saved value: %s", cfg.LastWorkRoot)
	}
}

func TestDraftChangeAllowsMissingBackupFolder(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "usbsync.json")
	workRoot := filepath.Join(t.TempDir(), "work")
	backupDir := filepath.Join(t.TempDir(), "backup-new", "nested", "level")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatalf("mkdir work root: %v", err)
	}

	controller := newRuntimeController(
		cfgPath,
		nil,
		func() time.Time { return time.Unix(0, 0) },
		func() (string, error) { return "Office", nil },
	)
	controller.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-office", Hostname: "Office"}, nil
	}

	controller.handleDraftChanged(ui.FormState{
		WorkRoot:    workRoot,
		DisplayName: "Office",
		BackupDir:   backupDir,
	})

	cfg, err := config.LoadMachineConfigForMachine(cfgPath, "hw-office")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.BackupDir != backupDir {
		t.Fatalf("unexpected backup dir: %s", cfg.BackupDir)
	}
}

func TestInitializeActionCreatesMissingBackupDirectory(t *testing.T) {
	workRoot := filepath.Join(t.TempDir(), "work")
	backupDir := filepath.Join(t.TempDir(), "backup-new", "nested", "level")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatalf("mkdir work root: %v", err)
	}
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "machine.json")
	controller := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			return usb.DriveProbe{
				ExePath:     exePath,
				IsRemovable: true,
				VolumeID:    "VOL004",
			}, nil
		},
		func() time.Time { return time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC) },
		func() (string, error) { return "Office", nil },
	)
	controller.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-office", Hostname: "Office"}, nil
	}

	result := controller.handleInitialize(ui.FormState{
		WorkRoot:    workRoot,
		DisplayName: "Office",
		BackupDir:   backupDir,
	}, nil)

	if len(result.Results.Failures) != 0 {
		t.Fatalf("unexpected initialize failures: %#v", result.Results.Failures)
	}
	if info, err := os.Stat(backupDir); err != nil || !info.IsDir() {
		t.Fatalf("expected backup dir to be created, got err=%v info=%#v", err, info)
	}
}

func TestSyncActionAllowsNewComputerToPullFromExistingUSB(t *testing.T) {
	workRoot := filepath.Join(t.TempDir(), "work")
	backupDir := filepath.Join(t.TempDir(), "backup")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatalf("mkdir work root: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	drive, err := usb.BuildDriveContext(usb.DriveProbe{
		ExePath:     exePath,
		IsRemovable: true,
		VolumeID:    "VOL005",
	})
	if err != nil {
		t.Fatalf("build drive context: %v", err)
	}

	store, err := db.OpenStore(drive.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.InitializeDeviceMeta("device-usb", 1, "2026-03-30T00:00:00Z", 2); err != nil {
		t.Fatalf("initialize device meta: %v", err)
	}
	if err := store.UpsertMachine("remote-1", "Office", `D:\office`, "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert remote machine: %v", err)
	}
	const remoteBlobID = "39ee43d362145cff5c1cea14f0f39840"
	if err := store.StoreBlobChunks(remoteBlobID, [][]byte{[]byte("remote data")}); err != nil {
		t.Fatalf("store blob: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO entries(path_key, display_path, kind, deleted, last_revision, last_machine_id, updated_at)
		VALUES('docs', 'docs', 'dir', 0, 1, 'remote-1', '2026-03-30T00:00:00Z')
	`); err != nil {
		t.Fatalf("seed dir entry: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO entries(path_key, display_path, kind, size, mtime_ns, content_md5, blob_id, chunks, deleted, last_revision, last_machine_id, updated_at)
		VALUES('docs/a.txt', 'docs/a.txt', 'file', 11, 10, '', ?, 1, 0, 1, 'remote-1', '2026-03-30T00:00:00Z')
	`, remoteBlobID); err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO change_log(revision, machine_id, op, path_key, display_path, kind, base_revision, created_at)
		VALUES(1, 'remote-1', 'mkdir', 'docs', 'docs', 'dir', 0, '2026-03-30T00:00:30Z')
	`); err != nil {
		t.Fatalf("seed dir change log: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO change_log(revision, machine_id, op, path_key, display_path, kind, base_revision, size, mtime_ns, content_md5, blob_id, created_at)
		VALUES(2, 'remote-1', 'add', 'docs/a.txt', 'docs/a.txt', 'file', 0, 11, 10, '', ?, '2026-03-30T00:01:00Z')
	`, remoteBlobID); err != nil {
		t.Fatalf("seed change log: %v", err)
	}
	store.Close()

	cfgPath := filepath.Join(t.TempDir(), "usbsync.json")
	controller := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			return usb.DriveProbe{
				ExePath:     exePath,
				IsRemovable: true,
				VolumeID:    "VOL005",
			}, nil
		},
		func() time.Time { return time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC) },
		func() (string, error) { return "MININT-NEW", nil },
	)
	controller.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-new", Hostname: "MININT-NEW"}, nil
	}

	result := controller.handleSync(ui.FormState{
		WorkRoot:    workRoot,
		DisplayName: "Home",
		BackupDir:   backupDir,
	}, nil)

	if len(result.Results.Failures) != 0 {
		t.Fatalf("unexpected sync failures: %#v", result.Results.Failures)
	}
	if !strings.Contains(result.Results.Status, "同步完成") {
		t.Fatalf("unexpected status: %s", result.Results.Status)
	}
	data, err := os.ReadFile(filepath.Join(workRoot, "docs", "a.txt"))
	if err != nil {
		t.Fatalf("read synced file: %v", err)
	}
	if string(data) != "remote data" {
		t.Fatalf("unexpected synced content: %s", string(data))
	}

	cfg, err := config.LoadMachineConfigForMachine(cfgPath, "hw-new")
	if err != nil {
		t.Fatalf("load new machine config: %v", err)
	}
	if cfg.MachineID == "" {
		t.Fatal("expected machine id to be created for new computer")
	}
	if cfg.BoundDeviceID != "device-usb" {
		t.Fatalf("unexpected bound device id: %s", cfg.BoundDeviceID)
	}
}

func TestSecondSyncAfterInitialPullDoesNotWriteEverythingBack(t *testing.T) {
	workRoot := filepath.Join(t.TempDir(), "work")
	backupDir := filepath.Join(t.TempDir(), "backup")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatalf("mkdir work root: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	drive, err := usb.BuildDriveContext(usb.DriveProbe{
		ExePath:     exePath,
		IsRemovable: true,
		VolumeID:    "VOL006",
	})
	if err != nil {
		t.Fatalf("build drive context: %v", err)
	}

	store, err := db.OpenStore(drive.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.InitializeDeviceMeta("device-usb", 1, "2026-03-30T00:00:00Z", 2); err != nil {
		t.Fatalf("initialize device meta: %v", err)
	}
	if err := store.UpsertMachine("remote-1", "Office", `D:\office`, "2026-03-30T00:00:00Z"); err != nil {
		t.Fatalf("upsert remote machine: %v", err)
	}
	const remoteBlobID = "39ee43d362145cff5c1cea14f0f39840"
	if err := store.StoreBlobChunks(remoteBlobID, [][]byte{[]byte("remote data")}); err != nil {
		t.Fatalf("store blob: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO entries(path_key, display_path, kind, deleted, last_revision, last_machine_id, updated_at)
		VALUES('docs', 'docs', 'dir', 0, 1, 'remote-1', '2026-03-30T00:00:00Z')
	`); err != nil {
		t.Fatalf("seed dir entry: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO entries(path_key, display_path, kind, size, mtime_ns, content_md5, blob_id, chunks, deleted, last_revision, last_machine_id, updated_at)
		VALUES('docs/a.txt', 'docs/a.txt', 'file', 11, 10, '', ?, 1, 0, 2, 'remote-1', '2026-03-30T00:00:00Z')
	`, remoteBlobID); err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO change_log(revision, machine_id, op, path_key, display_path, kind, base_revision, created_at)
		VALUES(1, 'remote-1', 'mkdir', 'docs', 'docs', 'dir', 0, '2026-03-30T00:00:30Z')
	`); err != nil {
		t.Fatalf("seed dir change log: %v", err)
	}
	if _, err := store.DB.Exec(`
		INSERT INTO change_log(revision, machine_id, op, path_key, display_path, kind, base_revision, size, mtime_ns, content_md5, blob_id, created_at)
		VALUES(2, 'remote-1', 'add', 'docs/a.txt', 'docs/a.txt', 'file', 0, 11, 10, '', ?, '2026-03-30T00:01:00Z')
	`, remoteBlobID); err != nil {
		t.Fatalf("seed change log: %v", err)
	}
	store.Close()

	cfgPath := filepath.Join(t.TempDir(), "usbsync.json")
	controller := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			return usb.DriveProbe{
				ExePath:     exePath,
				IsRemovable: true,
				VolumeID:    "VOL006",
			}, nil
		},
		func() time.Time { return time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC) },
		func() (string, error) { return "MININT-NEW", nil },
	)
	controller.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-new", Hostname: "MININT-NEW"}, nil
	}

	first := controller.handleSync(ui.FormState{
		WorkRoot:    workRoot,
		DisplayName: "Home",
		BackupDir:   backupDir,
	}, nil)
	if len(first.Results.Failures) != 0 {
		t.Fatalf("unexpected first sync failures: %#v", first.Results.Failures)
	}

	second := controller.handleSync(ui.FormState{
		WorkRoot:    workRoot,
		DisplayName: "Home",
		BackupDir:   backupDir,
	}, nil)
	if len(second.Results.Failures) != 0 {
		t.Fatalf("unexpected second sync failures: %#v", second.Results.Failures)
	}
	if second.Results.Status != "同步完成，没有发现新变化" {
		t.Fatalf("unexpected second sync status: %s", second.Results.Status)
	}

	store, err = db.OpenStore(drive.DBPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()

	latestRevision, err := store.GetLatestRevision()
	if err != nil {
		t.Fatalf("get latest revision: %v", err)
	}
	if latestRevision != 2 {
		t.Fatalf("expected no extra revision after second sync, got %d", latestRevision)
	}
}

func TestFileModifiedOnBReturnsToAWithoutConflict(t *testing.T) {
	workRootA := filepath.Join(t.TempDir(), "work-a")
	workRootB := filepath.Join(t.TempDir(), "work-b")
	backupA := filepath.Join(t.TempDir(), "backup-a")
	backupB := filepath.Join(t.TempDir(), "backup-b")
	for _, dir := range []string{workRootA, workRootB, backupA, backupB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	cfgPath := filepath.Join(t.TempDir(), "usbsync.json")
	now := func() time.Time { return time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC) }

	controllerA := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			return usb.DriveProbe{ExePath: exePath, IsRemovable: true, VolumeID: "VOL007"}, nil
		},
		now,
		func() (string, error) { return "PC-A", nil },
	)
	controllerA.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-a", Hostname: "PC-A"}, nil
	}

	controllerB := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			return usb.DriveProbe{ExePath: exePath, IsRemovable: true, VolumeID: "VOL007"}, nil
		},
		now,
		func() (string, error) { return "PC-B", nil },
	)
	controllerB.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-b", Hostname: "PC-B"}, nil
	}

	if err := os.MkdirAll(filepath.Join(workRootA, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	originalPath := filepath.Join(workRootA, "docs", "note.txt")
	if err := os.WriteFile(originalPath, []byte("from-a"), 0o644); err != nil {
		t.Fatalf("write original file: %v", err)
	}

	initA := controllerA.handleInitialize(ui.FormState{
		WorkRoot:    workRootA,
		DisplayName: "A",
		BackupDir:   backupA,
	}, nil)
	if len(initA.Results.Failures) != 0 {
		t.Fatalf("unexpected A initialize failures: %#v", initA.Results.Failures)
	}
	syncA := controllerA.handleSync(ui.FormState{
		WorkRoot:    workRootA,
		DisplayName: "A",
		BackupDir:   backupA,
	}, nil)
	if len(syncA.Results.Failures) != 0 {
		t.Fatalf("unexpected A sync failures: %#v", syncA.Results.Failures)
	}

	syncB1 := controllerB.handleSync(ui.FormState{
		WorkRoot:    workRootB,
		DisplayName: "B",
		BackupDir:   backupB,
	}, nil)
	if len(syncB1.Results.Failures) != 0 {
		t.Fatalf("unexpected first B sync failures: %#v", syncB1.Results.Failures)
	}

	bPath := filepath.Join(workRootB, "docs", "note.txt")
	if err := os.WriteFile(bPath, []byte("changed-on-b"), 0o644); err != nil {
		t.Fatalf("rewrite B file: %v", err)
	}

	syncB2 := controllerB.handleSync(ui.FormState{
		WorkRoot:    workRootB,
		DisplayName: "B",
		BackupDir:   backupB,
	}, nil)
	if len(syncB2.Results.Failures) != 0 {
		t.Fatalf("unexpected second B sync failures: %#v", syncB2.Results.Failures)
	}
	if len(syncB2.Results.Conflicts) != 0 {
		t.Fatalf("unexpected B conflicts: %#v", syncB2.Results.Conflicts)
	}

	syncA2 := controllerA.handleSync(ui.FormState{
		WorkRoot:    workRootA,
		DisplayName: "A",
		BackupDir:   backupA,
	}, nil)
	if len(syncA2.Results.Failures) != 0 {
		t.Fatalf("unexpected second A sync failures: %#v", syncA2.Results.Failures)
	}
	if len(syncA2.Results.Conflicts) != 0 {
		t.Fatalf("unexpected A conflicts: %#v", syncA2.Results.Conflicts)
	}

	data, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("read A file: %v", err)
	}
	if string(data) != "changed-on-b" {
		t.Fatalf("unexpected A file content: %s", string(data))
	}
}

func TestFileDeletedOnBDeletesOnAWithoutReupload(t *testing.T) {
	workRootA := filepath.Join(t.TempDir(), "work-a")
	workRootB := filepath.Join(t.TempDir(), "work-b")
	backupA := filepath.Join(t.TempDir(), "backup-a")
	backupB := filepath.Join(t.TempDir(), "backup-b")
	for _, dir := range []string{workRootA, workRootB, backupA, backupB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	cfgPath := filepath.Join(t.TempDir(), "usbsync.json")
	now := func() time.Time { return time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC) }

	controllerA := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			return usb.DriveProbe{ExePath: exePath, IsRemovable: true, VolumeID: "VOL008"}, nil
		},
		now,
		func() (string, error) { return "PC-A", nil },
	)
	controllerA.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-a", Hostname: "PC-A"}, nil
	}

	controllerB := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			return usb.DriveProbe{ExePath: exePath, IsRemovable: true, VolumeID: "VOL008"}, nil
		},
		now,
		func() (string, error) { return "PC-B", nil },
	)
	controllerB.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-b", Hostname: "PC-B"}, nil
	}

	if err := os.MkdirAll(filepath.Join(workRootA, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	originalPath := filepath.Join(workRootA, "docs", "note.txt")
	if err := os.WriteFile(originalPath, []byte("from-a"), 0o644); err != nil {
		t.Fatalf("write original file: %v", err)
	}

	initA := controllerA.handleInitialize(ui.FormState{
		WorkRoot:    workRootA,
		DisplayName: "A",
		BackupDir:   backupA,
	}, nil)
	if len(initA.Results.Failures) != 0 {
		t.Fatalf("unexpected A initialize failures: %#v", initA.Results.Failures)
	}
	syncA := controllerA.handleSync(ui.FormState{
		WorkRoot:    workRootA,
		DisplayName: "A",
		BackupDir:   backupA,
	}, nil)
	if len(syncA.Results.Failures) != 0 {
		t.Fatalf("unexpected A sync failures: %#v", syncA.Results.Failures)
	}

	syncB1 := controllerB.handleSync(ui.FormState{
		WorkRoot:    workRootB,
		DisplayName: "B",
		BackupDir:   backupB,
	}, nil)
	if len(syncB1.Results.Failures) != 0 {
		t.Fatalf("unexpected first B sync failures: %#v", syncB1.Results.Failures)
	}

	bPath := filepath.Join(workRootB, "docs", "note.txt")
	if err := os.Remove(bPath); err != nil {
		t.Fatalf("remove B file: %v", err)
	}

	syncB2 := controllerB.handleSync(ui.FormState{
		WorkRoot:    workRootB,
		DisplayName: "B",
		BackupDir:   backupB,
	}, nil)
	if len(syncB2.Results.Failures) != 0 {
		t.Fatalf("unexpected second B sync failures: %#v", syncB2.Results.Failures)
	}
	if len(syncB2.Results.Conflicts) != 0 {
		t.Fatalf("unexpected B conflicts: %#v", syncB2.Results.Conflicts)
	}

	syncA2 := controllerA.handleSync(ui.FormState{
		WorkRoot:    workRootA,
		DisplayName: "A",
		BackupDir:   backupA,
	}, nil)
	if len(syncA2.Results.Failures) != 0 {
		t.Fatalf("unexpected second A sync failures: %#v", syncA2.Results.Failures)
	}
	if len(syncA2.Results.Conflicts) != 0 {
		t.Fatalf("unexpected A conflicts: %#v", syncA2.Results.Conflicts)
	}
	if _, err := os.Stat(originalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected A file to be deleted, got err=%v", err)
	}

	store, err := db.OpenStore(filepath.Join(exeDir, "USBSync.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	records, err := store.ListEntries()
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected docs dir and deleted file entry, got %#v", records)
	}
	var deletedFile *db.EntryRecord
	for i := range records {
		if records[i].PathKey == "docs/note.txt" {
			deletedFile = &records[i]
			break
		}
	}
	if deletedFile == nil {
		t.Fatalf("expected deleted file record, got %#v", records)
	}
	if !deletedFile.Deleted {
		t.Fatalf("expected file to stay deleted in db, got %#v", *deletedFile)
	}
}

func TestFileDeletedOnBDeletesOnAWithoutReuploadWhenCacheMissing(t *testing.T) {
	workRootA := filepath.Join(t.TempDir(), "work-a")
	workRootB := filepath.Join(t.TempDir(), "work-b")
	backupA := filepath.Join(t.TempDir(), "backup-a")
	backupB := filepath.Join(t.TempDir(), "backup-b")
	for _, dir := range []string{workRootA, workRootB, backupA, backupB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	cfgPath := filepath.Join(t.TempDir(), "usbsync.json")
	now := func() time.Time { return time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC) }

	controllerA := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			return usb.DriveProbe{ExePath: exePath, IsRemovable: true, VolumeID: "VOL009"}, nil
		},
		now,
		func() (string, error) { return "PC-A", nil },
	)
	controllerA.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-a", Hostname: "PC-A"}, nil
	}

	controllerB := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			return usb.DriveProbe{ExePath: exePath, IsRemovable: true, VolumeID: "VOL009"}, nil
		},
		now,
		func() (string, error) { return "PC-B", nil },
	)
	controllerB.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-b", Hostname: "PC-B"}, nil
	}

	if err := os.MkdirAll(filepath.Join(workRootA, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	originalPath := filepath.Join(workRootA, "docs", "note.txt")
	if err := os.WriteFile(originalPath, []byte("from-a"), 0o644); err != nil {
		t.Fatalf("write original file: %v", err)
	}

	initA := controllerA.handleInitialize(ui.FormState{
		WorkRoot:    workRootA,
		DisplayName: "A",
		BackupDir:   backupA,
	}, nil)
	if len(initA.Results.Failures) != 0 {
		t.Fatalf("unexpected A initialize failures: %#v", initA.Results.Failures)
	}
	syncA := controllerA.handleSync(ui.FormState{
		WorkRoot:    workRootA,
		DisplayName: "A",
		BackupDir:   backupA,
	}, nil)
	if len(syncA.Results.Failures) != 0 {
		t.Fatalf("unexpected A sync failures: %#v", syncA.Results.Failures)
	}

	syncB1 := controllerB.handleSync(ui.FormState{
		WorkRoot:    workRootB,
		DisplayName: "B",
		BackupDir:   backupB,
	}, nil)
	if len(syncB1.Results.Failures) != 0 {
		t.Fatalf("unexpected first B sync failures: %#v", syncB1.Results.Failures)
	}

	bPath := filepath.Join(workRootB, "docs", "note.txt")
	if err := os.Remove(bPath); err != nil {
		t.Fatalf("remove B file: %v", err)
	}
	syncB2 := controllerB.handleSync(ui.FormState{
		WorkRoot:    workRootB,
		DisplayName: "B",
		BackupDir:   backupB,
	}, nil)
	if len(syncB2.Results.Failures) != 0 {
		t.Fatalf("unexpected second B sync failures: %#v", syncB2.Results.Failures)
	}

	if err := os.Remove(fileindex.DefaultCachePath(workRootA)); err != nil {
		t.Fatalf("remove A cache: %v", err)
	}

	syncA2 := controllerA.handleSync(ui.FormState{
		WorkRoot:    workRootA,
		DisplayName: "A",
		BackupDir:   backupA,
	}, nil)
	if len(syncA2.Results.Failures) != 0 {
		t.Fatalf("unexpected second A sync failures: %#v", syncA2.Results.Failures)
	}
	if len(syncA2.Results.Conflicts) != 0 {
		t.Fatalf("unexpected A conflicts: %#v", syncA2.Results.Conflicts)
	}
	if _, err := os.Stat(originalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected A file to be deleted, got err=%v", err)
	}
}

func TestFileModifiedOnBReturnsToAWithoutConflictWhenCacheMissing(t *testing.T) {
	workRootA := filepath.Join(t.TempDir(), "work-a")
	workRootB := filepath.Join(t.TempDir(), "work-b")
	backupA := filepath.Join(t.TempDir(), "backup-a")
	backupB := filepath.Join(t.TempDir(), "backup-b")
	for _, dir := range []string{workRootA, workRootB, backupA, backupB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	cfgPath := filepath.Join(t.TempDir(), "usbsync.json")
	now := func() time.Time { return time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC) }

	controllerA := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			return usb.DriveProbe{ExePath: exePath, IsRemovable: true, VolumeID: "VOL010"}, nil
		},
		now,
		func() (string, error) { return "PC-A", nil },
	)
	controllerA.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-a", Hostname: "PC-A"}, nil
	}

	controllerB := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			return usb.DriveProbe{ExePath: exePath, IsRemovable: true, VolumeID: "VOL010"}, nil
		},
		now,
		func() (string, error) { return "PC-B", nil },
	)
	controllerB.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-b", Hostname: "PC-B"}, nil
	}

	if err := os.MkdirAll(filepath.Join(workRootA, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	originalPath := filepath.Join(workRootA, "docs", "note.txt")
	if err := os.WriteFile(originalPath, []byte("from-a"), 0o644); err != nil {
		t.Fatalf("write original file: %v", err)
	}

	initA := controllerA.handleInitialize(ui.FormState{
		WorkRoot:    workRootA,
		DisplayName: "A",
		BackupDir:   backupA,
	}, nil)
	if len(initA.Results.Failures) != 0 {
		t.Fatalf("unexpected A initialize failures: %#v", initA.Results.Failures)
	}
	syncA := controllerA.handleSync(ui.FormState{
		WorkRoot:    workRootA,
		DisplayName: "A",
		BackupDir:   backupA,
	}, nil)
	if len(syncA.Results.Failures) != 0 {
		t.Fatalf("unexpected A sync failures: %#v", syncA.Results.Failures)
	}

	syncB1 := controllerB.handleSync(ui.FormState{
		WorkRoot:    workRootB,
		DisplayName: "B",
		BackupDir:   backupB,
	}, nil)
	if len(syncB1.Results.Failures) != 0 {
		t.Fatalf("unexpected first B sync failures: %#v", syncB1.Results.Failures)
	}

	bPath := filepath.Join(workRootB, "docs", "note.txt")
	if err := os.WriteFile(bPath, []byte("changed-on-b"), 0o644); err != nil {
		t.Fatalf("rewrite B file: %v", err)
	}
	syncB2 := controllerB.handleSync(ui.FormState{
		WorkRoot:    workRootB,
		DisplayName: "B",
		BackupDir:   backupB,
	}, nil)
	if len(syncB2.Results.Failures) != 0 {
		t.Fatalf("unexpected second B sync failures: %#v", syncB2.Results.Failures)
	}

	if err := os.Remove(fileindex.DefaultCachePath(workRootA)); err != nil {
		t.Fatalf("remove A cache: %v", err)
	}

	syncA2 := controllerA.handleSync(ui.FormState{
		WorkRoot:    workRootA,
		DisplayName: "A",
		BackupDir:   backupA,
	}, nil)
	if len(syncA2.Results.Failures) != 0 {
		t.Fatalf("unexpected second A sync failures: %#v", syncA2.Results.Failures)
	}
	if len(syncA2.Results.Conflicts) != 0 {
		t.Fatalf("unexpected A conflicts: %#v", syncA2.Results.Conflicts)
	}

	data, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("read A file: %v", err)
	}
	if string(data) != "changed-on-b" {
		t.Fatalf("unexpected A file content: %s", string(data))
	}
}

func TestSyncActionRejectsInvalidFolderPath(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "usbsync.json")
	controller := newRuntimeController(
		cfgPath,
		func() (usb.DriveProbe, error) {
			exePath := filepath.Join(t.TempDir(), "USBSync.exe")
			_ = os.WriteFile(exePath, []byte("exe"), 0o644)
			return usb.DriveProbe{
				ExePath:     exePath,
				IsRemovable: true,
				VolumeID:    "VOL003",
			}, nil
		},
		func() time.Time { return time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC) },
		func() (string, error) { return "Office", nil },
	)
	controller.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "hw-office", Hostname: "Office"}, nil
	}

	result := controller.handleSync(ui.FormState{
		WorkRoot:    `bad\path`,
		DisplayName: "Office",
		BackupDir:   `C:\backup`,
	}, nil)

	if len(result.Results.Failures) == 0 {
		t.Fatal("expected invalid path failure")
	}
}

func TestRuntimeControllerKeepsDifferentHardwareProfilesSeparate(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "usbsync.json")
	backupDirA := filepath.Join(t.TempDir(), "backup-a")
	backupDirB := filepath.Join(t.TempDir(), "backup-b")
	workRootA := filepath.Join(t.TempDir(), "work-a")
	workRootB := filepath.Join(t.TempDir(), "work-b")
	for _, dir := range []string{backupDirA, backupDirB, workRootA, workRootB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	controllerA := newRuntimeController(
		cfgPath,
		nil,
		func() time.Time { return time.Unix(0, 0) },
		func() (string, error) { return "MININT-123", nil },
	)
	controllerA.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "{hw-a}", Hostname: "MININT-123"}, nil
	}
	controllerA.handleDraftChanged(ui.FormState{
		WorkRoot:    workRootA,
		DisplayName: "Office",
		BackupDir:   backupDirA,
	})

	controllerB := newRuntimeController(
		cfgPath,
		nil,
		func() time.Time { return time.Unix(0, 0) },
		func() (string, error) { return "MININT-123", nil },
	)
	controllerB.profile = func() (machine.Profile, error) {
		return machine.Profile{HardwareID: "{hw-b}", Hostname: "MININT-123"}, nil
	}
	controllerB.handleDraftChanged(ui.FormState{
		WorkRoot:    workRootB,
		DisplayName: "Home",
		BackupDir:   backupDirB,
	})

	cfgA, err := config.LoadMachineConfigForMachine(cfgPath, "{hw-a}")
	if err != nil {
		t.Fatalf("load machine A config: %v", err)
	}
	cfgB, err := config.LoadMachineConfigForMachine(cfgPath, "{hw-b}")
	if err != nil {
		t.Fatalf("load machine B config: %v", err)
	}

	if cfgA.LastWorkRoot != workRootA || cfgA.DisplayName != "Office" {
		t.Fatalf("unexpected machine A config: %#v", cfgA)
	}
	if cfgB.LastWorkRoot != workRootB || cfgB.DisplayName != "Home" {
		t.Fatalf("unexpected machine B config: %#v", cfgB)
	}
}

func findMergePreview(preview []syncpreview.MergeResolution, pathKey string) *syncpreview.MergeResolution {
	for _, item := range preview {
		if item.PathKey == pathKey {
			copy := item
			return &copy
		}
	}
	return nil
}

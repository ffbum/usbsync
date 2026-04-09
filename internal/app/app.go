package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"usbsync/internal/config"
	"usbsync/internal/db"
	"usbsync/internal/fileindex"
	"usbsync/internal/fileutil"
	"usbsync/internal/machine"
	syncpreview "usbsync/internal/sync"
	"usbsync/internal/ui"
	"usbsync/internal/usb"
)

type BuildInfo struct {
	Version string
}

type StartupState struct {
	MachineConfig config.MachineConfig
	Drive         usb.DriveContext
}

type InitializeCurrentUSBRequest struct {
	MachineConfigPath string
	MachineProfile    string
	Drive             usb.DriveContext
	MachineID         string
	DisplayName       string
	WorkRoot          string
	BackupDir         string
	DeviceID          string
	SchemaVersion     int64
	SeenAt            string
	Progress          func(syncpreview.Event)
}

type InitializeCurrentUSBResult struct {
	DatabasePath string
	Progress     []syncpreview.Event
}

type PreviewSyncRequest struct {
	WorkRoot         string
	DatabasePath     string
	LastSeenRevision int64
}

type PreviewSyncResult struct {
	LocalChanges   []syncpreview.LocalChange
	MergePreview   []syncpreview.MergeResolution
	LatestRevision int64
}

type SyncCurrentUSBRequest struct {
	WorkRoot         string
	DatabasePath     string
	MachineID        string
	LastSeenRevision int64
	SeenAt           string
	Progress         func(syncpreview.Event)
}

type SyncCurrentUSBResult struct {
	LocalChanges           []syncpreview.LocalChange
	LocalBreakdown         syncpreview.ChangeBreakdown
	RemoteAppliedBreakdown syncpreview.ChangeBreakdown
	ConflictCount          int
	MergePreview           []syncpreview.MergeResolution
	CommittedCount         int
	LatestRevision         int64
	Progress               []syncpreview.Event
}

func DefaultBuildInfo() BuildInfo {
	return BuildInfo{
		Version: "dev",
	}
}

func Run() error {
	_ = DefaultBuildInfo()

	cfgPath, cfgErr := defaultMachineConfigPath(os.Executable, os.Hostname)
	state, stateErr := loadStartupStateForPath(cfgPath)
	if stateErr == nil && cfgErr != nil {
		stateErr = cfgErr
	}
	controller := newRuntimeController(cfgPath, usb.ProbeCurrentDrive, time.Now, os.Hostname)
	controller.ensureDraftFile(state.MachineConfig)

	window, err := ui.NewMainWindow(buildRuntimeViewModel(state, stateErr, controller))
	if err != nil {
		return err
	}

	_, err = window.Run()
	return err
}

func LoadStartupState(machineConfigPath string, probe usb.DriveProbe) (StartupState, error) {
	profile, _ := machine.CurrentProfile()
	cfg, err := config.LoadMachineConfigOrDefaultForKeys(machineConfigPath, profile.ConfigKey(), profile.LegacyConfigKeys()...)
	if err != nil {
		return StartupState{}, err
	}

	drive, err := usb.BuildDriveContext(probe)
	if err != nil {
		return StartupState{
			MachineConfig: cfg,
		}, err
	}

	return StartupState{
		MachineConfig: cfg,
		Drive:         drive,
	}, nil
}

func InitializeCurrentUSB(request InitializeCurrentUSBRequest) (InitializeCurrentUSBResult, error) {
	if request.MachineConfigPath == "" {
		return InitializeCurrentUSBResult{}, fmt.Errorf("missing machine config path")
	}
	if request.MachineID == "" {
		return InitializeCurrentUSBResult{}, fmt.Errorf("missing machine id")
	}
	if request.DisplayName == "" {
		return InitializeCurrentUSBResult{}, fmt.Errorf("missing display name")
	}
	if request.WorkRoot == "" {
		return InitializeCurrentUSBResult{}, fmt.Errorf("missing work root")
	}
	if request.BackupDir == "" {
		return InitializeCurrentUSBResult{}, fmt.Errorf("missing backup dir")
	}
	if request.DeviceID == "" {
		request.DeviceID = "device-1"
	}
	if request.SchemaVersion == 0 {
		request.SchemaVersion = 1
	}
	if request.SeenAt == "" {
		request.SeenAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	store, err := db.OpenStore(request.Drive.DBPath)
	if err != nil {
		return InitializeCurrentUSBResult{}, err
	}
	defer store.Close()

	if err := store.InitializeDeviceMeta(request.DeviceID, request.SchemaVersion, request.SeenAt, 2); err != nil {
		return InitializeCurrentUSBResult{}, err
	}
	event := syncpreview.Event{
		Phase:      "initialize",
		Item:       filepath.Base(request.Drive.DBPath),
		Done:       1,
		Total:      3,
		TotalKnown: true,
		Status:     "完成",
		Detail:     "已创建数据库文件",
	}
	emitAppProgress(request.Progress, event)
	if err := store.UpsertMachine(request.MachineID, request.DisplayName, request.WorkRoot, request.SeenAt); err != nil {
		return InitializeCurrentUSBResult{}, err
	}
	event = syncpreview.Event{
		Phase:      "initialize",
		Item:       request.DisplayName,
		Done:       2,
		Total:      3,
		TotalKnown: true,
		Status:     "完成",
		Detail:     "已登记当前电脑",
	}
	emitAppProgress(request.Progress, event)
	if err := store.UpdateMachineState(request.MachineID, 0, request.SeenAt, "", 1); err != nil {
		return InitializeCurrentUSBResult{}, err
	}

	cfg := config.DefaultMachineConfig()
	cfg.MachineID = request.MachineID
	cfg.DisplayName = request.DisplayName
	cfg.LastWorkRoot = request.WorkRoot
	cfg.BackupDir = request.BackupDir
	cfg.BoundDeviceID = request.DeviceID
	cfg.BoundVolumeID = request.Drive.VolumeID
	cfg.MachineConfigPath = request.MachineConfigPath
	cfg.StateDir = filepathDir(request.MachineConfigPath)

	machineName := strings.TrimSpace(request.MachineProfile)
	if machineName == "" {
		profile, _ := machine.CurrentProfile()
		machineName = profile.ConfigKey()
	}
	if err := config.SaveMachineConfigForMachine(request.MachineConfigPath, machineName, cfg); err != nil {
		return InitializeCurrentUSBResult{}, err
	}
	event = syncpreview.Event{
		Phase:      "initialize",
		Item:       request.MachineConfigPath,
		Done:       3,
		Total:      3,
		TotalKnown: true,
		Status:     "完成",
		Detail:     "已保存本机设置",
	}
	emitAppProgress(request.Progress, event)

	return InitializeCurrentUSBResult{
		DatabasePath: request.Drive.DBPath,
		Progress: []syncpreview.Event{{
			Phase:      "initialize",
			Item:       filepath.Base(request.Drive.DBPath),
			Done:       1,
			Total:      3,
			TotalKnown: true,
			Status:     "完成",
			Detail:     "已创建数据库文件",
		}, {
			Phase:      "initialize",
			Item:       request.DisplayName,
			Done:       2,
			Total:      3,
			TotalKnown: true,
			Status:     "完成",
			Detail:     "已登记当前电脑",
		}, {
			Phase:      "initialize",
			Item:       request.MachineConfigPath,
			Done:       3,
			Total:      3,
			TotalKnown: true,
			Status:     "完成",
			Detail:     "已保存本机设置",
		}},
	}, nil
}

func PreviewSync(request PreviewSyncRequest) (PreviewSyncResult, error) {
	if request.WorkRoot == "" {
		return PreviewSyncResult{}, fmt.Errorf("missing work root")
	}
	if request.DatabasePath == "" {
		return PreviewSyncResult{}, fmt.Errorf("missing database path")
	}

	store, err := db.OpenStore(request.DatabasePath)
	if err != nil {
		return PreviewSyncResult{}, err
	}
	defer store.Close()

	entryRecords, err := store.ListEntries()
	if err != nil {
		return PreviewSyncResult{}, err
	}
	baselineRecords := entryRecords
	if request.LastSeenRevision > 0 {
		baselineRecords, err = store.ListEntriesAtRevision(request.LastSeenRevision)
		if err != nil {
			return PreviewSyncResult{}, err
		}
	}
	changeRecords, err := store.ListChangesAfter(request.LastSeenRevision)
	if err != nil {
		return PreviewSyncResult{}, err
	}
	latestRevision, err := store.GetLatestRevision()
	if err != nil {
		return PreviewSyncResult{}, err
	}

	result, err := syncpreview.NewEngine().Run(syncpreview.Options{
		WorkRoot:      request.WorkRoot,
		Mode:          syncpreview.ModePreview,
		KnownEntries:  mapKnownEntries(baselineRecords),
		RemoteChanges: mapRemoteChanges(changeRecords),
	})
	if err != nil {
		return PreviewSyncResult{}, err
	}

	return PreviewSyncResult{
		LocalChanges:   result.LocalChanges,
		MergePreview:   result.MergePreview,
		LatestRevision: latestRevision,
	}, nil
}

func SyncCurrentUSB(request SyncCurrentUSBRequest) (SyncCurrentUSBResult, error) {
	if request.WorkRoot == "" {
		return SyncCurrentUSBResult{}, fmt.Errorf("missing work root")
	}
	if request.DatabasePath == "" {
		return SyncCurrentUSBResult{}, fmt.Errorf("missing database path")
	}
	if request.MachineID == "" {
		return SyncCurrentUSBResult{}, fmt.Errorf("missing machine id")
	}
	if request.SeenAt == "" {
		request.SeenAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	store, err := db.OpenStore(request.DatabasePath)
	if err != nil {
		return SyncCurrentUSBResult{}, err
	}
	defer store.Close()

	entryRecords, err := store.ListEntries()
	if err != nil {
		return SyncCurrentUSBResult{}, err
	}
	baselineRecords := entryRecords
	if request.LastSeenRevision > 0 {
		baselineRecords, err = store.ListEntriesAtRevision(request.LastSeenRevision)
		if err != nil {
			return SyncCurrentUSBResult{}, err
		}
	}
	changeRecords, err := store.ListChangesAfter(request.LastSeenRevision)
	if err != nil {
		return SyncCurrentUSBResult{}, err
	}

	preview, err := syncpreview.NewEngine().Run(syncpreview.Options{
		WorkRoot:      request.WorkRoot,
		Mode:          syncpreview.ModePreview,
		KnownEntries:  mapKnownEntries(baselineRecords),
		RemoteChanges: mapRemoteChanges(changeRecords),
		Progress:      request.Progress,
	})
	if err != nil {
		return SyncCurrentUSBResult{}, err
	}

	progress := append([]syncpreview.Event(nil), preview.Progress...)
	remoteByPath := latestChangeRecordByPath(changeRecords)
	commitPaths := make(map[string]struct{})
	localBreakdown := syncpreview.CountLocalChangeBreakdown(preview.LocalChanges)
	remoteAppliedBreakdown := syncpreview.ChangeBreakdown{}
	conflictCount := 0
	latestRemoteRevision := request.LastSeenRevision
	remoteTotal := 0
	for _, resolution := range preview.MergePreview {
		record, hasRemote := remoteByPath[resolution.PathKey]
		if !hasRemote {
			continue
		}

		if record.Revision > latestRemoteRevision {
			latestRemoteRevision = record.Revision
		}

		switch resolution.Decision {
		case syncpreview.DecisionApplyRemote, syncpreview.DecisionKeepRemoteWithWarning, syncpreview.DecisionConflict:
			remoteTotal++
		}
	}

	remoteDone := 0
	for _, resolution := range preview.MergePreview {
		record, hasRemote := remoteByPath[resolution.PathKey]
		switch resolution.Decision {
		case syncpreview.DecisionApplyRemote, syncpreview.DecisionKeepRemoteWithWarning:
			if hasRemote {
				if err := applyRemoteChange(store, request.WorkRoot, record, record.DisplayPath); err != nil {
					return SyncCurrentUSBResult{}, err
				}
				remoteDone++
				progress = append(progress, syncpreview.Event{
					Phase:      "apply",
					Item:       record.DisplayPath,
					Done:       remoteDone,
					Total:      remoteTotal,
					TotalKnown: true,
					Status:     syncpreview.OperationLabel(record.Op),
					Detail:     remoteApplyDetail(record.Op),
				})
				remoteAppliedBreakdown.AddOp(record.Op)
				emitAppProgress(request.Progress, progress[len(progress)-1])
			}
		case syncpreview.DecisionConflict:
			if hasRemote {
				if err := applyRemoteChange(store, request.WorkRoot, record, resolution.ConflictDisplayPath); err != nil {
					return SyncCurrentUSBResult{}, err
				}
				remoteDone++
				progress = append(progress, syncpreview.Event{
					Phase:      "conflict",
					Item:       resolution.ConflictDisplayPath,
					Done:       remoteDone,
					Total:      remoteTotal,
					TotalKnown: true,
					Status:     "冲突",
					Detail:     "发现双方修改，已写入冲突副本",
				})
				conflictCount++
				emitAppProgress(request.Progress, progress[len(progress)-1])
			}
			commitPaths[resolution.PathKey] = struct{}{}
		case syncpreview.DecisionCommitLocal, syncpreview.DecisionKeepLocalWithWarning:
			commitPaths[resolution.PathKey] = struct{}{}
		}
	}

	committer := dbCommitter{store: store}
	committedCount := 0
	commitTotal := len(commitPaths)
	for _, change := range preview.LocalChanges {
		if _, shouldCommit := commitPaths[change.PathKey]; !shouldCommit {
			continue
		}

		blob := syncpreview.BlobWrite{}
		if change.Kind == "file" && change.Op != "delete" {
			blobData, err := fileutil.ReadFileBlob(filepath.Join(request.WorkRoot, filepath.FromSlash(change.DisplayPath)))
			if err != nil {
				return SyncCurrentUSBResult{}, err
			}
			blob = syncpreview.BlobWrite{
				BlobID: blobData.BlobID,
				Chunks: blobData.Chunks,
			}
		}

		if _, err := committer.CommitLocalChange(request.MachineID, change, blob, request.SeenAt); err != nil {
			return SyncCurrentUSBResult{}, err
		}
		committedCount++
		progress = append(progress, syncpreview.Event{
			Phase:      "commit",
			Item:       change.DisplayPath,
			Done:       committedCount,
			Total:      commitTotal,
			TotalKnown: true,
			Status:     syncpreview.OperationLabel(change.Op),
			Detail:     commitDetail(change.Op),
		})
		emitAppProgress(request.Progress, progress[len(progress)-1])
	}

	if committedCount == 0 && latestRemoteRevision > request.LastSeenRevision {
		state, err := store.GetMachineState(request.MachineID)
		if err != nil {
			return SyncCurrentUSBResult{}, err
		}
		workspaceGeneration, err := store.GetWorkspaceGeneration()
		if err != nil {
			return SyncCurrentUSBResult{}, err
		}
		if err := store.UpdateMachineState(request.MachineID, latestRemoteRevision, request.SeenAt, state.LastBackupAt, workspaceGeneration); err != nil {
			return SyncCurrentUSBResult{}, err
		}
		progress = append(progress, syncpreview.Event{
			Phase:      "state",
			Item:       request.MachineID,
			Done:       1,
			Total:      1,
			TotalKnown: true,
			Status:     "完成",
			Detail:     "已更新这台电脑的同步版本",
		})
		emitAppProgress(request.Progress, progress[len(progress)-1])
	} else if commitTotal == 0 {
		progress = append(progress, syncpreview.Event{
			Phase:      "commit",
			Item:       request.MachineID,
			Done:       0,
			Total:      0,
			TotalKnown: true,
			Status:     "完成",
			Detail:     "没有需要写入数据库的本地变化",
		})
		emitAppProgress(request.Progress, progress[len(progress)-1])
	}

	latestRevision, err := store.GetLatestRevision()
	if err != nil {
		return SyncCurrentUSBResult{}, err
	}
	if err := refreshLocalScanCache(request.WorkRoot, store); err != nil {
		return SyncCurrentUSBResult{}, err
	}

	return SyncCurrentUSBResult{
		LocalChanges:           preview.LocalChanges,
		LocalBreakdown:         localBreakdown,
		RemoteAppliedBreakdown: remoteAppliedBreakdown,
		ConflictCount:          conflictCount,
		MergePreview:           preview.MergePreview,
		CommittedCount:         committedCount,
		LatestRevision:         latestRevision,
		Progress:               progress,
	}, nil
}

func loadDefaultStartupState() (StartupState, error) {
	cfgPath, err := defaultMachineConfigPath(os.Executable, os.Hostname)
	if err != nil {
		return StartupState{}, err
	}
	return loadStartupStateForPath(cfgPath)
}

func loadStartupStateForPath(cfgPath string) (StartupState, error) {
	profile, _ := machine.CurrentProfile()
	cfg, err := config.LoadMachineConfigOrDefaultForKeys(cfgPath, profile.ConfigKey(), profile.LegacyConfigKeys()...)
	if err != nil {
		return StartupState{}, err
	}

	probe, err := usb.ProbeCurrentDrive()
	if err != nil {
		return StartupState{MachineConfig: cfg}, err
	}

	drive, err := usb.BuildDriveContext(probe)
	if err != nil {
		return StartupState{MachineConfig: cfg}, err
	}

	return StartupState{
		MachineConfig: cfg,
		Drive:         drive,
	}, nil
}

func defaultMachineConfigPath(
	executable func() (string, error),
	hostname func() (string, error),
) (string, error) {
	if executable == nil {
		executable = os.Executable
	}
	if hostname == nil {
		hostname = os.Hostname
	}

	exePath, err := executable()
	if err != nil {
		return config.DefaultMachineConfig().MachineConfigPath, err
	}

	_, hostErr := hostname()
	path := config.PortableMachineConfigPath(exePath)
	if strings.TrimSpace(path) == "" {
		return config.DefaultMachineConfig().MachineConfigPath, hostErr
	}
	return path, hostErr
}

func driveStatusText(state StartupState, err error) string {
	if err == nil {
		dbPath := strings.TrimSpace(state.Drive.DBPath)
		if dbPath != "" {
			return dbPath
		}
		exeDir := strings.TrimSpace(state.Drive.ExeDir)
		if exeDir != "" {
			return filepath.Join(exeDir, usb.DatabaseFileName)
		}
		if strings.TrimSpace(state.Drive.RootPath) != "" {
			return filepath.Join(state.Drive.RootPath, usb.DatabaseFileName)
		}
		return usb.DatabaseFileName
	}

	return err.Error()
}

func buildMainViewModel(state StartupState, stateErr error) ui.MainViewModel {
	driveStatus := driveStatusText(state, stateErr)
	initializeEnabled := !state.Drive.DatabaseExists && stateErr == nil
	syncEnabled := stateErr == nil && state.Drive.DatabaseExists
	results := ui.ResultSummary{
		Status: "尚未开始同步",
	}

	if stateErr != nil {
		initializeEnabled = false
		syncEnabled = false
	}
	if stateErr == nil {
		if required, err := requiresReinitialize(state.MachineConfig.MachineID, state.MachineConfig.LastWorkRoot, state.Drive.DBPath); err == nil && required {
			initializeEnabled = true
			syncEnabled = false
			results = ui.ResultSummary{
				Status: "工作文件夹已更换，请重新初始化数据库",
			}
		}
	}

	return ui.MainViewModel{
		WorkRoot:          state.MachineConfig.LastWorkRoot,
		DisplayName:       state.MachineConfig.DisplayName,
		BackupDir:         state.MachineConfig.BackupDir,
		DatabasePath:      state.Drive.DBPath,
		DriveStatus:       driveStatus,
		InitializeEnabled: initializeEnabled,
		SyncEnabled:       syncEnabled,
		Results:           results,
	}
}

func requiresReinitialize(machineID, workRoot, databasePath string) (bool, error) {
	if strings.TrimSpace(machineID) == "" || strings.TrimSpace(workRoot) == "" || strings.TrimSpace(databasePath) == "" {
		return false, nil
	}
	if _, err := os.Stat(databasePath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	store, err := db.OpenStore(databasePath)
	if err != nil {
		return false, err
	}
	defer store.Close()

	records, err := store.ListMachines()
	if err != nil {
		return false, err
	}

	currentWorkRoot := fileutil.NormalizeFolderPath(workRoot)
	for _, record := range records {
		if record.MachineID != machineID {
			continue
		}
		return fileutil.NormalizeFolderPath(record.LastWorkRoot) != currentWorkRoot, nil
	}

	return false, nil
}

func emitAppProgress(callback func(syncpreview.Event), event syncpreview.Event) {
	if callback != nil {
		callback(event)
	}
}

func filepathDir(path string) string {
	if path == "" {
		return ""
	}

	lastSlash := len(path) - 1
	for lastSlash >= 0 && path[lastSlash] != '\\' && path[lastSlash] != '/' {
		lastSlash--
	}
	if lastSlash <= 0 {
		return path
	}

	return path[:lastSlash]
}

func mapKnownEntries(records []db.EntryRecord) []syncpreview.KnownEntry {
	entries := make([]syncpreview.KnownEntry, 0, len(records))
	for _, record := range records {
		if record.PathKey == db.WorkspaceResetPathKey {
			continue
		}
		entries = append(entries, syncpreview.KnownEntry{
			PathKey:      record.PathKey,
			DisplayPath:  record.DisplayPath,
			Kind:         record.Kind,
			Size:         record.Size,
			CtimeNS:      record.CtimeNS,
			MtimeNS:      record.MtimeNS,
			MD5:          record.ContentMD5,
			Deleted:      record.Deleted,
			LastRevision: record.LastRevision,
		})
	}
	return entries
}

func mapRemoteChanges(records []db.ChangeRecord) []syncpreview.RemoteChange {
	changes := make([]syncpreview.RemoteChange, 0, len(records))
	for _, record := range records {
		if record.Op == "workspace_reset" {
			continue
		}
		changes = append(changes, syncpreview.RemoteChange{
			Revision:     record.Revision,
			Op:           record.Op,
			PathKey:      record.PathKey,
			DisplayPath:  record.DisplayPath,
			Kind:         record.Kind,
			BaseRevision: record.BaseRevision,
			CtimeNS:      record.CtimeNS,
			MachineName:  record.MachineName,
		})
	}
	return changes
}

func latestChangeRecordByPath(records []db.ChangeRecord) map[string]db.ChangeRecord {
	latest := make(map[string]db.ChangeRecord, len(records))
	for _, record := range records {
		latest[record.PathKey] = record
	}
	return latest
}

func applyRemoteChange(store *db.Store, workRoot string, change db.ChangeRecord, targetDisplayPath string) error {
	targetPath := filepath.Join(workRoot, filepath.FromSlash(targetDisplayPath))

	switch change.Op {
	case "delete":
		return removePath(targetPath)
	case "mkdir":
		return ensureDirectory(targetPath)
	default:
		if change.Kind == "dir" {
			return ensureDirectory(targetPath)
		}
		data, err := store.ReadBlob(change.BlobID)
		if err != nil {
			return err
		}
		if err := fileutil.WriteFileAtomically(targetPath, data); err != nil {
			return err
		}
		return applyRecordedTimes(targetPath, change.CtimeNS, change.MtimeNS)
	}
}

type dbCommitter struct {
	store *db.Store
}

func (c dbCommitter) CommitLocalChange(machineID string, change syncpreview.LocalChange, blob syncpreview.BlobWrite, seenAt string) (int64, error) {
	return c.store.CommitLocalChange(machineID, change, db.BlobWrite{
		BlobID: blob.BlobID,
		Chunks: blob.Chunks,
	}, seenAt)
}

func ensureDirectory(path string) error {
	return os.MkdirAll(path, 0o755)
}

func removePath(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	return nil
}

func applyRecordedTimes(path string, ctimeNS, mtimeNS int64) error {
	if mtimeNS > 0 {
		timestamp := time.Unix(0, mtimeNS)
		if err := os.Chtimes(path, timestamp, timestamp); err != nil {
			return err
		}
	}

	return setCreationTime(path, ctimeNS)
}

func refreshLocalScanCache(workRoot string, store *db.Store) error {
	entries, err := fileutil.ScanWorktree(workRoot)
	if err != nil {
		return err
	}
	records, err := store.ListEntries()
	if err != nil {
		return err
	}

	recordByPath := make(map[string]db.EntryRecord, len(records))
	for _, record := range records {
		if record.Deleted {
			continue
		}
		recordByPath[record.PathKey] = record
	}

	for i := range entries {
		record, ok := recordByPath[entries[i].PathKey]
		if !ok || record.Kind != entries[i].Kind {
			continue
		}
		entries[i].LastRevision = record.LastRevision
		entries[i].MD5 = record.ContentMD5
	}

	return fileindex.Save(fileindex.DefaultCachePath(workRoot), entries)
}

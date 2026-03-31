package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"usbsync/internal/config"
	"usbsync/internal/db"
	"usbsync/internal/fileutil"
	"usbsync/internal/machine"
	syncpreview "usbsync/internal/sync"
	"usbsync/internal/ui"
	"usbsync/internal/usb"
)

const (
	latestBackupFileName = "USBSync-latest.db"
	prevBackupFileName   = "USBSync-prev.db"
)

type runtimeController struct {
	machineConfigPath string
	probe             func() (usb.DriveProbe, error)
	now               func() time.Time
	hostname          func() (string, error)
	profile           func() (machine.Profile, error)
}

func newRuntimeController(
	machineConfigPath string,
	probe func() (usb.DriveProbe, error),
	now func() time.Time,
	hostname func() (string, error),
) runtimeController {
	if machineConfigPath == "" {
		machineConfigPath = config.DefaultMachineConfig().MachineConfigPath
	}
	if probe == nil {
		probe = usb.ProbeCurrentDrive
	}
	if now == nil {
		now = time.Now
	}
	if hostname == nil {
		hostname = os.Hostname
	}
	profile := func() (machine.Profile, error) {
		return machine.CurrentProfileWithHostname(hostname)
	}

	return runtimeController{
		machineConfigPath: machineConfigPath,
		probe:             probe,
		now:               now,
		hostname:          hostname,
		profile:           profile,
	}
}

func buildRuntimeViewModel(state StartupState, stateErr error, controller runtimeController) ui.MainViewModel {
	vm := buildMainViewModel(state, stateErr)
	vm.OnInitialize = controller.handleInitialize
	vm.OnSync = controller.handleSync
	vm.OnDraftChanged = controller.handleDraftChanged
	vm.OnDraftCommit = controller.handleDraftChanged
	return vm
}

func (c runtimeController) ensureDraftFile(cfg config.MachineConfig) {
	profile := c.currentMachineProfile()
	_ = saveMachineConfig(c.machineConfigPath, profile.ConfigKey(), cfg, cfg)
}

func (c runtimeController) handleDraftChanged(form ui.FormState) {
	if err := validateDraftPathsForSave(form); err != nil {
		return
	}

	profile := c.currentMachineProfile()
	cfg, err := c.loadMachineConfig(profile)
	if err != nil {
		return
	}

	normalized := c.normalizeFormState(form, cfg)
	_ = saveMachineConfig(c.machineConfigPath, profile.ConfigKey(), cfg, config.MachineConfig{
		MachineID:         cfg.MachineID,
		DisplayName:       normalized.DisplayName,
		LastWorkRoot:      normalized.WorkRoot,
		BackupDir:         normalized.BackupDir,
		BoundDeviceID:     cfg.BoundDeviceID,
		BoundVolumeID:     cfg.BoundVolumeID,
		StateDir:          filepath.Dir(c.machineConfigPath),
		MachineConfigPath: c.machineConfigPath,
	})
}

func (c runtimeController) handleInitialize(form ui.FormState, report ui.ProgressHandler) ui.ActionResult {
	cfg, state, driveErr, result := c.prepareActionResult(form)
	if driveErr != nil {
		result.Results = failureSummary("当前 U 盘不可用", driveErr)
		return result
	}
	if err := validateActionPaths(result.WorkRoot, result.BackupDir); err != nil {
		result.Results = failureSummary("初始化未开始", err)
		return result
	}
	if err := os.MkdirAll(result.WorkRoot, 0o755); err != nil {
		result.Results = failureSummary("初始化未开始", fmt.Errorf("无法准备工作文件夹: %w", err))
		return result
	}

	machineID, err := ensureMachineID(cfg)
	if err != nil {
		result.Results = failureSummary("初始化未开始", err)
		return result
	}

	deviceID := strings.TrimSpace(cfg.BoundDeviceID)
	if deviceID == "" {
		deviceID = defaultDeviceID(state.Drive)
	}
	seenAt := c.now().UTC().Format(time.RFC3339Nano)

	rebuildRequired, err := requiresReinitialize(cfg.MachineID, result.WorkRoot, state.Drive.DBPath)
	if err != nil {
		result.Results = failureSummary("初始化失败", err)
		return result
	}
	if rebuildRequired && state.Drive.DatabaseExists {
		backupErr := refreshLocalBackup(state.Drive.DBPath, result.BackupDir)
		if backupErr != nil {
			result.Results = failureSummary("初始化失败", fmt.Errorf("旧同步库备份失败：%w", backupErr))
			return result
		}
		if err := removeDatabaseFiles(state.Drive.DBPath); err != nil {
			result.Results = failureSummary("初始化失败", fmt.Errorf("旧同步库清理失败：%w", err))
			return result
		}
		state.Drive.DatabaseExists = false
	}

	initResult, err := InitializeCurrentUSB(InitializeCurrentUSBRequest{
		MachineConfigPath: c.machineConfigPath,
		MachineProfile:    c.currentMachineProfile().ConfigKey(),
		Drive:             state.Drive,
		MachineID:         machineID,
		DisplayName:       result.DisplayName,
		WorkRoot:          result.WorkRoot,
		BackupDir:         result.BackupDir,
		DeviceID:          deviceID,
		SchemaVersion:     1,
		SeenAt:            seenAt,
		Progress:          report,
	})
	if err != nil {
		result.Results = failureSummary("初始化失败", err)
		return result
	}

	backupErr := refreshLocalBackup(initResult.DatabasePath, result.BackupDir)
	savedCfg, err := c.loadMachineConfig(c.currentMachineProfile())
	if err != nil {
		result.Results = failureSummary("初始化失败", err)
		return result
	}

	updatedState := StartupState{
		MachineConfig: savedCfg,
		Drive: usb.DriveContext{
			ExePath:        state.Drive.ExePath,
			ExeDir:         state.Drive.ExeDir,
			RootPath:       state.Drive.RootPath,
			DBPath:         state.Drive.DBPath,
			DatabaseExists: true,
			IsRemovable:    state.Drive.IsRemovable,
			VolumeID:       state.Drive.VolumeID,
		},
	}
	result = actionResultFromState(updatedState, nil)
	result.ProgressRows = initResult.Progress
	result.Results = ui.ResultSummary{
		Status: "当前 U 盘已初始化，可以开始同步",
	}
	if backupErr != nil {
		result.Results.AddFailure("初始化已完成，但本机备份没有刷新：" + backupErr.Error())
	}
	return result
}

func (c runtimeController) handleSync(form ui.FormState, report ui.ProgressHandler) ui.ActionResult {
	cfg, state, driveErr, result := c.prepareActionResult(form)
	if driveErr != nil {
		result.Results = failureSummary("同步未开始", driveErr)
		return result
	}
	if !state.Drive.DatabaseExists {
		result.InitializeEnabled = true
		result.SyncEnabled = false
		result.Results = failureSummary("同步未开始", fmt.Errorf("当前 U 盘还没有 USBSync.db"))
		return result
	}
	if err := validateActionPaths(result.WorkRoot, result.BackupDir); err != nil {
		result.Results = failureSummary("同步未开始", err)
		return result
	}
	if err := os.MkdirAll(result.WorkRoot, 0o755); err != nil {
		result.Results = failureSummary("同步未开始", fmt.Errorf("无法准备工作文件夹: %w", err))
		return result
	}

	machineID, err := ensureMachineID(cfg)
	if err != nil {
		result.Results = failureSummary("同步未开始", err)
		return result
	}
	cfg.MachineID = machineID

	seenAt := c.now().UTC().Format(time.RFC3339Nano)
	rebuildRequired, err := requiresReinitialize(cfg.MachineID, result.WorkRoot, state.Drive.DBPath)
	if err != nil {
		result.Results = failureSummary("同步未开始", err)
		return result
	}
	if rebuildRequired {
		result.InitializeEnabled = true
		result.SyncEnabled = false
		result.Results = ui.ResultSummary{
			Status: "工作文件夹已更换，请重新初始化当前 U 盘",
		}
		return result
	}

	lastSeenRevision, deviceID, err := c.loadSyncContext(cfg, state.Drive, result.DisplayName, result.WorkRoot, seenAt)
	if err != nil {
		result.Results = failureSummary("同步未开始", err)
		return result
	}

	syncResult, err := SyncCurrentUSB(SyncCurrentUSBRequest{
		WorkRoot:         result.WorkRoot,
		DatabasePath:     state.Drive.DBPath,
		MachineID:        machineID,
		LastSeenRevision: lastSeenRevision,
		SeenAt:           seenAt,
		Progress:         report,
	})
	if err != nil {
		result.Results = failureSummary("同步失败", err)
		return result
	}

	profile := c.currentMachineProfile()
	saveErr := saveMachineConfig(c.machineConfigPath, profile.ConfigKey(), cfg, config.MachineConfig{
		MachineID:         cfg.MachineID,
		DisplayName:       result.DisplayName,
		LastWorkRoot:      result.WorkRoot,
		BackupDir:         result.BackupDir,
		BoundDeviceID:     deviceID,
		BoundVolumeID:     state.Drive.VolumeID,
		StateDir:          filepath.Dir(c.machineConfigPath),
		MachineConfigPath: c.machineConfigPath,
	})
	if saveErr != nil {
		result.Results = failureSummary("同步失败", saveErr)
		return result
	}

	backupErr := refreshLocalBackup(state.Drive.DBPath, result.BackupDir)
	savedCfg, err := c.loadMachineConfig(profile)
	if err != nil {
		result.Results = failureSummary("同步失败", err)
		return result
	}

	result = actionResultFromState(StartupState{
		MachineConfig: savedCfg,
		Drive:         state.Drive,
	}, nil)
	result.ProgressRows = syncResult.Progress
	result.Results = buildSyncSummary(syncResult)
	if backupErr != nil {
		result.Results.AddFailure("同步已完成，但本机备份没有刷新：" + backupErr.Error())
	}
	return result
}

func (c runtimeController) prepareActionResult(form ui.FormState) (config.MachineConfig, StartupState, error, ui.ActionResult) {
	cfg, err := c.loadMachineConfig(c.currentMachineProfile())
	if err != nil {
		summary := failureSummary("读取本机设置失败", err)
		return config.MachineConfig{}, StartupState{}, err, ui.ActionResult{
			WorkRoot:          strings.TrimSpace(form.WorkRoot),
			DisplayName:       strings.TrimSpace(form.DisplayName),
			BackupDir:         strings.TrimSpace(form.BackupDir),
			DriveStatus:       err.Error(),
			Results:           summary,
			InitializeEnabled: false,
			SyncEnabled:       false,
		}
	}

	drive, driveErr := c.currentDrive()
	state := StartupState{MachineConfig: cfg}
	if driveErr == nil {
		state.Drive = drive
	}

	normalizedForm := c.normalizeFormState(form, cfg)
	result := actionResultFromState(state, driveErr)
	result.WorkRoot = normalizedForm.WorkRoot
	result.DisplayName = normalizedForm.DisplayName
	result.BackupDir = normalizedForm.BackupDir
	return cfg, state, driveErr, result
}

func (c runtimeController) currentDrive() (usb.DriveContext, error) {
	probe, err := c.probe()
	if err != nil {
		return usb.DriveContext{}, err
	}
	return usb.BuildDriveContext(probe)
}

func (c runtimeController) currentMachineName() string {
	profile := c.currentMachineProfile()
	if strings.TrimSpace(profile.Hostname) != "" {
		return strings.TrimSpace(profile.Hostname)
	}
	host, err := c.hostname()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(host)
}

func (c runtimeController) currentMachineProfile() machine.Profile {
	if c.profile != nil {
		if profile, err := c.profile(); err == nil {
			return profile
		}
	}
	host, _ := c.hostname()
	host = strings.TrimSpace(host)
	return machine.Profile{
		HardwareID: host,
		Hostname:   host,
	}
}

func (c runtimeController) loadMachineConfig(profile machine.Profile) (config.MachineConfig, error) {
	return config.LoadMachineConfigOrDefaultForKeys(c.machineConfigPath, profile.ConfigKey(), profile.LegacyConfigKeys()...)
}

func (c runtimeController) normalizeFormState(form ui.FormState, cfg config.MachineConfig) ui.FormState {
	workRoot := strings.TrimSpace(form.WorkRoot)
	if workRoot == "" {
		workRoot = strings.TrimSpace(cfg.LastWorkRoot)
	}
	if workRoot != "" {
		workRoot = filepath.Clean(workRoot)
	}

	displayName := strings.TrimSpace(form.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(cfg.DisplayName)
	}
	if displayName == "" {
		if host, err := c.hostname(); err == nil && strings.TrimSpace(host) != "" {
			displayName = strings.TrimSpace(host)
		} else {
			displayName = "This PC"
		}
	}

	backupDir := strings.TrimSpace(form.BackupDir)
	if backupDir == "" {
		backupDir = strings.TrimSpace(cfg.BackupDir)
	}
	if backupDir == "" {
		backupDir = config.DefaultMachineConfig().BackupDir
	}
	if backupDir != "" {
		backupDir = filepath.Clean(backupDir)
	}

	return ui.FormState{
		WorkRoot:    workRoot,
		DisplayName: displayName,
		BackupDir:   backupDir,
	}
}

func (c runtimeController) loadSyncContext(cfg config.MachineConfig, drive usb.DriveContext, displayName, workRoot, seenAt string) (int64, string, error) {
	store, err := db.OpenStore(drive.DBPath)
	if err != nil {
		return 0, "", err
	}
	defer store.Close()

	meta, err := store.GetDeviceMeta()
	if err != nil {
		return 0, "", err
	}
	if strings.TrimSpace(cfg.BoundDeviceID) != "" && cfg.BoundDeviceID != meta.DeviceID {
		return 0, "", fmt.Errorf("当前 U 盘不是这台电脑原来绑定的那一只")
	}
	if strings.TrimSpace(cfg.BoundVolumeID) != "" && drive.VolumeID != "" && cfg.BoundVolumeID != drive.VolumeID {
		return 0, "", fmt.Errorf("当前 U 盘卷标识与上次记录不一致")
	}
	if err := store.UpsertMachine(cfg.MachineID, displayName, workRoot, seenAt); err != nil {
		return 0, "", err
	}

	state, err := store.GetMachineState(cfg.MachineID)
	if err != nil {
		return 0, "", fmt.Errorf("这台电脑还没有在当前 U 盘完成初始化")
	}

	return state.LastSeenRevision, meta.DeviceID, nil
}

func actionResultFromState(state StartupState, stateErr error) ui.ActionResult {
	vm := buildMainViewModel(state, stateErr)
	return ui.ActionResult{
		WorkRoot:          vm.WorkRoot,
		DisplayName:       vm.DisplayName,
		BackupDir:         vm.BackupDir,
		DriveStatus:       vm.DriveStatus,
		Results:           vm.Results,
		ProgressRows:      vm.ProgressRows,
		InitializeEnabled: vm.InitializeEnabled,
		SyncEnabled:       vm.SyncEnabled,
	}
}

func ensureMachineID(cfg config.MachineConfig) (string, error) {
	if strings.TrimSpace(cfg.MachineID) != "" {
		return cfg.MachineID, nil
	}

	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("无法生成本机标识: %w", err)
	}
	return "machine-" + hex.EncodeToString(buf), nil
}

func defaultDeviceID(drive usb.DriveContext) string {
	if strings.TrimSpace(drive.VolumeID) != "" {
		return "device-" + strings.ToLower(strings.TrimSpace(drive.VolumeID))
	}
	return "device-1"
}

func refreshLocalBackup(databasePath, backupDir string) error {
	if strings.TrimSpace(databasePath) == "" {
		return fmt.Errorf("missing database path")
	}
	if strings.TrimSpace(backupDir) == "" {
		return fmt.Errorf("missing backup dir")
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return err
	}

	latestPath := filepath.Join(backupDir, latestBackupFileName)
	prevPath := filepath.Join(backupDir, prevBackupFileName)
	if _, err := os.Stat(latestPath); err == nil {
		data, err := os.ReadFile(latestPath)
		if err != nil {
			return err
		}
		if err := fileutil.WriteFileAtomically(prevPath, data); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	data, err := os.ReadFile(databasePath)
	if err != nil {
		return err
	}
	return fileutil.WriteFileAtomically(latestPath, data)
}

func saveMachineConfig(path, machineName string, current, next config.MachineConfig) error {
	current.MachineID = next.MachineID
	current.DisplayName = next.DisplayName
	current.LastWorkRoot = next.LastWorkRoot
	current.BackupDir = next.BackupDir
	current.BoundDeviceID = next.BoundDeviceID
	current.BoundVolumeID = next.BoundVolumeID
	current.StateDir = next.StateDir
	current.MachineConfigPath = path
	return config.SaveMachineConfigForMachine(path, machineName, current)
}

func failureSummary(status string, err error) ui.ResultSummary {
	summary := ui.ResultSummary{Status: status}
	if err != nil {
		summary.AddFailure(err.Error())
	}
	return summary
}

func buildSyncSummary(result SyncCurrentUSBResult) ui.ResultSummary {
	summary := ui.ResultSummary{}
	localBreakdown := result.LocalBreakdown
	if localBreakdown.Total() == 0 && len(result.LocalChanges) > 0 {
		localBreakdown = syncpreview.CountLocalChangeBreakdown(result.LocalChanges)
	}
	remoteAppliedCount := result.RemoteAppliedBreakdown.Total()

	for _, item := range result.MergePreview {
		switch item.Decision {
		case syncpreview.DecisionApplyRemote, syncpreview.DecisionKeepRemoteWithWarning:
			if item.Decision == syncpreview.DecisionKeepRemoteWithWarning {
				summary.AddWarning("已采用 U 盘中的版本：" + item.PathKey)
			}
		case syncpreview.DecisionConflict:
			if item.ConflictDisplayPath != "" {
				summary.Conflicts = append(summary.Conflicts, item.PathKey+" 已另存为 "+item.ConflictDisplayPath)
			} else {
				summary.Conflicts = append(summary.Conflicts, item.PathKey)
			}
		case syncpreview.DecisionKeepLocalWithWarning:
			summary.AddWarning("已保留本地版本：" + item.PathKey)
		}
	}

	switch {
	case localBreakdown.Total() == 0 && remoteAppliedCount == 0 && result.ConflictCount == 0:
		summary.Status = "同步完成，没有发现新变化"
	default:
		status := fmt.Sprintf(
			"同步完成：本地%s，写入 U 盘 %d 项，写回本地%s",
			localBreakdown.SummaryText(),
			result.CommittedCount,
			result.RemoteAppliedBreakdown.SummaryText(),
		)
		if result.ConflictCount > 0 {
			status += fmt.Sprintf("，冲突副本 %d 项", result.ConflictCount)
		}
		summary.Status = status
	}

	return summary
}

func remoteApplyDetail(op string) string {
	switch op {
	case "delete":
		return "已从本地删除"
	case "mkdir", "add":
		return "已新增到本地目录"
	case "modify":
		return "已更新本地内容"
	default:
		return "已写回本地目录"
	}
}

func commitDetail(op string) string {
	switch op {
	case "delete":
		return "已将删除记录写入 U 盘数据库"
	case "mkdir", "add":
		return "已将新增内容写入 U 盘数据库"
	case "modify":
		return "已将修改内容写入 U 盘数据库"
	default:
		return "已写入 U 盘数据库"
	}
}

func validateActionPaths(workRoot, backupDir string) error {
	if err := fileutil.ValidateFolderPath(workRoot); err != nil {
		return fmt.Errorf("工作文件夹无效：%w", err)
	}
	if err := fileutil.ValidateCreatableFolderPath(backupDir); err != nil {
		return fmt.Errorf("备份目录无效：%w", err)
	}
	return nil
}

func validateDraftPathsForSave(form ui.FormState) error {
	if strings.TrimSpace(form.WorkRoot) != "" {
		if err := fileutil.ValidateFolderPath(form.WorkRoot); err != nil {
			return err
		}
	}
	if strings.TrimSpace(form.BackupDir) != "" {
		if err := fileutil.ValidateCreatableFolderPath(form.BackupDir); err != nil {
			return err
		}
	}
	return nil
}

func removeDatabaseFiles(databasePath string) error {
	paths := []string{
		databasePath,
		databasePath + "-journal",
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

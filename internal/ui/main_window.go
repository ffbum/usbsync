//go:build windows

package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lxn/walk"
	D "github.com/lxn/walk/declarative"
	"usbsync/internal/fileutil"
	progress "usbsync/internal/sync"
)

type MainViewModel struct {
	WorkRoot          string
	DisplayName       string
	BackupDir         string
	DatabasePath      string
	DriveStatus       string
	InitializeEnabled bool
	SyncEnabled       bool
	Results           ResultSummary
	ProgressRows      []progress.Event
	OnInitialize      ActionHandler
	OnSync            ActionHandler
	OnDraftChanged    DraftHandler
	OnDraftCommit     DraftHandler
	OnBeforeClose     BeforeCloseHandler
}

type MainWindow struct {
	window                      *walk.MainWindow
	workRootEdit                *walk.LineEdit
	displayNameEdit             *walk.LineEdit
	backupDirEdit               *walk.LineEdit
	statusLabel                 *walk.Label
	resultsBox                  *walk.TextEdit
	progressTable               *walk.TableView
	progressBar                 *walk.ProgressBar
	initButton                  *walk.PushButton
	syncButton                  *walk.PushButton
	openBackupBtn               *walk.PushButton
	workRootPickBtn             *walk.PushButton
	backupPickBtn               *walk.PushButton
	progressModel               *ProgressTableModel
	progressViewport            progressViewport
	viewModel                   MainViewModel
	runningAction               bool
	suppressNextValidationPopup bool
	lastCommittedForm           FormState
}

type progressViewport interface {
	SetCurrentIndex(index int) error
	EnsureItemVisible(index int)
}

func NewMainWindow(vm MainViewModel) (*MainWindow, error) {
	shell := &MainWindow{
		progressModel: NewProgressTableModel(),
		viewModel:     vm,
		lastCommittedForm: normalizeFormState(FormState{
			WorkRoot:    vm.WorkRoot,
			DisplayName: vm.DisplayName,
			BackupDir:   vm.BackupDir,
		}),
	}

	var mw *walk.MainWindow
	err := (D.MainWindow{
		AssignTo: &mw,
		Title:    "USBSync",
		MinSize:  D.Size{Width: 960, Height: 720},
		Layout:   D.VBox{},
		Children: []D.Widget{
			D.GroupBox{
				Title:  "当前信息",
				Layout: D.Grid{Columns: 3},
				Children: []D.Widget{
					D.Label{Text: "工作文件夹"},
					D.LineEdit{AssignTo: &shell.workRootEdit, Text: vm.WorkRoot, OnTextChanged: shell.handleDraftChanged, OnEditingFinished: shell.handleWorkRootEditingFinished},
					D.PushButton{AssignTo: &shell.workRootPickBtn, Text: "选择…", OnMouseDown: shell.preparePickFolder, OnClicked: shell.pickWorkRoot},
					D.Label{Text: "机器名称"},
					D.LineEdit{AssignTo: &shell.displayNameEdit, Text: vm.DisplayName, OnTextChanged: shell.handleDraftChanged, ColumnSpan: 2},
					D.Label{Text: "备份目录"},
					D.LineEdit{AssignTo: &shell.backupDirEdit, Text: vm.BackupDir, OnTextChanged: shell.handleDraftChanged, OnEditingFinished: shell.handleBackupDirEditingFinished},
					D.PushButton{AssignTo: &shell.backupPickBtn, Text: "选择…", OnMouseDown: shell.preparePickFolder, OnClicked: shell.pickBackupDir},
					D.Label{Text: "数据库"},
					D.Label{AssignTo: &shell.statusLabel, Text: vm.DriveStatus, ColumnSpan: 2},
				},
			},
			D.Composite{
				Layout: D.HBox{},
				Children: []D.Widget{
					D.PushButton{AssignTo: &shell.initButton, Text: "初始化数据库", Enabled: vm.InitializeEnabled, OnClicked: shell.handleInitialize},
					D.PushButton{AssignTo: &shell.syncButton, Text: "同步", Enabled: vm.SyncEnabled, OnClicked: shell.handleSync},
					D.PushButton{
						AssignTo:  &shell.openBackupBtn,
						Text:      "打开数据库目录",
						OnClicked: shell.openDatabaseDir,
					},
				},
			},
			D.GroupBox{
				Title:   "详细进度表",
				Layout:  D.VBox{},
				MinSize: D.Size{Height: progressGroupHeight()},
				MaxSize: D.Size{Height: progressGroupHeight()},
				Children: []D.Widget{
					D.ProgressBar{
						AssignTo: &shell.progressBar,
						MinValue: 0,
						MaxValue: 100,
						Value:    progressValue(vm.ProgressRows),
						MinSize:  D.Size{Height: 20},
						MaxSize:  D.Size{Height: 20},
					},
					D.TableView{
						AssignTo: &shell.progressTable,
						MinSize:  D.Size{Height: progressTableHeight()},
						MaxSize:  D.Size{Height: progressTableHeight()},
						Columns: []D.TableViewColumn{
							{Title: "阶段", Width: 120},
							{Title: "当前对象", Width: 240},
							{Title: "已完成", Width: 80},
							{Title: "总数", Width: 80},
							{Title: "状态", Width: 100},
							{Title: "说明", Width: 280},
						},
						Model: shell.progressModel,
					},
				},
			},
			D.GroupBox{
				Title:   "结果摘要",
				Layout:  D.VBox{},
				MinSize: D.Size{Height: resultSummaryGroupHeight()},
				MaxSize: D.Size{Height: resultSummaryGroupHeight()},
				Children: []D.Widget{
					D.TextEdit{
						AssignTo: &shell.resultsBox,
						Text:     vm.Results.SummaryText(),
						ReadOnly: true,
						VScroll:  true,
						MinSize:  D.Size{Height: resultSummaryBoxHeight()},
						MaxSize:  D.Size{Height: resultSummaryBoxHeight()},
					},
				},
			},
			D.VSpacer{},
		},
	}).Create()
	if err != nil {
		return nil, err
	}

	shell.window = mw
	shell.applyWindowIcon()
	shell.progressViewport = shell.progressTable
	if shell.window != nil {
		shell.window.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
			if shell.runningAction {
				*canceled = true
				walk.MsgBox(shell.window, "USBSync", "正在执行同步，写入数据库期间不能退出。", walk.MsgBoxOK|walk.MsgBoxIconWarning)
				return
			}
			shell.suppressNextValidationPopup = true
			if vm.OnDraftCommit != nil {
				shell.handleDraftCommit()
			}
			if err := shell.canClose(); err != nil {
				*canceled = true
				walk.MsgBox(shell.window, "USBSync", "退出前数据库检查失败："+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
			}
		})
	}
	shell.progressModel.Reset(vm.ProgressRows)
	shell.updateProgressBar(vm.ProgressRows)
	shell.scrollProgressToLatest()
	return shell, nil
}

func (w *MainWindow) Run() (int, error) {
	if w == nil || w.window == nil {
		return 0, fmt.Errorf("main window not initialized")
	}

	return w.window.Run(), nil
}

func (w *MainWindow) openDatabaseDir() {
	databasePath := strings.TrimSpace(w.viewModel.DatabasePath)
	if databasePath == "" {
		return
	}

	_ = exec.Command("explorer.exe", filepath.Dir(databasePath)).Start()
}

func (w *MainWindow) handleInitialize() {
	if w.viewModel.OnInitialize == nil {
		return
	}
	w.runAction("正在初始化数据库…", w.viewModel.OnInitialize)
}

func (w *MainWindow) handleSync() {
	if w.viewModel.OnSync == nil {
		return
	}
	w.runAction("正在同步，请稍候…", w.viewModel.OnSync)
}

func (w *MainWindow) handleDraftChanged() {
	if w.runningAction {
		return
	}

	form := normalizeFormState(w.currentFormState())
	if !draftChanged(w.lastCommittedForm, form) {
		w.restoreReadyState()
		return
	}
	if err := validateDraftForm(form); err != nil {
		w.showInlineValidation(err.Error())
		return
	}

	if w.viewModel.OnDraftChanged == nil {
		w.restoreReadyState()
		return
	}
	w.viewModel.OnDraftChanged(form)
	w.lastCommittedForm = form
	w.restoreReadyState()
}

func (w *MainWindow) handleDraftCommit() {
	if w.runningAction || w.viewModel.OnDraftCommit == nil {
		return
	}

	form := w.currentFormState()
	normalized := normalizeFormState(form)
	if !draftChanged(w.lastCommittedForm, normalized) {
		return
	}
	if err := validateDraftForm(form); err != nil {
		return
	}
	w.viewModel.OnDraftCommit(normalized)
	w.lastCommittedForm = normalized
}

func (w *MainWindow) handleWorkRootEditingFinished() {
	if w.consumeValidationPopupSuppression() {
		return
	}
	if sameNormalizedPath(textValue(w.workRootEdit), w.lastCommittedForm.WorkRoot) {
		return
	}
	w.showFolderValidationError("工作文件夹", w.workRootEdit, fileutil.ValidateFolderPath)
}

func (w *MainWindow) handleBackupDirEditingFinished() {
	if w.consumeValidationPopupSuppression() {
		return
	}
	if sameNormalizedPath(textValue(w.backupDirEdit), w.lastCommittedForm.BackupDir) {
		return
	}
	w.showFolderValidationError("备份目录", w.backupDirEdit, fileutil.ValidateCreatableFolderPath)
}

func (w *MainWindow) preparePickFolder(x, y int, button walk.MouseButton) {
	w.suppressNextValidationPopup = true
}

func (w *MainWindow) showFolderValidationError(fieldName string, edit *walk.LineEdit, validate func(string) error) {
	if w.runningAction || edit == nil {
		return
	}

	value := textValue(edit)
	if strings.TrimSpace(value) == "" {
		return
	}

	if validate == nil {
		validate = fileutil.ValidateFolderPath
	}
	if err := validate(value); err != nil {
		message := fieldName + "无效：" + err.Error()
		w.showValidationMessage(message)
		_ = edit.SetFocus()
		edit.SetTextSelection(0, -1)
	}
}

func (w *MainWindow) pickWorkRoot() {
	w.pickFolder("选择工作文件夹", w.workRootEdit)
}

func (w *MainWindow) pickBackupDir() {
	w.pickFolder("选择备份目录", w.backupDirEdit)
}

func (w *MainWindow) pickFolder(title string, edit *walk.LineEdit) {
	if w.runningAction || edit == nil {
		return
	}

	dialog := walk.FileDialog{
		Title:          title,
		InitialDirPath: browseFolderRoot(textValue(edit)),
	}

	accepted, err := dialog.ShowBrowseFolder(w.window)
	if err != nil {
		walk.MsgBox(w.window, "USBSync", err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if !accepted {
		return
	}

	selected := fileutil.NormalizeFolderPath(dialog.FilePath)
	if err := fileutil.ValidateFolderPath(selected); err != nil {
		walk.MsgBox(w.window, "USBSync", err.Error(), walk.MsgBoxOK|walk.MsgBoxIconWarning)
		return
	}

	edit.SetText(selected)
	w.handleDraftChanged()
}

func validateDraftForm(form FormState) error {
	if form.WorkRoot != "" {
		if err := fileutil.ValidateFolderPath(form.WorkRoot); err != nil {
			return fmt.Errorf("工作文件夹无效：%w", err)
		}
	}
	if form.BackupDir != "" {
		if err := fileutil.ValidateCreatableFolderPath(form.BackupDir); err != nil {
			return fmt.Errorf("备份目录无效：%w", err)
		}
	}
	return nil
}

func browseFolderRoot(current string) string {
	return ""
}

func (w *MainWindow) runAction(status string, handler ActionHandler) {
	if w == nil || handler == nil || w.runningAction {
		return
	}

	form := w.currentFormState()
	if err := validateDraftForm(form); err != nil {
		w.showValidationMessage(err.Error())
		return
	}
	w.runningAction = true
	w.setRunningState(status)
	if w.progressModel != nil {
		w.progressModel.Reset(nil)
	}
	w.viewModel.ProgressRows = nil
	w.updateProgressBar(nil)

	go func() {
		result := handler(form, func(event progress.Event) {
			if w.window == nil {
				return
			}
			w.window.Synchronize(func() {
				w.appendProgressEvent(event)
			})
		})
		if w.window == nil {
			return
		}
		w.window.Synchronize(func() {
			w.runningAction = false
			w.applyActionResult(result)
		})
	}()
}

func (w *MainWindow) currentFormState() FormState {
	return FormState{
		WorkRoot:    textValue(w.workRootEdit),
		DisplayName: textValue(w.displayNameEdit),
		BackupDir:   textValue(w.backupDirEdit),
	}
}

func (w *MainWindow) applyActionResult(result ActionResult) {
	if result.WorkRoot != "" {
		w.viewModel.WorkRoot = result.WorkRoot
		if w.workRootEdit != nil {
			w.workRootEdit.SetText(result.WorkRoot)
		}
	}
	if result.DisplayName != "" {
		w.viewModel.DisplayName = result.DisplayName
		if w.displayNameEdit != nil {
			w.displayNameEdit.SetText(result.DisplayName)
		}
	}
	if result.BackupDir != "" {
		w.viewModel.BackupDir = result.BackupDir
		if w.backupDirEdit != nil {
			w.backupDirEdit.SetText(result.BackupDir)
		}
	}
	if result.DatabasePath != "" {
		w.viewModel.DatabasePath = result.DatabasePath
	}
	w.viewModel.Results = result.Results
	w.viewModel.DriveStatus = result.DriveStatus
	w.viewModel.ProgressRows = append([]progress.Event(nil), result.ProgressRows...)
	w.viewModel.InitializeEnabled = result.InitializeEnabled
	w.viewModel.SyncEnabled = result.SyncEnabled
	w.lastCommittedForm = normalizeFormState(FormState{
		WorkRoot:    w.viewModel.WorkRoot,
		DisplayName: w.viewModel.DisplayName,
		BackupDir:   w.viewModel.BackupDir,
	})

	if w.statusLabel != nil {
		w.statusLabel.SetText(result.DriveStatus)
	}
	if w.resultsBox != nil {
		w.resultsBox.SetText(result.Results.SummaryText())
	}
	if w.progressModel != nil {
		w.progressModel.Reset(result.ProgressRows)
	}
	w.updateProgressBar(result.ProgressRows)
	w.scrollProgressToLatest()
	if w.initButton != nil {
		w.initButton.SetEnabled(result.InitializeEnabled)
	}
	if w.syncButton != nil {
		w.syncButton.SetEnabled(result.SyncEnabled)
	}
	if w.workRootPickBtn != nil {
		w.workRootPickBtn.SetEnabled(!w.runningAction)
	}
	if w.backupPickBtn != nil {
		w.backupPickBtn.SetEnabled(!w.runningAction)
	}
}

func (w *MainWindow) setRunningState(status string) {
	runningSummary := ResultSummary{Status: status}
	w.viewModel.Results = runningSummary
	if w.resultsBox != nil {
		w.resultsBox.SetText(runningSummary.SummaryText())
	}
	if w.statusLabel != nil && strings.TrimSpace(w.viewModel.DriveStatus) != "" {
		w.statusLabel.SetText(w.viewModel.DriveStatus)
	}
	if w.initButton != nil {
		w.initButton.SetEnabled(false)
	}
	if w.syncButton != nil {
		w.syncButton.SetEnabled(false)
	}
	if w.workRootPickBtn != nil {
		w.workRootPickBtn.SetEnabled(false)
	}
	if w.backupPickBtn != nil {
		w.backupPickBtn.SetEnabled(false)
	}
	if w.progressBar != nil {
		_ = w.progressBar.SetMarqueeMode(true)
		w.progressBar.SetValue(0)
	}
}

func (w *MainWindow) showValidationMessage(message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	w.showInlineValidation(message)
	if w.window != nil {
		walk.MsgBox(w.window, "USBSync", message, walk.MsgBoxOK|walk.MsgBoxIconWarning)
	}
}

func (w *MainWindow) showInlineValidation(message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	if w.statusLabel != nil {
		w.statusLabel.SetText(message)
	}
	if w.resultsBox != nil {
		summary := ResultSummary{Status: "请先修正路径"}
		summary.AddFailure(message)
		w.resultsBox.SetText(summary.SummaryText())
	}
	w.applyDraftButtonState(fmt.Errorf("%s", message))
}

func (w *MainWindow) restoreReadyState() {
	if w.statusLabel != nil {
		w.statusLabel.SetText(w.viewModel.DriveStatus)
	}
	if w.resultsBox != nil {
		w.resultsBox.SetText(w.viewModel.Results.SummaryText())
	}
	w.applyDraftButtonState(nil)
	w.updateProgressBar(w.viewModel.ProgressRows)
}

func normalizeFormState(form FormState) FormState {
	return FormState{
		WorkRoot:    fileutil.NormalizeFolderPath(form.WorkRoot),
		DisplayName: strings.TrimSpace(form.DisplayName),
		BackupDir:   fileutil.NormalizeFolderPath(form.BackupDir),
	}
}

func draftChanged(previous, current FormState) bool {
	return normalizeFormState(previous) != normalizeFormState(current)
}

func sameNormalizedPath(current, saved string) bool {
	return fileutil.NormalizeFolderPath(current) == fileutil.NormalizeFolderPath(saved)
}

func (w *MainWindow) appendProgressEvent(event progress.Event) {
	w.viewModel.ProgressRows = append(w.viewModel.ProgressRows, event)
	if w.progressModel != nil {
		w.progressModel.Append(event)
	}
	w.updateProgressBar(w.viewModel.ProgressRows)
	w.scrollProgressToLatest()
}

func (w *MainWindow) updateProgressBar(rows []progress.Event) {
	if w.progressBar == nil {
		return
	}

	value, marquee := progressBarState(rows)
	_ = w.progressBar.SetMarqueeMode(marquee)
	if !marquee {
		w.progressBar.SetValue(value)
	}
}

func progressBarState(rows []progress.Event) (int, bool) {
	if len(rows) == 0 {
		return 0, false
	}

	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		if !row.TotalKnown {
			continue
		}
		if row.Total <= 0 {
			return 100, false
		}
		value := row.Done * 100 / row.Total
		if value < 0 {
			value = 0
		}
		if value > 100 {
			value = 100
		}
		return value, false
	}

	return 0, true
}

func progressValue(rows []progress.Event) int {
	value, _ := progressBarState(rows)
	return value
}

func (w *MainWindow) consumeValidationPopupSuppression() bool {
	if !w.suppressNextValidationPopup {
		return false
	}
	w.suppressNextValidationPopup = false
	return true
}

func textValue(edit *walk.LineEdit) string {
	if edit == nil {
		return ""
	}
	return edit.Text()
}

func (w *MainWindow) applyDraftButtonState(validationErr error) {
	initializeEnabled, syncEnabled := draftButtonState(w.viewModel.InitializeEnabled, w.viewModel.SyncEnabled, validationErr)
	if w.initButton != nil {
		w.initButton.SetEnabled(initializeEnabled)
	}
	if w.syncButton != nil {
		w.syncButton.SetEnabled(syncEnabled)
	}
}

func draftButtonState(baseInitializeEnabled, baseSyncEnabled bool, validationErr error) (bool, bool) {
	if validationErr != nil {
		return false, false
	}
	return baseInitializeEnabled, baseSyncEnabled
}

func (w *MainWindow) scrollProgressToLatest() {
	if w == nil || w.progressViewport == nil {
		return
	}
	lastIndex := len(w.viewModel.ProgressRows) - 1
	if lastIndex < 0 {
		return
	}
	_ = w.progressViewport.SetCurrentIndex(lastIndex)
	w.progressViewport.EnsureItemVisible(lastIndex)
}

func (w *MainWindow) canClose() error {
	if w == nil {
		return nil
	}
	if w.runningAction {
		return fmt.Errorf("正在执行同步，写入数据库期间不能退出")
	}
	if w.viewModel.OnBeforeClose != nil {
		return w.viewModel.OnBeforeClose()
	}
	return nil
}

func (w *MainWindow) applyWindowIcon() {
	if w == nil || w.window == nil {
		return
	}

	if icon, err := walk.NewIconFromResource("APPICON"); err == nil {
		_ = w.window.SetIcon(icon)
		return
	}
	if icon, err := walk.NewIconFromResourceId(1); err == nil {
		_ = w.window.SetIcon(icon)
		return
	}

	// Last fallback for dev runs before resource embedding.
	if exePath, err := os.Executable(); err == nil {
		iconPath := filepath.Join(filepath.Dir(exePath), "usbsync.ico")
		if icon, iconErr := walk.NewIconFromFile(iconPath); iconErr == nil {
			_ = w.window.SetIcon(icon)
		}
	}
}

func progressGroupHeight() int {
	return 360
}

func progressTableHeight() int {
	return 300
}

func resultSummaryGroupHeight() int {
	return 150
}

func resultSummaryBoxHeight() int {
	return 96
}

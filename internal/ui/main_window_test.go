//go:build windows

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	progress "usbsync/internal/sync"
)

type fakeProgressViewport struct {
	currentIndexes []int
	visibleIndexes []int
}

func (f *fakeProgressViewport) SetCurrentIndex(index int) error {
	f.currentIndexes = append(f.currentIndexes, index)
	return nil
}

func (f *fakeProgressViewport) EnsureItemVisible(index int) {
	f.visibleIndexes = append(f.visibleIndexes, index)
}

func TestValidateDraftFormRejectsInvalidWorkRoot(t *testing.T) {
	err := validateDraftForm(FormState{
		WorkRoot:  `bad\path`,
		BackupDir: `C:\backup`,
	})
	if err == nil {
		t.Fatal("expected invalid work root error")
	}
}

func TestValidateDraftFormRejectsMissingFolder(t *testing.T) {
	err := validateDraftForm(FormState{
		WorkRoot:  filepath.Join(t.TempDir(), "missing"),
		BackupDir: `C:\backup`,
	})
	if err == nil {
		t.Fatal("expected missing folder error")
	}
}

func TestValidateDraftFormAcceptsExistingFolders(t *testing.T) {
	workRoot := filepath.Join(t.TempDir(), "work")
	backupDir := filepath.Join(t.TempDir(), "backup")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatalf("mkdir work root: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}

	err := validateDraftForm(FormState{
		WorkRoot:  workRoot,
		BackupDir: backupDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateDraftFormAllowsMissingBackupFolder(t *testing.T) {
	workRoot := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatalf("mkdir work root: %v", err)
	}

	err := validateDraftForm(FormState{
		WorkRoot:  workRoot,
		BackupDir: filepath.Join(t.TempDir(), "missing", "nested", "backup"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBrowseFolderRootShowsAllComputerFolders(t *testing.T) {
	root := browseFolderRoot(`D:\work\docs`)
	if root != "" {
		t.Fatalf("expected empty root to show all folders, got %s", root)
	}
}

func TestConsumeValidationPopupSuppressionOnlyOnce(t *testing.T) {
	window := &MainWindow{suppressNextValidationPopup: true}
	if !window.consumeValidationPopupSuppression() {
		t.Fatal("expected first suppression to be consumed")
	}
	if window.consumeValidationPopupSuppression() {
		t.Fatal("expected suppression to be cleared")
	}
}

func TestDraftChangedIgnoresUnchangedNormalizedValues(t *testing.T) {
	previous := FormState{
		WorkRoot:    `D:\work`,
		DisplayName: "Office",
		BackupDir:   `D:\backup`,
	}
	current := FormState{
		WorkRoot:    `D:\work\.`,
		DisplayName: " Office ",
		BackupDir:   `D:\backup`,
	}
	if draftChanged(previous, current) {
		t.Fatal("expected unchanged normalized values to be ignored")
	}
}

func TestSameNormalizedPathTreatsEquivalentPathAsUnchanged(t *testing.T) {
	if !sameNormalizedPath(`D:\work\.`, `D:\work`) {
		t.Fatal("expected equivalent paths to match")
	}
}

func TestProgressBarStateUsesLatestKnownProgress(t *testing.T) {
	value, marquee := progressBarState([]progress.Event{
		{Phase: "scan", Status: "running", TotalKnown: false},
		{Phase: "commit", Done: 2, Total: 4, TotalKnown: true},
	})
	if marquee {
		t.Fatal("expected determinate progress")
	}
	if value != 50 {
		t.Fatalf("unexpected progress value: %d", value)
	}
}

func TestProgressBarStateUsesMarqueeWhenTotalUnknown(t *testing.T) {
	_, marquee := progressBarState([]progress.Event{
		{Phase: "scan", Status: "running", TotalKnown: false},
	})
	if !marquee {
		t.Fatal("expected marquee mode")
	}
}

func TestPhaseLabelUsesChineseName(t *testing.T) {
	if got := phaseLabel("scan"); got != "扫描" {
		t.Fatalf("unexpected phase label: %s", got)
	}
	if got := phaseLabel("initialize"); got != "初始化" {
		t.Fatalf("unexpected phase label: %s", got)
	}
	if got := phaseLabel("commit"); got != "写入 U 盘" {
		t.Fatalf("unexpected phase label: %s", got)
	}
}

func TestResultSummarySectionUsesFixedHeights(t *testing.T) {
	if resultSummaryGroupHeight() != 150 {
		t.Fatalf("unexpected result summary group height: %d", resultSummaryGroupHeight())
	}
	if resultSummaryBoxHeight() != 96 {
		t.Fatalf("unexpected result summary box height: %d", resultSummaryBoxHeight())
	}
}

func TestProgressSectionUsesFixedHeights(t *testing.T) {
	if progressGroupHeight() != 360 {
		t.Fatalf("unexpected progress group height: %d", progressGroupHeight())
	}
	if progressTableHeight() != 300 {
		t.Fatalf("unexpected progress table height: %d", progressTableHeight())
	}
}

func TestDraftButtonStateRecoversAfterValidationError(t *testing.T) {
	initializeEnabled, syncEnabled := draftButtonState(true, true, fmt.Errorf("bad path"))
	if initializeEnabled || syncEnabled {
		t.Fatalf("expected buttons disabled during validation error, got init=%v sync=%v", initializeEnabled, syncEnabled)
	}

	initializeEnabled, syncEnabled = draftButtonState(true, true, nil)
	if !initializeEnabled || !syncEnabled {
		t.Fatalf("expected buttons restored after valid path, got init=%v sync=%v", initializeEnabled, syncEnabled)
	}
}

func TestAppendProgressEventKeepsLatestRowVisible(t *testing.T) {
	viewport := &fakeProgressViewport{}
	window := &MainWindow{
		progressModel:    NewProgressTableModel(),
		progressViewport: viewport,
	}

	window.appendProgressEvent(progress.Event{Phase: "scan", Item: "a"})
	window.appendProgressEvent(progress.Event{Phase: "commit", Item: "b"})

	if len(viewport.currentIndexes) == 0 || viewport.currentIndexes[len(viewport.currentIndexes)-1] != 1 {
		t.Fatalf("expected latest current index 1, got %#v", viewport.currentIndexes)
	}
	if len(viewport.visibleIndexes) == 0 || viewport.visibleIndexes[len(viewport.visibleIndexes)-1] != 1 {
		t.Fatalf("expected latest visible index 1, got %#v", viewport.visibleIndexes)
	}
}

func TestApplyActionResultKeepsLatestRowVisible(t *testing.T) {
	viewport := &fakeProgressViewport{}
	window := &MainWindow{
		progressModel:    NewProgressTableModel(),
		progressViewport: viewport,
	}

	window.applyActionResult(ActionResult{
		DriveStatus: "当前 U 盘：E:",
		Results:     ResultSummary{Status: "同步完成"},
		ProgressRows: []progress.Event{
			{Phase: "scan", Item: "a"},
			{Phase: "commit", Item: "b"},
			{Phase: "state", Item: "c"},
		},
	})

	if len(viewport.currentIndexes) == 0 || viewport.currentIndexes[len(viewport.currentIndexes)-1] != 2 {
		t.Fatalf("expected latest current index 2, got %#v", viewport.currentIndexes)
	}
	if len(viewport.visibleIndexes) == 0 || viewport.visibleIndexes[len(viewport.visibleIndexes)-1] != 2 {
		t.Fatalf("expected latest visible index 2, got %#v", viewport.visibleIndexes)
	}
}

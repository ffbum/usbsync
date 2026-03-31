# USBSync V1 Foundation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the first runnable USBSync foundation: local toolchain, on-disk machine config, SQLite schema bootstrap, current-drive validation, and a manual main window shell with a real progress model.

**Architecture:** Start from the lowest-risk layers first. Lock down machine-local state and the database contract before wiring the window and sync orchestration, so later behavior is built on tested paths, stable IDs, and a deterministic schema. Keep the first development slice non-destructive where possible: initialize, load, validate, and report before full file mutation.

**Tech Stack:** Go 1.22.12, `modernc.org/sqlite`, `lxn/walk`, standard library file APIs, table-driven Go tests

---

### Task 1: Bootstrap toolchain and repository skeleton

**Files:**
- Create: `go.mod`
- Create: `build.bat`
- Create: `cmd/usbsync/main.go`
- Create: `internal/app/app.go`
- Create: `internal/app/app_test.go`

**Step 1: Write the failing test**

```go
package app

import "testing"

func TestDefaultAppVersionIsSet(t *testing.T) {
	cfg := DefaultBuildInfo()
	if cfg.Version == "" {
		t.Fatal("expected version to be non-empty")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `.\tools\go1.22.12\go\bin\go.exe test ./internal/app -v`
Expected: FAIL because package or symbol does not exist yet

**Step 3: Write minimal implementation**

Create the Go module, add `internal/app`, return a non-empty build version, and make `cmd/usbsync/main.go` call into `app.Run()`.

**Step 4: Run test to verify it passes**

Run: `.\tools\go1.22.12\go\bin\go.exe test ./internal/app -v`
Expected: PASS

**Step 5: Commit**

```bash
git add go.mod build.bat cmd/usbsync/main.go internal/app/app.go internal/app/app_test.go
git commit -m "chore: bootstrap go entrypoint"
```

### Task 2: Machine-local config and default paths

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Modify: `internal/app/app.go`

**Step 1: Write the failing test**

```go
func TestDefaultMachineConfigUsesDotUSBsyncOnC(t *testing.T) {
	cfg := DefaultMachineConfig()
	if cfg.StateDir != `C:\.usbsync` {
		t.Fatalf("unexpected state dir: %s", cfg.StateDir)
	}
	if cfg.MachineConfigPath != `C:\.usbsync\machine.json` {
		t.Fatalf("unexpected machine config path: %s", cfg.MachineConfigPath)
	}
	if cfg.BackupDir != `C:\.usbsync\backup` {
		t.Fatalf("unexpected backup dir: %s", cfg.BackupDir)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `.\tools\go1.22.12\go\bin\go.exe test ./internal/config -v`
Expected: FAIL because config package does not exist yet

**Step 3: Write minimal implementation**

Implement:
- default local paths under `C:\.usbsync`
- machine config struct with `machine_id`, `display_name`, `last_work_root`, `backup_dir`, `bound_device_id`, `bound_volume_id`
- load/save helpers
- lazy directory creation for `C:\.usbsync` and `C:\.usbsync\backup`

**Step 4: Run test to verify it passes**

Run: `.\tools\go1.22.12\go\bin\go.exe test ./internal/config -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/app/app.go
git commit -m "feat: add machine config defaults"
```

### Task 3: SQLite schema bootstrap and store initialization

**Files:**
- Create: `internal/db/schema.go`
- Create: `internal/db/store.go`
- Create: `internal/db/schema_test.go`
- Modify: `go.mod`

**Step 1: Write the failing test**

```go
func TestInitCreatesCoreTables(t *testing.T) {
	db := openTempDB(t)
	if err := InitSchema(db); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	for _, table := range []string{
		"device_meta", "machine_registry", "entries",
		"change_log", "machine_state", "sync_sessions", "sync_log",
	} {
		if !hasTable(t, db, table) {
			t.Fatalf("missing table: %s", table)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `.\tools\go1.22.12\go\bin\go.exe test ./internal/db -v`
Expected: FAIL because schema package does not exist yet

**Step 3: Write minimal implementation**

Add:
- schema creation SQL matching `implementation_plan.md`
- store opener for `USBSync.db`
- rollback-journal-safe open options
- `workspace_generation` fields and `workspace_reset` operation support

**Step 4: Run test to verify it passes**

Run: `.\tools\go1.22.12\go\bin\go.exe test ./internal/db -v`
Expected: PASS

**Step 5: Commit**

```bash
git add go.mod internal/db/schema.go internal/db/store.go internal/db/schema_test.go
git commit -m "feat: add sqlite schema bootstrap"
```

### Task 4: Current-drive validation and startup loading

**Files:**
- Create: `internal/usb/current_drive.go`
- Create: `internal/usb/current_drive_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/config/config.go`

**Step 1: Write the failing test**

```go
func TestDriveContextRequiresExecutableOnRemovableDrive(t *testing.T) {
	_, err := BuildDriveContext(DriveProbe{
		ExePath: `C:\temp\USBSync.exe`,
		IsRemovable: false,
	})
	if err == nil {
		t.Fatal("expected removable-drive validation error")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `.\tools\go1.22.12\go\bin\go.exe test ./internal/usb -v`
Expected: FAIL because usb package does not exist yet

**Step 3: Write minimal implementation**

Implement:
- executable-location probe
- removable-drive validation contract
- detection of `USBSync.db` beside the executable
- startup flow that loads machine config first, then validates current drive

**Step 4: Run test to verify it passes**

Run: `.\tools\go1.22.12\go\bin\go.exe test ./internal/usb -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/usb/current_drive.go internal/usb/current_drive_test.go internal/app/app.go internal/config/config.go
git commit -m "feat: add current drive validation"
```

### Task 5: Main window shell and progress model

**Files:**
- Create: `internal/ui/main_window.go`
- Create: `internal/ui/progress_table.go`
- Create: `internal/ui/results.go`
- Create: `internal/sync/progress.go`
- Create: `internal/sync/progress_test.go`
- Modify: `internal/app/app.go`

**Step 1: Write the failing test**

```go
func TestProgressEventAllowsUnknownTotals(t *testing.T) {
	ev := Event{Phase: "scan", Done: 3, TotalKnown: false}
	if ev.Total != 0 {
		t.Fatalf("expected zero total when unknown, got %d", ev.Total)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `.\tools\go1.22.12\go\bin\go.exe test ./internal/sync -v`
Expected: FAIL because progress model does not exist yet

**Step 3: Write minimal implementation**

Implement:
- main window shell
- fields for work root, display name, backup dir
- buttons for initialize, sync, open backup dir, view results
- progress table model that supports unknown totals first, filled totals later

**Step 4: Run test to verify it passes**

Run: `.\tools\go1.22.12\go\bin\go.exe test ./internal/sync -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/ui/main_window.go internal/ui/progress_table.go internal/ui/results.go internal/sync/progress.go internal/sync/progress_test.go internal/app/app.go
git commit -m "feat: add manual window shell"
```

### Task 6: Non-destructive sync foundation

**Files:**
- Create: `internal/fileutil/path.go`
- Create: `internal/fileutil/fileutil.go`
- Create: `internal/fileindex/cache.go`
- Create: `internal/sync/engine.go`
- Create: `internal/sync/engine_test.go`

**Step 1: Write the failing test**

```go
func TestScanExcludesUSBsyncLocalDirectory(t *testing.T) {
	paths := scanPaths(t, []string{
		`docs\a.txt`,
		`.usbsync-local\logs\run.log`,
	})
	if len(paths) != 1 || paths[0] != "docs/a.txt" {
		t.Fatalf("unexpected scan paths: %#v", paths)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `.\tools\go1.22.12\go\bin\go.exe test ./internal/sync -v`
Expected: FAIL because scanner/engine behavior does not exist yet

**Step 3: Write minimal implementation**

Implement:
- path normalization
- `.usbsync-local` exclusion
- scan cache persistence
- sync engine shell that can scan, validate, emit progress, and stop before destructive mutation

**Step 4: Run test to verify it passes**

Run: `.\tools\go1.22.12\go\bin\go.exe test ./internal/sync -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/fileutil/path.go internal/fileutil/fileutil.go internal/fileindex/cache.go internal/sync/engine.go internal/sync/engine_test.go
git commit -m "feat: add sync foundation"
```

### Task 7: Manual machine retirement and workspace reset propagation

**Files:**
- Create: `internal/sync/workspace_reset.go`
- Create: `internal/sync/workspace_reset_test.go`
- Modify: `internal/db/store.go`
- Modify: `internal/ui/results.go`

**Step 1: Write the failing test**

```go
func TestWorkspaceResetRequiresExplicitConfirmation(t *testing.T) {
	err := ApplyWorkspaceReset(false)
	if err == nil {
		t.Fatal("expected confirmation requirement")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `.\tools\go1.22.12\go\bin\go.exe test ./internal/sync -v`
Expected: FAIL because workspace-reset handling does not exist yet

**Step 3: Write minimal implementation**

Implement:
- `workspace_generation` propagation
- `workspace_reset` event creation
- explicit confirmation gate before replacing an old directory
- manual machine retirement plumbing for the future “管理机器” entry

**Step 4: Run test to verify it passes**

Run: `.\tools\go1.22.12\go\bin\go.exe test ./internal/sync -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/sync/workspace_reset.go internal/sync/workspace_reset_test.go internal/db/store.go internal/ui/results.go
git commit -m "feat: add workspace reset rules"
```


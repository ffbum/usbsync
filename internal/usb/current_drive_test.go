package usb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDriveContextRequiresExecutablePath(t *testing.T) {
	_, err := BuildDriveContext(DriveProbe{
		IsRemovable: false,
	})
	if err != ErrExecutablePathMissing {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildDriveContextAllowsUsbThatIsReportedAsFixedDrive(t *testing.T) {
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}

	ctx, err := BuildDriveContext(DriveProbe{
		ExePath:     exePath,
		IsRemovable: false,
		VolumeID:    "vol-fixed",
		VolumeName:  "MyUSB",
	})
	if err != nil {
		t.Fatalf("build drive context: %v", err)
	}
	if ctx.RootPath == "" {
		t.Fatal("expected drive root")
	}
	if ctx.VolumeName != "MyUSB" {
		t.Fatalf("unexpected volume name: %s", ctx.VolumeName)
	}
}

func TestBuildDriveContextReportsDatabasePathWhenDatabaseMissing(t *testing.T) {
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}

	ctx, err := BuildDriveContext(DriveProbe{
		ExePath:     exePath,
		IsRemovable: true,
		VolumeName:  "USB-DISK",
	})
	if err != nil {
		t.Fatalf("build drive context: %v", err)
	}

	if ctx.DBPath != filepath.Join(exeDir, DatabaseFileName) {
		t.Fatalf("unexpected db path: %s", ctx.DBPath)
	}
	if ctx.DatabaseExists {
		t.Fatal("expected database to be marked missing")
	}
	if ctx.DBPath != filepath.Join(exeDir, DatabaseFileName) {
		t.Fatalf("unexpected db path: %s", ctx.DBPath)
	}
}

func TestBuildDriveContextDetectsDatabaseBesideExecutable(t *testing.T) {
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "USBSync.exe")
	dbPath := filepath.Join(exeDir, DatabaseFileName)

	if err := os.WriteFile(exePath, []byte("exe"), 0o644); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	if err := os.WriteFile(dbPath, []byte("db"), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}

	ctx, err := BuildDriveContext(DriveProbe{
		ExePath:     exePath,
		IsRemovable: true,
		VolumeID:    "vol-123",
		VolumeName:  "WORKUSB",
	})
	if err != nil {
		t.Fatalf("build drive context: %v", err)
	}

	if ctx.DBPath != dbPath {
		t.Fatalf("unexpected db path: %s", ctx.DBPath)
	}
	if !ctx.DatabaseExists {
		t.Fatal("expected database to be marked present")
	}
	if ctx.VolumeID != "vol-123" {
		t.Fatalf("unexpected volume id: %s", ctx.VolumeID)
	}
	if ctx.VolumeName != "WORKUSB" {
		t.Fatalf("unexpected volume name: %s", ctx.VolumeName)
	}
}

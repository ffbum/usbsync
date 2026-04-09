package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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

func TestPortableMachineConfigPathUsesProgramDirectory(t *testing.T) {
	exePath := filepath.Join(`E:\apps`, "USBSync.exe")
	path := PortableMachineConfigPath(exePath)

	expected := filepath.Join(`E:\apps`, "usbsync.json")
	if path != expected {
		t.Fatalf("unexpected portable config path: %s", path)
	}
}

func TestSaveAndLoadMachineConfigForMachineKeepsMachinesSeparate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usbsync.json")

	office := DefaultMachineConfig()
	office.DisplayName = "Office"
	office.LastWorkRoot = `D:\office`
	if err := SaveMachineConfigForMachine(path, "Office-PC", office); err != nil {
		t.Fatalf("save office config: %v", err)
	}

	home := DefaultMachineConfig()
	home.DisplayName = "Home"
	home.LastWorkRoot = `D:\home`
	if err := SaveMachineConfigForMachine(path, "Home-PC", home); err != nil {
		t.Fatalf("save home config: %v", err)
	}

	loadedOffice, err := LoadMachineConfigForMachine(path, "Office-PC")
	if err != nil {
		t.Fatalf("load office config: %v", err)
	}
	loadedHome, err := LoadMachineConfigForMachine(path, "Home-PC")
	if err != nil {
		t.Fatalf("load home config: %v", err)
	}

	if loadedOffice.DisplayName != "Office" || loadedOffice.LastWorkRoot != `D:\office` {
		t.Fatalf("unexpected office config: %#v", loadedOffice)
	}
	if loadedHome.DisplayName != "Home" || loadedHome.LastWorkRoot != `D:\home` {
		t.Fatalf("unexpected home config: %#v", loadedHome)
	}
}

func TestLoadMachineConfigForKeysFallsBackToSingleLegacyEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usbsync.json")

	cfg := DefaultMachineConfig()
	cfg.DisplayName = "Office"
	cfg.LastWorkRoot = `D:\office`
	if err := SaveMachineConfigForMachine(path, "MININT-ABCD", cfg); err != nil {
		t.Fatalf("save legacy config: %v", err)
	}

	loaded, err := LoadMachineConfigForKeys(path, "{hardware-guid}", "MININT-ABCD")
	if err != nil {
		t.Fatalf("load config with fallback: %v", err)
	}
	if loaded.DisplayName != "Office" || loaded.LastWorkRoot != `D:\office` {
		t.Fatalf("unexpected loaded config: %#v", loaded)
	}
}

func TestLoadMachineConfigForKeysDoesNotCrossLoadWhenMultipleEntriesExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usbsync.json")

	office := DefaultMachineConfig()
	office.DisplayName = "Office"
	if err := SaveMachineConfigForMachine(path, "OFFICE-HOST", office); err != nil {
		t.Fatalf("save office config: %v", err)
	}

	home := DefaultMachineConfig()
	home.DisplayName = "Home"
	if err := SaveMachineConfigForMachine(path, "HOME-HOST", home); err != nil {
		t.Fatalf("save home config: %v", err)
	}

	_, err := LoadMachineConfigForKeys(path, "{hardware-guid}", "OFFICE-HOST")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no fallback match for multi-entry config, got %v", err)
	}
}

func TestSaveAndLoadMachineConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "machine.json")
	cfg := DefaultMachineConfig()
	cfg.DisplayName = "Office"
	cfg.LastWorkRoot = `D:\work`

	if err := SaveMachineConfig(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded, err := LoadMachineConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if loaded.DisplayName != cfg.DisplayName {
		t.Fatalf("display name mismatch: %s", loaded.DisplayName)
	}
	if loaded.LastWorkRoot != cfg.LastWorkRoot {
		t.Fatalf("work root mismatch: %s", loaded.LastWorkRoot)
	}
	if loaded.BackupDir != cfg.BackupDir {
		t.Fatalf("backup dir mismatch: %s", loaded.BackupDir)
	}
}

func TestLoadMachineConfigOrDefaultReturnsDefaultsWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "machine.json")

	cfg, err := LoadMachineConfigOrDefault(path)
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}

	if cfg.MachineConfigPath != path {
		t.Fatalf("unexpected machine config path: %s", cfg.MachineConfigPath)
	}
	if cfg.StateDir != filepath.Dir(path) {
		t.Fatalf("unexpected state dir: %s", cfg.StateDir)
	}
	if cfg.BackupDir != `C:\.usbsync\backup` {
		t.Fatalf("unexpected backup dir: %s", cfg.BackupDir)
	}
}

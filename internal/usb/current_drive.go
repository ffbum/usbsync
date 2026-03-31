//go:build windows

package usb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	DatabaseFileName = "USBSync.db"

	driveTypeRemovable = 2
)

var (
	ErrExecutablePathMissing = errors.New("missing executable path")
	ErrDriveRootMissing      = errors.New("program path must be on a mounted drive")

	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetDriveTypeW        = kernel32.NewProc("GetDriveTypeW")
	procGetVolumeInformation = kernel32.NewProc("GetVolumeInformationW")
)

type DriveProbe struct {
	ExePath     string
	IsRemovable bool
	VolumeID    string
	VolumeName  string
}

type DriveContext struct {
	ExePath        string
	ExeDir         string
	RootPath       string
	DBPath         string
	DatabaseExists bool
	IsRemovable    bool
	VolumeID       string
	VolumeName     string
}

func BuildDriveContext(probe DriveProbe) (DriveContext, error) {
	if probe.ExePath == "" {
		return DriveContext{}, ErrExecutablePathMissing
	}

	exeDir := filepath.Dir(probe.ExePath)
	dbPath := filepath.Join(exeDir, DatabaseFileName)
	dbExists := true
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			dbExists = false
		} else {
			return DriveContext{}, err
		}
	}

	rootPath := filepath.VolumeName(probe.ExePath)
	if rootPath == "" {
		return DriveContext{}, ErrDriveRootMissing
	}
	if rootPath[len(rootPath)-1] != '\\' {
		rootPath += `\`
	}

	return DriveContext{
		ExePath:        probe.ExePath,
		ExeDir:         exeDir,
		RootPath:       rootPath,
		DBPath:         dbPath,
		DatabaseExists: dbExists,
		IsRemovable:    probe.IsRemovable,
		VolumeID:       probe.VolumeID,
		VolumeName:     probe.VolumeName,
	}, nil
}

func ProbeCurrentDrive() (DriveProbe, error) {
	exePath, err := os.Executable()
	if err != nil {
		return DriveProbe{}, err
	}

	rootPath := filepath.VolumeName(exePath)
	if rootPath == "" {
		return DriveProbe{}, ErrExecutablePathMissing
	}
	rootPath += `\`

	isRemovable, err := isRemovableDrive(rootPath)
	if err != nil {
		return DriveProbe{}, err
	}

	volumeName, volumeID, err := volumeInfo(rootPath)
	if err != nil {
		return DriveProbe{}, err
	}

	return DriveProbe{
		ExePath:     exePath,
		IsRemovable: isRemovable,
		VolumeID:    volumeID,
		VolumeName:  volumeName,
	}, nil
}

func isRemovableDrive(rootPath string) (bool, error) {
	rootPtr, err := syscall.UTF16PtrFromString(rootPath)
	if err != nil {
		return false, err
	}

	r1, _, callErr := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(rootPtr)))
	if r1 == 0 {
		return false, fmt.Errorf("query drive type: %w", callErr)
	}

	return r1 == driveTypeRemovable, nil
}

func volumeInfo(rootPath string) (string, string, error) {
	rootPtr, err := syscall.UTF16PtrFromString(rootPath)
	if err != nil {
		return "", "", err
	}

	var (
		volumeName [261]uint16
		serial     uint32
		maxCompLen uint32
		flags      uint32
	)

	r1, _, callErr := procGetVolumeInformation.Call(
		uintptr(unsafe.Pointer(rootPtr)),
		uintptr(unsafe.Pointer(&volumeName[0])),
		uintptr(len(volumeName)),
		uintptr(unsafe.Pointer(&serial)),
		uintptr(unsafe.Pointer(&maxCompLen)),
		uintptr(unsafe.Pointer(&flags)),
		0,
		0,
	)
	if r1 == 0 {
		return "", "", fmt.Errorf("query volume serial: %w", callErr)
	}

	return syscall.UTF16ToString(volumeName[:]), fmt.Sprintf("%08X", serial), nil
}

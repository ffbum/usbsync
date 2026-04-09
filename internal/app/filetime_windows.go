//go:build windows

package app

import "golang.org/x/sys/windows"

func setCreationTime(path string, ctimeNS int64) error {
	if ctimeNS <= 0 {
		return nil
	}

	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}

	handle, err := windows.CreateFile(
		pathPtr,
		windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	creationTime := windows.NsecToFiletime(ctimeNS)
	return windows.SetFileTime(handle, &creationTime, nil, nil)
}

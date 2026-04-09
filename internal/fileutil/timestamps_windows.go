//go:build windows

package fileutil

import (
	"io/fs"
	"syscall"
)

func creationTimeUnixNano(info fs.FileInfo) int64 {
	if info == nil {
		return 0
	}

	attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || attributes == nil {
		return 0
	}

	return attributes.CreationTime.Nanoseconds()
}

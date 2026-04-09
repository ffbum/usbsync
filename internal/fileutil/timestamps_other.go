//go:build !windows

package fileutil

import "io/fs"

func creationTimeUnixNano(_ fs.FileInfo) int64 {
	return 0
}

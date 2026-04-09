//go:build windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestApplyRecordedTimesSetsCreationAndModifiedTimes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ctime := time.Date(2024, 1, 2, 3, 4, 5, 123456700, time.UTC).UnixNano()
	mtime := time.Date(2024, 2, 3, 4, 5, 6, 765432100, time.UTC).UnixNano()
	if err := applyRecordedTimes(path, ctime, mtime); err != nil {
		t.Fatalf("apply recorded times: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}

	if !withinTimestampTolerance(info.ModTime().UnixNano(), mtime, time.Second) {
		t.Fatalf("unexpected mtime: got=%d want=%d", info.ModTime().UnixNano(), mtime)
	}

	gotCtime, err := fileCreationTimeUnixNano(info)
	if err != nil {
		t.Fatalf("read creation time: %v", err)
	}
	if !withinTimestampTolerance(gotCtime, ctime, time.Second) {
		t.Fatalf("unexpected ctime: got=%d want=%d", gotCtime, ctime)
	}
}

func fileCreationTimeUnixNano(info os.FileInfo) (int64, error) {
	attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || attributes == nil {
		return 0, fmt.Errorf("missing Win32 file attributes")
	}
	return attributes.CreationTime.Nanoseconds(), nil
}

func withinTimestampTolerance(got, want int64, tolerance time.Duration) bool {
	delta := got - want
	if delta < 0 {
		delta = -delta
	}
	return delta <= int64(tolerance)
}

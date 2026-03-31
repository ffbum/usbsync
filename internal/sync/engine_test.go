package sync

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestScanExcludesUSBsyncLocalDirectory(t *testing.T) {
	paths := scanFilePaths(t, []string{
		`docs\a.txt`,
		`.usbsync-local\logs\run.log`,
	})
	if len(paths) != 1 || paths[0] != "docs/a.txt" {
		t.Fatalf("unexpected scan paths: %#v", paths)
	}
}

func TestScanNormalizesPathKeyForWindowsComparison(t *testing.T) {
	paths := scanFilePaths(t, []string{
		`Docs\Readme.TXT`,
	})

	if len(paths) != 1 || paths[0] != "docs/readme.txt" {
		t.Fatalf("unexpected normalized paths: %#v", paths)
	}
}

func TestEngineStopsBeforeDestructiveMutation(t *testing.T) {
	workRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(workRoot, "docs", "a.txt"), "hello")

	engine := NewEngine()
	result, err := engine.Run(Options{
		WorkRoot: workRoot,
		Mode:     ModePreview,
	})
	if err != nil {
		t.Fatalf("run engine: %v", err)
	}

	if result.MutationAttempted {
		t.Fatal("expected non-destructive preview run")
	}
	if countFiles(result.Entries) != 1 {
		t.Fatalf("unexpected entry count: %d", len(result.Entries))
	}
	if len(result.Progress) == 0 {
		t.Fatal("expected progress events")
	}
}

func TestEngineScanProgressIncludesChangeBreakdown(t *testing.T) {
	workRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(workRoot, "docs", "new.txt"), "hello")

	result, err := NewEngine().Run(Options{
		WorkRoot: workRoot,
		Mode:     ModePreview,
		KnownEntries: []KnownEntry{
			{
				PathKey:      "docs",
				DisplayPath:  "docs",
				Kind:         "dir",
				LastRevision: 2,
			},
			{
				PathKey:      "docs/old.txt",
				DisplayPath:  "docs/old.txt",
				Kind:         "file",
				LastRevision: 3,
			},
		},
	})
	if err != nil {
		t.Fatalf("run engine: %v", err)
	}
	if len(result.Progress) == 0 {
		t.Fatal("expected progress events")
	}

	last := result.Progress[len(result.Progress)-1]
	if last.Status != "完成" {
		t.Fatalf("expected completed scan status, got %s", last.Status)
	}
	if !strings.Contains(last.Detail, "新增 1 项") {
		t.Fatalf("expected add count in detail, got %s", last.Detail)
	}
	if !strings.Contains(last.Detail, "删除 1 项") {
		t.Fatalf("expected delete count in detail, got %s", last.Detail)
	}
}

func TestScanCacheRoundTrip(t *testing.T) {
	workRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(workRoot, "docs", "a.txt"), "hello")

	engine := NewEngine()
	first, err := engine.Run(Options{WorkRoot: workRoot, Mode: ModePreview})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	cachePath := filepath.Join(workRoot, ".usbsync-local", "scan_cache.json")
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected cache file: %v", err)
	}

	second, err := engine.Run(Options{WorkRoot: workRoot, Mode: ModePreview})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	if len(second.CacheEntries) != len(first.Entries) {
		t.Fatalf("unexpected cache entry count: %d", len(second.CacheEntries))
	}
}

func TestEngineBuildsLocalChangesAgainstKnownEntries(t *testing.T) {
	workRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(workRoot, "docs", "new.txt"), "hello")

	result, err := NewEngine().Run(Options{
		WorkRoot: workRoot,
		Mode:     ModePreview,
		KnownEntries: []KnownEntry{
			{
				PathKey:      "docs/old.txt",
				DisplayPath:  "docs/old.txt",
				Kind:         "file",
				LastRevision: 4,
			},
		},
	})
	if err != nil {
		t.Fatalf("run engine: %v", err)
	}

	fileChanges := filterFileChanges(result.LocalChanges)
	if len(fileChanges) != 2 {
		t.Fatalf("unexpected file change count: %d", len(fileChanges))
	}
}

func TestEngineBuildsMergePreview(t *testing.T) {
	workRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(workRoot, "docs", "report.txt"), "hello")

	result, err := NewEngine().Run(Options{
		WorkRoot: workRoot,
		Mode:     ModePreview,
		KnownEntries: []KnownEntry{
			{
				PathKey:      "docs",
				DisplayPath:  "docs",
				Kind:         "dir",
				LastRevision: 3,
			},
			{
				PathKey:      "docs/report.txt",
				DisplayPath:  "docs/report.txt",
				Kind:         "file",
				Size:         1,
				MtimeNS:      1,
				LastRevision: 3,
			},
		},
		RemoteChanges: []RemoteChange{
			{
				Revision:     6,
				Op:           "modify",
				PathKey:      "docs/report.txt",
				DisplayPath:  "docs/report.txt",
				Kind:         "file",
				BaseRevision: 3,
				MachineName:  "Office PC",
			},
		},
	})
	if err != nil {
		t.Fatalf("run engine: %v", err)
	}

	filePreview := findPreview(result.MergePreview, "docs/report.txt")
	if filePreview == nil {
		t.Fatal("expected preview entry for docs/report.txt")
	}
	if filePreview.Decision != DecisionConflict {
		t.Fatalf("unexpected merge decision: %s", filePreview.Decision)
	}
}

func TestEngineCommitModeCommitsLocalFileChanges(t *testing.T) {
	workRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(workRoot, "docs", "new.txt"), "hello")

	committer := &fakeCommitter{}
	result, err := NewEngine().Run(Options{
		WorkRoot:  workRoot,
		Mode:      ModeCommitLocal,
		MachineID: "m1",
		SeenAt:    "2026-03-30T00:00:00Z",
		Committer: committer,
	})
	if err != nil {
		t.Fatalf("run engine: %v", err)
	}

	if len(committer.commits) == 0 {
		t.Fatal("expected commit calls")
	}
	fileCommit := findFileCommit(committer.commits)
	if fileCommit == nil {
		t.Fatal("expected file commit")
	}
	if fileCommit.change.PathKey != "docs/new.txt" {
		t.Fatalf("unexpected committed path: %s", fileCommit.change.PathKey)
	}
	if fileCommit.blob.BlobID == "" {
		t.Fatal("expected blob id for file commit")
	}
	if result.MutationAttempted != true {
		t.Fatal("expected commit mode to mark mutation attempted")
	}
}

func TestEngineDoesNotTreatSameContentWithDifferentTimestampAsLocalModify(t *testing.T) {
	workRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(workRoot, "docs", "a.txt"), "hello")
	sum := md5.Sum([]byte("hello"))

	result, err := NewEngine().Run(Options{
		WorkRoot: workRoot,
		Mode:     ModePreview,
		KnownEntries: []KnownEntry{
			{
				PathKey:      "docs",
				DisplayPath:  "docs",
				Kind:         "dir",
				MtimeNS:      1,
				LastRevision: 2,
			},
			{
				PathKey:      "docs/a.txt",
				DisplayPath:  "docs/a.txt",
				Kind:         "file",
				Size:         5,
				MtimeNS:      1,
				MD5:          hex.EncodeToString(sum[:]),
				LastRevision: 3,
			},
		},
	})
	if err != nil {
		t.Fatalf("run engine: %v", err)
	}
	if len(result.LocalChanges) != 0 {
		t.Fatalf("expected no local changes, got %#v", result.LocalChanges)
	}
}

func scanFilePaths(t *testing.T, relPaths []string) []string {
	t.Helper()

	workRoot := t.TempDir()
	for _, relPath := range relPaths {
		mustWriteFile(t, filepath.Join(workRoot, relPath), relPath)
	}

	result, err := NewEngine().Run(Options{
		WorkRoot: workRoot,
		Mode:     ModePreview,
	})
	if err != nil {
		t.Fatalf("scan work root: %v", err)
	}

	paths := make([]string, 0, len(result.Entries))
	for _, entry := range result.Entries {
		if entry.Kind == "file" {
			paths = append(paths, entry.PathKey)
		}
	}
	slices.Sort(paths)

	return paths
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func countFiles(entries []ScannedEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.Kind == "file" {
			count++
		}
	}
	return count
}

func filterFileChanges(changes []LocalChange) []LocalChange {
	filtered := make([]LocalChange, 0, len(changes))
	for _, change := range changes {
		if change.Kind == "file" {
			filtered = append(filtered, change)
		}
	}
	return filtered
}

func findPreview(preview []MergeResolution, pathKey string) *MergeResolution {
	for _, item := range preview {
		if item.PathKey == pathKey {
			copy := item
			return &copy
		}
	}
	return nil
}

func findFileCommit(commits []fakeCommit) *fakeCommit {
	for _, item := range commits {
		if item.change.Kind == "file" {
			copy := item
			return &copy
		}
	}
	return nil
}

type fakeCommitter struct {
	commits []fakeCommit
}

type fakeCommit struct {
	machineID string
	change    LocalChange
	blob      BlobWrite
	seenAt    string
}

func (f *fakeCommitter) CommitLocalChange(machineID string, change LocalChange, blob BlobWrite, seenAt string) (int64, error) {
	f.commits = append(f.commits, fakeCommit{
		machineID: machineID,
		change:    change,
		blob:      blob,
		seenAt:    seenAt,
	})
	return int64(len(f.commits)), nil
}

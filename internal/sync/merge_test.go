package sync

import "testing"

func TestBuildLocalChangesDetectsAddModifyAndDelete(t *testing.T) {
	current := []ScannedEntry{
		{
			PathKey:     "docs/new.txt",
			DisplayPath: "docs/new.txt",
			Kind:        "file",
			Size:        10,
			MtimeNS:     20,
		},
		{
			PathKey:      "docs/existing.txt",
			DisplayPath:  "docs/existing.txt",
			Kind:         "file",
			Size:         99,
			MtimeNS:      200,
			LastRevision: 2,
		},
	}
	known := []KnownEntry{
		{
			PathKey:      "docs/existing.txt",
			DisplayPath:  "docs/existing.txt",
			Kind:         "file",
			Size:         50,
			MtimeNS:      100,
			LastRevision: 2,
		},
		{
			PathKey:      "docs/old.txt",
			DisplayPath:  "docs/old.txt",
			Kind:         "file",
			Size:         5,
			MtimeNS:      30,
			LastRevision: 4,
		},
	}

	changes := BuildLocalChanges(current, known)
	if len(changes) != 3 {
		t.Fatalf("unexpected change count: %d", len(changes))
	}

	byPath := make(map[string]LocalChange, len(changes))
	for _, change := range changes {
		byPath[change.PathKey] = change
	}

	assertChange(t, byPath["docs/new.txt"], "add", "docs/new.txt", 0)
	assertChange(t, byPath["docs/existing.txt"], "modify", "docs/existing.txt", 2)
	assertChange(t, byPath["docs/old.txt"], "delete", "docs/old.txt", 4)
}

func TestBuildLocalChangesIgnoresDirectoryTimestampDifferences(t *testing.T) {
	current := []ScannedEntry{
		{
			PathKey:     "docs",
			DisplayPath: "docs",
			Kind:        "dir",
			MtimeNS:     200,
		},
	}
	known := []KnownEntry{
		{
			PathKey:      "docs",
			DisplayPath:  "docs",
			Kind:         "dir",
			MtimeNS:      100,
			LastRevision: 3,
		},
	}

	changes := BuildLocalChanges(current, known)
	if len(changes) != 0 {
		t.Fatalf("expected no directory-only changes, got %#v", changes)
	}
}

func TestResolveMergeMarksConflictForDualModify(t *testing.T) {
	local := &LocalChange{
		Op:           "modify",
		PathKey:      "docs/report.txt",
		DisplayPath:  "docs/report.txt",
		Kind:         "file",
		BaseRevision: 3,
	}
	remote := &RemoteChange{
		Revision:     6,
		Op:           "modify",
		PathKey:      "docs/report.txt",
		DisplayPath:  "docs/report.txt",
		Kind:         "file",
		BaseRevision: 3,
	}

	resolution := ResolveMerge(local, remote, "Office PC")
	if resolution.Decision != DecisionConflict {
		t.Fatalf("unexpected decision: %s", resolution.Decision)
	}
	expectedConflict := "docs/report (conflict-office-pc-r6).txt"
	if resolution.ConflictDisplayPath != expectedConflict {
		t.Fatalf("unexpected conflict path: %s", resolution.ConflictDisplayPath)
	}
}

func TestResolveMergeKeepsModifiedVersionWhenDeleteMeetsModify(t *testing.T) {
	local := &LocalChange{
		Op:           "delete",
		PathKey:      "docs/report.txt",
		DisplayPath:  "docs/report.txt",
		Kind:         "file",
		BaseRevision: 3,
	}
	remote := &RemoteChange{
		Revision:     6,
		Op:           "modify",
		PathKey:      "docs/report.txt",
		DisplayPath:  "docs/report.txt",
		Kind:         "file",
		BaseRevision: 3,
	}

	resolution := ResolveMerge(local, remote, "Office PC")
	if resolution.Decision != DecisionKeepRemoteWithWarning {
		t.Fatalf("unexpected decision: %s", resolution.Decision)
	}
	if resolution.Warning == "" {
		t.Fatal("expected warning for delete versus modify")
	}
}

func assertChange(t *testing.T, change LocalChange, wantOp, wantPath string, wantBaseRevision int64) {
	t.Helper()

	if change.Op != wantOp {
		t.Fatalf("unexpected op for %s: %s", wantPath, change.Op)
	}
	if change.PathKey != wantPath {
		t.Fatalf("unexpected path: %s", change.PathKey)
	}
	if change.BaseRevision != wantBaseRevision {
		t.Fatalf("unexpected base revision for %s: %d", wantPath, change.BaseRevision)
	}
}

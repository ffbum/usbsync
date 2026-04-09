package sync

import (
	"errors"
	"fmt"
	"path/filepath"

	"usbsync/internal/fileindex"
	"usbsync/internal/fileutil"
)

type Mode string

const (
	ModePreview     Mode = "preview"
	ModeCommitLocal Mode = "commit_local"
)

type BlobWrite struct {
	BlobID string
	Chunks [][]byte
}

type ChangeCommitter interface {
	CommitLocalChange(machineID string, change LocalChange, blob BlobWrite, seenAt string) (int64, error)
}

type Options struct {
	WorkRoot      string
	Mode          Mode
	KnownEntries  []KnownEntry
	RemoteChanges []RemoteChange
	MachineID     string
	SeenAt        string
	Committer     ChangeCommitter
	Progress      func(Event)
}

type ScannedEntry = fileutil.Entry

type Result struct {
	Entries            []ScannedEntry
	CacheEntries       []ScannedEntry
	LocalChanges       []LocalChange
	MergePreview       []MergeResolution
	CommittedRevisions []int64
	Progress           []Event
	MutationAttempted  bool
}

type Engine struct{}

func NewEngine() *Engine {
	return &Engine{}
}

func (e *Engine) Run(options Options) (Result, error) {
	if options.WorkRoot == "" {
		return Result{}, errors.New("missing work root")
	}
	if options.Mode == "" {
		options.Mode = ModePreview
	}
	if options.Mode != ModePreview && options.Mode != ModeCommitLocal {
		return Result{}, errors.New("unsupported mode")
	}

	cachePath := fileindex.DefaultCachePath(options.WorkRoot)
	cacheEntries, err := fileindex.Load(cachePath)
	if err != nil {
		return Result{}, err
	}
	comparisonKnown := options.KnownEntries
	if len(cacheEntries) > 0 {
		comparisonKnown = knownEntriesFromCache(cacheEntries)
	}

	progress := []Event{
		{
			Phase:      "scan",
			Status:     "扫描中",
			Detail:     "正在扫描工作目录",
			TotalKnown: false,
		},
	}
	emitProgress(options.Progress, progress[0])

	entries, err := fileutil.ScanWorktree(options.WorkRoot)
	if err != nil {
		return Result{}, err
	}
	entries, err = enrichEntriesForComparison(options.WorkRoot, entries, options.KnownEntries)
	if err != nil {
		return Result{}, err
	}

	localChanges := BuildLocalChanges(entries, comparisonKnown)
	mergePreview := BuildMergePreview(localChanges, options.RemoteChanges)
	localBreakdown := CountLocalChangeBreakdown(localChanges)

	progress = append(progress, Event{
		Phase:      "scan",
		Status:     "完成",
		Detail:     fmt.Sprintf("扫描完成：%s", localBreakdown.SummaryText()),
		Done:       len(entries),
		Total:      len(entries),
		TotalKnown: true,
	})
	emitProgress(options.Progress, progress[len(progress)-1])

	if err := fileindex.Save(cachePath, entries); err != nil {
		return Result{}, err
	}

	mutationAttempted := false
	committedRevisions := make([]int64, 0)
	if options.Mode == ModeCommitLocal {
		if options.Committer == nil {
			return Result{}, errors.New("missing committer")
		}
		if options.MachineID == "" {
			return Result{}, errors.New("missing machine id")
		}

		for _, change := range localChanges {
			blob := BlobWrite{}
			if change.Kind == "file" && change.Op != "delete" {
				blobData, err := fileutil.ReadFileBlob(filepath.Join(options.WorkRoot, filepath.FromSlash(change.DisplayPath)))
				if err != nil {
					return Result{}, err
				}
				blob = BlobWrite{
					BlobID: blobData.BlobID,
					Chunks: blobData.Chunks,
				}
			}

			revision, err := options.Committer.CommitLocalChange(options.MachineID, change, blob, options.SeenAt)
			if err != nil {
				return Result{}, err
			}
			committedRevisions = append(committedRevisions, revision)
			mutationAttempted = true
		}
	}

	return Result{
		Entries:            entries,
		CacheEntries:       cacheEntries,
		LocalChanges:       localChanges,
		MergePreview:       mergePreview,
		CommittedRevisions: committedRevisions,
		Progress:           progress,
		MutationAttempted:  mutationAttempted,
	}, nil
}

func emitProgress(callback func(Event), event Event) {
	if callback != nil {
		callback(event)
	}
}

func knownEntriesFromCache(entries []fileutil.Entry) []KnownEntry {
	known := make([]KnownEntry, 0, len(entries))
	for _, entry := range entries {
		known = append(known, KnownEntry{
			PathKey:      entry.PathKey,
			DisplayPath:  entry.DisplayPath,
			Kind:         entry.Kind,
			Size:         entry.Size,
			CtimeNS:      entry.CtimeNS,
			MtimeNS:      entry.MtimeNS,
			MD5:          entry.MD5,
			LastRevision: entry.LastRevision,
		})
	}
	return known
}

func enrichEntriesForComparison(workRoot string, entries []ScannedEntry, known []KnownEntry) ([]ScannedEntry, error) {
	if len(entries) == 0 || len(known) == 0 {
		return entries, nil
	}

	knownByPath := make(map[string]KnownEntry, len(known))
	for _, entry := range known {
		knownByPath[entry.PathKey] = entry
	}

	updated := append([]ScannedEntry(nil), entries...)
	for i := range updated {
		entry := &updated[i]
		if entry.Kind != "file" {
			continue
		}

		knownEntry, ok := knownByPath[entry.PathKey]
		if !ok || knownEntry.Deleted {
			continue
		}
		if knownEntry.MD5 == "" {
			continue
		}
		if entry.Size != knownEntry.Size {
			continue
		}

		blob, err := fileutil.ReadFileBlob(filepath.Join(workRoot, filepath.FromSlash(entry.DisplayPath)))
		if err != nil {
			return nil, err
		}
		entry.MD5 = blob.BlobID
	}

	return updated, nil
}

package sync

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

type KnownEntry struct {
	PathKey      string
	DisplayPath  string
	Kind         string
	Size         int64
	MtimeNS      int64
	MD5          string
	Deleted      bool
	LastRevision int64
}

type LocalChange struct {
	Op           string
	PathKey      string
	DisplayPath  string
	Kind         string
	Size         int64
	MtimeNS      int64
	MD5          string
	BaseRevision int64
}

type RemoteChange struct {
	Revision     int64
	Op           string
	PathKey      string
	DisplayPath  string
	Kind         string
	BaseRevision int64
	MachineName  string
}

type Decision string

const (
	DecisionNoop                  Decision = "noop"
	DecisionCommitLocal           Decision = "commit_local"
	DecisionApplyRemote           Decision = "apply_remote"
	DecisionConflict              Decision = "conflict"
	DecisionKeepLocalWithWarning  Decision = "keep_local_with_warning"
	DecisionKeepRemoteWithWarning Decision = "keep_remote_with_warning"
)

type MergeResolution struct {
	PathKey             string
	Decision            Decision
	ConflictDisplayPath string
	Warning             string
}

func BuildLocalChanges(current []ScannedEntry, known []KnownEntry) []LocalChange {
	currentByPath := make(map[string]ScannedEntry, len(current))
	for _, entry := range current {
		currentByPath[entry.PathKey] = entry
	}

	knownByPath := make(map[string]KnownEntry, len(known))
	for _, entry := range known {
		knownByPath[entry.PathKey] = entry
	}

	changes := make([]LocalChange, 0)
	for _, entry := range current {
		knownEntry, exists := knownByPath[entry.PathKey]
		if !exists || knownEntry.Deleted {
			op := "add"
			if entry.Kind == "dir" {
				op = "mkdir"
			}
			changes = append(changes, LocalChange{
				Op:           op,
				PathKey:      entry.PathKey,
				DisplayPath:  entry.DisplayPath,
				Kind:         entry.Kind,
				Size:         entry.Size,
				MtimeNS:      entry.MtimeNS,
				MD5:          entry.MD5,
				BaseRevision: knownEntry.LastRevision,
			})
			continue
		}

		if differs(entry, knownEntry) {
			changes = append(changes, LocalChange{
				Op:           "modify",
				PathKey:      entry.PathKey,
				DisplayPath:  entry.DisplayPath,
				Kind:         entry.Kind,
				Size:         entry.Size,
				MtimeNS:      entry.MtimeNS,
				MD5:          entry.MD5,
				BaseRevision: knownEntry.LastRevision,
			})
		}
	}

	for _, entry := range known {
		if entry.Deleted {
			continue
		}
		if _, exists := currentByPath[entry.PathKey]; exists {
			continue
		}
		changes = append(changes, LocalChange{
			Op:           "delete",
			PathKey:      entry.PathKey,
			DisplayPath:  entry.DisplayPath,
			Kind:         entry.Kind,
			BaseRevision: entry.LastRevision,
		})
	}

	slices.SortFunc(changes, func(a, b LocalChange) int {
		switch {
		case a.PathKey < b.PathKey:
			return -1
		case a.PathKey > b.PathKey:
			return 1
		default:
			return 0
		}
	})

	return changes
}

func ResolveMerge(local *LocalChange, remote *RemoteChange, remoteDisplayName string) MergeResolution {
	pathKey := ""
	if local != nil {
		pathKey = local.PathKey
	} else if remote != nil {
		pathKey = remote.PathKey
	}

	switch {
	case local == nil && remote == nil:
		return MergeResolution{Decision: DecisionNoop}
	case local == nil:
		return MergeResolution{PathKey: pathKey, Decision: DecisionApplyRemote}
	case remote == nil:
		return MergeResolution{PathKey: pathKey, Decision: DecisionCommitLocal}
	}

	if local.Kind != remote.Kind {
		return MergeResolution{
			PathKey:             pathKey,
			Decision:            DecisionConflict,
			ConflictDisplayPath: ConflictDisplayPath(remote.DisplayPath, remoteDisplayName, remote.Revision),
		}
	}

	switch {
	case isDelete(local.Op) && isDelete(remote.Op):
		return MergeResolution{PathKey: pathKey, Decision: DecisionNoop}
	case isDelete(local.Op) && isModify(remote.Op):
		return MergeResolution{
			PathKey:  pathKey,
			Decision: DecisionKeepRemoteWithWarning,
			Warning:  "一端删除、另一端修改，保留修改版",
		}
	case isModify(local.Op) && isDelete(remote.Op):
		return MergeResolution{
			PathKey:  pathKey,
			Decision: DecisionKeepLocalWithWarning,
			Warning:  "一端修改、另一端删除，保留修改版",
		}
	case isModify(local.Op) && isModify(remote.Op):
		return MergeResolution{
			PathKey:             pathKey,
			Decision:            DecisionConflict,
			ConflictDisplayPath: ConflictDisplayPath(remote.DisplayPath, remoteDisplayName, remote.Revision),
		}
	default:
		return MergeResolution{PathKey: pathKey, Decision: DecisionApplyRemote}
	}
}

func BuildMergePreview(localChanges []LocalChange, remoteChanges []RemoteChange) []MergeResolution {
	localByPath := make(map[string]LocalChange, len(localChanges))
	for _, change := range localChanges {
		localByPath[change.PathKey] = change
	}

	remoteByPath := make(map[string]RemoteChange, len(remoteChanges))
	for _, change := range remoteChanges {
		remoteByPath[change.PathKey] = change
	}

	pathSet := make(map[string]struct{}, len(localByPath)+len(remoteByPath))
	for pathKey := range localByPath {
		pathSet[pathKey] = struct{}{}
	}
	for pathKey := range remoteByPath {
		pathSet[pathKey] = struct{}{}
	}

	pathKeys := make([]string, 0, len(pathSet))
	for pathKey := range pathSet {
		pathKeys = append(pathKeys, pathKey)
	}
	slices.Sort(pathKeys)

	preview := make([]MergeResolution, 0, len(pathKeys))
	for _, pathKey := range pathKeys {
		local := localByPath[pathKey]
		remote := remoteByPath[pathKey]

		var localPtr *LocalChange
		if _, ok := localByPath[pathKey]; ok {
			localCopy := local
			localPtr = &localCopy
		}

		var remotePtr *RemoteChange
		var machineName string
		if _, ok := remoteByPath[pathKey]; ok {
			remoteCopy := remote
			remotePtr = &remoteCopy
			machineName = remoteCopy.MachineName
		}

		preview = append(preview, ResolveMerge(localPtr, remotePtr, machineName))
	}

	return preview
}

func ConflictDisplayPath(displayPath, machineName string, revision int64) string {
	sanitizedName := sanitizeConflictName(machineName)
	ext := path.Ext(displayPath)
	base := strings.TrimSuffix(displayPath, ext)

	return fmt.Sprintf("%s (conflict-%s-r%d)%s", base, sanitizedName, revision, ext)
}

func differs(current ScannedEntry, known KnownEntry) bool {
	if current.Kind != known.Kind {
		return true
	}
	if current.Kind == "dir" {
		return false
	}
	if current.Size != known.Size {
		return true
	}
	if current.MD5 != "" && known.MD5 != "" && current.MD5 != known.MD5 {
		return true
	}
	return false
}

func sanitizeConflictName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return "machine"
	}

	var builder strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}

	sanitized := strings.Trim(builder.String(), "-")
	if sanitized == "" {
		return "machine"
	}
	if len(sanitized) > 32 {
		sanitized = sanitized[:32]
		sanitized = strings.TrimRight(sanitized, "-")
	}
	if sanitized == "" {
		return "machine"
	}

	return sanitized
}

func isDelete(op string) bool {
	return op == "delete"
}

func isModify(op string) bool {
	switch op {
	case "add", "mkdir", "modify", "conflict_copy":
		return true
	default:
		return false
	}
}

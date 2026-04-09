package fileutil

import (
	"crypto/md5"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

type Entry struct {
	PathKey      string `json:"path_key"`
	DisplayPath  string `json:"display_path"`
	Kind         string `json:"kind"`
	Size         int64  `json:"size"`
	CtimeNS      int64  `json:"ctime_ns"`
	MtimeNS      int64  `json:"mtime_ns"`
	MD5          string `json:"md5,omitempty"`
	LastRevision int64  `json:"last_revision,omitempty"`
}

type BlobData struct {
	BlobID string
	Chunks [][]byte
}

const DefaultBlobChunkSize = 16 * 1024 * 1024

func ScanWorktree(workRoot string) ([]Entry, error) {
	entries := make([]Entry, 0)

	err := filepath.WalkDir(workRoot, func(fullPath string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fullPath == workRoot {
			return nil
		}

		relPath, err := filepath.Rel(workRoot, fullPath)
		if err != nil {
			return err
		}

		if IsLocalStateRelativePath(relPath) {
			if dirEntry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := dirEntry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if dirEntry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		pathKey, displayPath, err := NormalizeRelativePath(relPath)
		if err != nil {
			return err
		}

		entry := Entry{
			PathKey:     pathKey,
			DisplayPath: displayPath,
			CtimeNS:     creationTimeUnixNano(info),
			MtimeNS:     info.ModTime().UnixNano(),
		}
		if dirEntry.IsDir() {
			entry.Kind = "dir"
		} else {
			entry.Kind = "file"
			entry.Size = info.Size()
		}

		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.SortFunc(entries, func(a, b Entry) int {
		switch {
		case a.PathKey < b.PathKey:
			return -1
		case a.PathKey > b.PathKey:
			return 1
		default:
			return 0
		}
	})

	return entries, nil
}

func ReadFileBlob(path string) (BlobData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BlobData{}, err
	}

	sum := md5.Sum(data)
	blob := BlobData{
		BlobID: hex.EncodeToString(sum[:]),
		Chunks: make([][]byte, 0, maxChunkCount(len(data), DefaultBlobChunkSize)),
	}

	if len(data) == 0 {
		blob.Chunks = append(blob.Chunks, []byte{})
		return blob, nil
	}

	for start := 0; start < len(data); start += DefaultBlobChunkSize {
		end := start + DefaultBlobChunkSize
		if end > len(data) {
			end = len(data)
		}
		chunk := make([]byte, end-start)
		copy(chunk, data[start:end])
		blob.Chunks = append(blob.Chunks, chunk)
	}

	return blob, nil
}

func maxChunkCount(length, chunkSize int) int {
	if length == 0 {
		return 1
	}
	count := length / chunkSize
	if length%chunkSize != 0 {
		count++
	}
	return count
}

func WriteFileAtomically(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(filepath.Dir(path), ".usbsync-write-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}

	return os.Rename(tempPath, path)
}

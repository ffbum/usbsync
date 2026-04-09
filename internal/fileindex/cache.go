package fileindex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"usbsync/internal/fileutil"
)

type Cache struct {
	Entries []fileutil.Entry `json:"entries"`
}

func DefaultCachePath(workRoot string) string {
	return filepath.Join(workRoot, fileutil.LocalStateDirName, "scan_cache.json")
}

func Load(path string) ([]fileutil.Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var cache Cache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	return cache.Entries, nil
}

func Save(path string, entries []fileutil.Entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	cache := Cache{Entries: entries}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

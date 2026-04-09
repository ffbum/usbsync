package fileutil

import (
	"fmt"
	"path"
	"strings"
)

const LocalStateDirName = ".usbsync-local"

var reservedNames = map[string]struct{}{
	"con":  {},
	"prn":  {},
	"aux":  {},
	"nul":  {},
	"com1": {},
	"com2": {},
	"com3": {},
	"com4": {},
	"com5": {},
	"com6": {},
	"com7": {},
	"com8": {},
	"com9": {},
	"lpt1": {},
	"lpt2": {},
	"lpt3": {},
	"lpt4": {},
	"lpt5": {},
	"lpt6": {},
	"lpt7": {},
	"lpt8": {},
	"lpt9": {},
}

func NormalizeRelativePath(rel string) (string, string, error) {
	displayPath := strings.ReplaceAll(rel, `\`, `/`)
	displayPath = path.Clean(displayPath)
	if displayPath == "." || displayPath == "" {
		return "", "", fmt.Errorf("empty relative path")
	}
	if strings.HasPrefix(displayPath, "../") || displayPath == ".." || strings.HasPrefix(displayPath, "/") {
		return "", "", fmt.Errorf("path escapes work root: %s", rel)
	}

	parts := strings.Split(displayPath, "/")
	for _, part := range parts {
		if err := validatePathPart(part); err != nil {
			return "", "", err
		}
	}

	return strings.ToLower(displayPath), displayPath, nil
}

func IsLocalStateRelativePath(rel string) bool {
	if rel == "" {
		return false
	}

	displayPath := strings.ReplaceAll(rel, `\`, `/`)
	displayPath = path.Clean(displayPath)
	if displayPath == "." {
		return false
	}

	first := strings.Split(displayPath, "/")[0]
	return strings.EqualFold(first, LocalStateDirName)
}

func validatePathPart(part string) error {
	if part == "" || part == "." || part == ".." {
		return fmt.Errorf("invalid path part: %s", part)
	}
	if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
		return fmt.Errorf("path part cannot end with dot or space: %s", part)
	}
	if _, found := reservedNames[strings.ToLower(part)]; found {
		return fmt.Errorf("reserved path part: %s", part)
	}
	for _, ch := range part {
		switch ch {
		case '<', '>', ':', '"', '|', '?', '*':
			return fmt.Errorf("invalid path character in %s", part)
		}
	}

	return nil
}

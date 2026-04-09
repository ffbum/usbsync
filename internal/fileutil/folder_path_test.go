package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateFolderPathAcceptsAbsoluteWindowsPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := ValidateFolderPath(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFolderPathRejectsRelativePath(t *testing.T) {
	if err := ValidateFolderPath(`work\docs`); err == nil {
		t.Fatal("expected relative path error")
	}
}

func TestValidateFolderPathRejectsInvalidCharacters(t *testing.T) {
	if err := ValidateFolderPath(`D:\work\bad?name`); err == nil {
		t.Fatal("expected invalid character error")
	}
}

func TestValidateFolderPathRejectsMissingFolder(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if err := ValidateFolderPath(missing); err == nil {
		t.Fatal("expected missing folder error")
	}
}

func TestValidateCreatableFolderPathAcceptsMissingFolder(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if err := ValidateCreatableFolderPath(missing); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCreatableFolderPathAcceptsMissingNestedFolders(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "parent", "child", "backup")
	if err := ValidateCreatableFolderPath(missing); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCreatableFolderPathRejectsAncestorFile(t *testing.T) {
	root := t.TempDir()
	blockingFile := filepath.Join(root, "parent")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	missing := filepath.Join(blockingFile, "child")
	if err := ValidateCreatableFolderPath(missing); err == nil {
		t.Fatal("expected ancestor file error")
	}
}

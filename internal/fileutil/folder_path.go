package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func NormalizeFolderPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func ValidateFolderPath(path string) error {
	path, err := validateFolderPathSyntax(path)
	if err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("文件夹不存在")
		}
		return fmt.Errorf("无法读取文件夹")
	}
	if !info.IsDir() {
		return fmt.Errorf("这不是文件夹")
	}
	return nil
}

func ValidateCreatableFolderPath(path string) error {
	path, err := validateFolderPathSyntax(path)
	if err != nil {
		return err
	}

	current := path
	for {
		info, statErr := os.Stat(current)
		if statErr == nil {
			if !info.IsDir() {
				if current == path {
					return fmt.Errorf("这不是文件夹")
				}
				return fmt.Errorf("上级路径不是文件夹")
			}
			return nil
		}
		if !os.IsNotExist(statErr) {
			if current == path {
				return fmt.Errorf("无法读取文件夹")
			}
			return fmt.Errorf("无法读取上级文件夹")
		}

		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("驱动器不存在")
		}
		current = parent
	}
}

func validateFolderPathSyntax(path string) (string, error) {
	path = NormalizeFolderPath(path)
	if path == "" {
		return "", fmt.Errorf("请选择文件夹")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("文件夹路径必须是完整路径")
	}
	volume := filepath.VolumeName(path)
	if volume == "" {
		return "", fmt.Errorf("文件夹路径必须带盘符")
	}

	rest := strings.TrimPrefix(path, volume)
	if strings.ContainsAny(rest, "<>:\"|?*") {
		return "", fmt.Errorf("文件夹路径包含不支持的字符")
	}
	return path, nil
}

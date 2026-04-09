package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type MachineConfig struct {
	MachineID         string
	DisplayName       string
	LastWorkRoot      string
	BackupDir         string
	BoundDeviceID     string
	BoundVolumeID     string
	StateDir          string
	MachineConfigPath string
}

type portableConfigFile struct {
	Machines map[string]MachineConfig `json:"machines"`
}

func DefaultMachineConfig() MachineConfig {
	return MachineConfig{
		StateDir:          `C:\.usbsync`,
		MachineConfigPath: `C:\.usbsync\machine.json`,
		BackupDir:         `C:\.usbsync\backup`,
	}
}

func SaveMachineConfig(path string, cfg MachineConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

func SaveMachineConfigForMachine(path, machineName string, cfg MachineConfig) error {
	key := sanitizeMachineDir(machineName)
	if key == "default" && strings.TrimSpace(machineName) == "" {
		return SaveMachineConfig(path, cfg)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	file, err := loadPortableConfigFile(path)
	if err != nil {
		return err
	}
	if file.Machines == nil {
		file.Machines = map[string]MachineConfig{}
	}
	file.Machines[key] = cfg

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func LoadMachineConfig(path string) (MachineConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return MachineConfig{}, err
	}

	var cfg MachineConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return MachineConfig{}, err
	}

	return cfg, nil
}

func LoadMachineConfigForMachine(path, machineName string) (MachineConfig, error) {
	file, err := loadPortableConfigFile(path)
	if err != nil {
		return MachineConfig{}, err
	}
	if cfg, ok := lookupMachineConfig(file, machineName); ok {
		return withDerivedPaths(path, cfg), nil
	}
	return MachineConfig{}, os.ErrNotExist
}

func LoadMachineConfigOrDefault(path string) (MachineConfig, error) {
	cfg, err := LoadMachineConfig(path)
	if err == nil {
		return withDerivedPaths(path, cfg), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return MachineConfig{}, err
	}

	cfg = DefaultMachineConfig()
	cfg.StateDir = filepath.Dir(path)
	cfg.MachineConfigPath = path
	return cfg, nil
}

func withDerivedPaths(path string, cfg MachineConfig) MachineConfig {
	stateDir := filepath.Dir(path)
	if stateDir == "." || stateDir == "" {
		stateDir = cfg.StateDir
	}

	cfg.StateDir = stateDir
	cfg.MachineConfigPath = path
	if cfg.BackupDir == "" {
		cfg.BackupDir = filepath.Join(stateDir, "backup")
	}

	return cfg
}

func LoadMachineConfigOrDefaultForMachine(path, machineName string) (MachineConfig, error) {
	cfg, err := LoadMachineConfigForMachine(path, machineName)
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return MachineConfig{}, err
	}

	cfg = DefaultMachineConfig()
	cfg.StateDir = filepath.Dir(path)
	cfg.MachineConfigPath = path
	return cfg, nil
}

func LoadMachineConfigForKeys(path, machineKey string, legacyKeys ...string) (MachineConfig, error) {
	file, err := loadPortableConfigFile(path)
	if err != nil {
		return MachineConfig{}, err
	}

	if cfg, ok := lookupMachineConfig(file, machineKey); ok {
		return withDerivedPaths(path, cfg), nil
	}
	if cfg, ok := lookupFallbackMachineConfig(file, machineKey, legacyKeys...); ok {
		return withDerivedPaths(path, cfg), nil
	}
	return MachineConfig{}, os.ErrNotExist
}

func LoadMachineConfigOrDefaultForKeys(path, machineKey string, legacyKeys ...string) (MachineConfig, error) {
	cfg, err := LoadMachineConfigForKeys(path, machineKey, legacyKeys...)
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return MachineConfig{}, err
	}

	cfg = DefaultMachineConfig()
	cfg.StateDir = filepath.Dir(path)
	cfg.MachineConfigPath = path
	return cfg, nil
}

func PortableMachineConfigPath(exePath string) string {
	exeDir := filepath.Dir(exePath)
	if exeDir == "." || exeDir == "" {
		exeDir = filepath.Dir(DefaultMachineConfig().MachineConfigPath)
	}
	return filepath.Join(exeDir, "usbsync.json")
}

func sanitizeMachineDir(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return "default"
	}

	var builder strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}

	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "default"
	}
	return result
}

func loadPortableConfigFile(path string) (portableConfigFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return portableConfigFile{}, nil
		}
		return portableConfigFile{}, err
	}

	var file portableConfigFile
	if err := json.Unmarshal(data, &file); err == nil && file.Machines != nil {
		return file, nil
	}

	var cfg MachineConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return portableConfigFile{}, err
	}

	file.Machines = map[string]MachineConfig{
		"default": cfg,
	}
	return file, nil
}

func lookupMachineConfig(file portableConfigFile, machineName string) (MachineConfig, bool) {
	key := sanitizeMachineDir(machineName)
	cfg, ok := file.Machines[key]
	return cfg, ok
}

func lookupFallbackMachineConfig(file portableConfigFile, machineKey string, legacyKeys ...string) (MachineConfig, bool) {
	if len(file.Machines) != 1 {
		return MachineConfig{}, false
	}

	allowed := map[string]struct{}{
		"default": {},
	}
	currentKey := sanitizeMachineDir(machineKey)
	for _, key := range legacyKeys {
		sanitized := sanitizeMachineDir(key)
		if sanitized == "" || sanitized == currentKey {
			continue
		}
		allowed[sanitized] = struct{}{}
	}

	for key, cfg := range file.Machines {
		if _, ok := allowed[key]; ok {
			return cfg, true
		}
	}
	return MachineConfig{}, false
}

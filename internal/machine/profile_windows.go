//go:build windows

package machine

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

func currentHardwareID() (string, error) {
	systemKey, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\SystemInformation`, registry.QUERY_VALUE)
	if err == nil {
		defer systemKey.Close()

		if value, _, valueErr := systemKey.GetStringValue("ComputerHardwareId"); valueErr == nil && strings.TrimSpace(value) != "" {
			return value, nil
		}
		if values, _, valuesErr := systemKey.GetStringsValue("ComputerHardwareIds"); valuesErr == nil {
			for _, value := range values {
				if strings.TrimSpace(value) != "" {
					return value, nil
				}
			}
		}
	}

	cryptoKey, cryptoErr := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE)
	if cryptoErr == nil {
		defer cryptoKey.Close()

		if value, _, valueErr := cryptoKey.GetStringValue("MachineGuid"); valueErr == nil && strings.TrimSpace(value) != "" {
			return value, nil
		}
	}

	if err != nil {
		return "", err
	}
	return "", cryptoErr
}

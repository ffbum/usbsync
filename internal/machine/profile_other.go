//go:build !windows

package machine

import "fmt"

func currentHardwareID() (string, error) {
	return "", fmt.Errorf("hardware id is only available on Windows")
}

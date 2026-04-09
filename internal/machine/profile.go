package machine

import (
	"os"
	"strings"
)

type Profile struct {
	HardwareID string
	Hostname   string
}

func CurrentProfile() (Profile, error) {
	return CurrentProfileWithHostname(os.Hostname)
}

func CurrentProfileWithHostname(hostname func() (string, error)) (Profile, error) {
	if hostname == nil {
		hostname = os.Hostname
	}

	host, hostErr := hostname()
	host = strings.TrimSpace(host)

	hardwareID, hardwareErr := currentHardwareID()
	hardwareID = strings.TrimSpace(hardwareID)

	if hardwareID != "" {
		return Profile{
			HardwareID: hardwareID,
			Hostname:   host,
		}, nil
	}
	if host != "" {
		return Profile{
			HardwareID: host,
			Hostname:   host,
		}, nil
	}
	if hardwareErr != nil {
		return Profile{}, hardwareErr
	}
	return Profile{}, hostErr
}

func (p Profile) ConfigKey() string {
	if strings.TrimSpace(p.HardwareID) != "" {
		return strings.TrimSpace(p.HardwareID)
	}
	return strings.TrimSpace(p.Hostname)
}

func (p Profile) LegacyConfigKeys() []string {
	host := strings.TrimSpace(p.Hostname)
	if host == "" {
		return nil
	}
	if strings.EqualFold(host, p.ConfigKey()) {
		return nil
	}
	return []string{host}
}

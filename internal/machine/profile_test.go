package machine

import "testing"

func TestProfilePrefersHardwareIDAsConfigKey(t *testing.T) {
	profile := Profile{
		HardwareID: "{hardware-guid}",
		Hostname:   "MININT-123",
	}

	if profile.ConfigKey() != "{hardware-guid}" {
		t.Fatalf("unexpected config key: %s", profile.ConfigKey())
	}

	legacy := profile.LegacyConfigKeys()
	if len(legacy) != 1 || legacy[0] != "MININT-123" {
		t.Fatalf("unexpected legacy keys: %#v", legacy)
	}
}

func TestProfileFallsBackToHostnameWhenHardwareIDMissing(t *testing.T) {
	profile := Profile{
		Hostname: "Office-PC",
	}

	if profile.ConfigKey() != "Office-PC" {
		t.Fatalf("unexpected config key: %s", profile.ConfigKey())
	}
	if len(profile.LegacyConfigKeys()) != 0 {
		t.Fatalf("unexpected legacy keys: %#v", profile.LegacyConfigKeys())
	}
}

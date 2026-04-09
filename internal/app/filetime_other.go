//go:build !windows

package app

func setCreationTime(_ string, _ int64) error {
	return nil
}

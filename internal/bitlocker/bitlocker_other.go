//go:build !windows

package bitlocker

// GetBitLockerVolumes returns an empty list on non-Windows platforms.
func GetBitLockerVolumes() ([]VolumeInfo, error) {
	return []VolumeInfo{}, nil
}

// GetRecoveryKeys returns an empty list on non-Windows platforms.
func GetRecoveryKeys() ([]KeyProtector, error) {
	return []KeyProtector{}, nil
}

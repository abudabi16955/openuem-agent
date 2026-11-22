//go:build !windows

package selfservice

// ScanRegistryInstalled is a stub for non-Windows platforms.
func ScanRegistryInstalled() (map[string]string, error) {
	return make(map[string]string), nil
}

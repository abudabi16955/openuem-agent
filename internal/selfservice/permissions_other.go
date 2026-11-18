//go:build !windows

package selfservice

import "os"

// SetStrictSystemAdminPermissions sets POSIX 0755 for directory or 0644 for file.
func SetStrictSystemAdminPermissions(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return os.Chmod(path, 0755)
	}
	return os.Chmod(path, 0644)
}

// SetCacheExclusivePermissions sets POSIX 0700 for directory or 0600 for file.
func SetCacheExclusivePermissions(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return os.Chmod(path, 0700)
	}
	return os.Chmod(path, 0600)
}

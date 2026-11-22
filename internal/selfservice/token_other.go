//go:build !windows

package selfservice

// closeToken is a no-op on non-Windows platforms.
func closeToken(token uintptr) {}

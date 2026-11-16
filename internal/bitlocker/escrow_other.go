//go:build !windows

package bitlocker

// IsKeyEscrowed is a stub for non-Windows platforms.
func IsKeyEscrowed(keyID string) (bool, error) {
	return false, nil
}

// MarkKeyEscrowed is a stub for non-Windows platforms.
func MarkKeyEscrowed(keyID, driveLetter string) error {
	return nil
}

// ClearKeyEscrowed is a stub for non-Windows platforms.
func ClearKeyEscrowed(keyID string) error {
	return nil
}

//go:build windows

package bitlocker

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/windows/registry"
)

const RegistryBitLockerPath = `SOFTWARE\OpenUEM\BitLockerEscrowedProtectors`

// IsKeyEscrowed checks whether the specified key ID has already been recorded in HKLM\SOFTWARE\OpenUEM\BitLockerEscrowedProtectors.
func IsKeyEscrowed(keyID string) (bool, error) {
	if keyID == "" {
		return false, nil
	}

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, RegistryBitLockerPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer k.Close()

	_, _, err = k.GetStringValue(keyID)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// MarkKeyEscrowed records that the key ID has been successfully escrowed.
func MarkKeyEscrowed(keyID, driveLetter string) error {
	if keyID == "" {
		return nil
	}

	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, RegistryBitLockerPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open/create registry key %s: %w", RegistryBitLockerPath, err)
	}
	defer k.Close()

	val := fmt.Sprintf("%s|%s", driveLetter, time.Now().UTC().Format(time.RFC3339))
	return k.SetStringValue(keyID, val)
}

// ClearKeyEscrowed removes a key ID from the escrowed registry cache.
func ClearKeyEscrowed(keyID string) error {
	if keyID == "" {
		return nil
	}

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, RegistryBitLockerPath, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	defer k.Close()

	err = k.DeleteValue(keyID)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

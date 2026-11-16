//go:build !windows

package bitlocker

import (
	"sync"
)

var (
	mockRegistryMu    sync.RWMutex
	mockRegistryStore = make(map[string]string)
)

// IsKeyEscrowed checks whether the specified key ID is recorded in mock registry.
func IsKeyEscrowed(keyID string) (bool, error) {
	mockRegistryMu.RLock()
	defer mockRegistryMu.RUnlock()
	_, exists := mockRegistryStore[keyID]
	return exists, nil
}

// MarkKeyEscrowed records that the key ID has been successfully escrowed.
func MarkKeyEscrowed(keyID, driveLetter string) error {
	mockRegistryMu.Lock()
	defer mockRegistryMu.Unlock()
	mockRegistryStore[keyID] = driveLetter
	return nil
}

// ClearKeyEscrowed removes a key ID from the escrowed mock registry cache.
func ClearKeyEscrowed(keyID string) error {
	mockRegistryMu.Lock()
	defer mockRegistryMu.Unlock()
	delete(mockRegistryStore, keyID)
	return nil
}

// ResetMockRegistry clears all entries from the mock registry store (for testing).
func ResetMockRegistry() {
	mockRegistryMu.Lock()
	defer mockRegistryMu.Unlock()
	mockRegistryStore = make(map[string]string)
}

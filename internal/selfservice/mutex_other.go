//go:build !windows

package selfservice

import (
	"sync"
	"time"
)

var mockMSIMu sync.Mutex

// WaitForMSIExecuteMutex provides mock mutex synchronization for non-Windows platforms.
func WaitForMSIExecuteMutex(timeout time.Duration) (func(), error) {
	mockMSIMu.Lock()
	return func() {
		mockMSIMu.Unlock()
	}, nil
}

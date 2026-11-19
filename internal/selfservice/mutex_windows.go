//go:build windows

package selfservice

import (
	"fmt"
	"log"
	"time"

	"golang.org/x/sys/windows"
)

// WaitForMSIExecuteMutex waits for the Windows Installer mutex Global\_MSIExecute
// to be released if an installation is already in progress, avoiding error 1618.
func WaitForMSIExecuteMutex(timeout time.Duration) (func(), error) {
	mutexName, err := windows.UTF16PtrFromString("Global\\_MSIExecute")
	if err != nil {
		return func() {}, err
	}

	deadline := time.Now().Add(timeout)
	for {
		handle, err := windows.OpenMutex(windows.SYNCHRONIZE, false, mutexName)
		if err != nil {
			// Mutex does not exist; no MSI installer is currently running
			return func() {}, nil
		}

		log.Println("[INFO]: Global\\_MSIExecute mutex is active, waiting for installer to complete...")

		waitMs := uint32(5000)
		remaining := time.Until(deadline)
		if remaining <= 0 {
			_ = windows.CloseHandle(handle)
			return func() {}, fmt.Errorf("timed out waiting for Windows Installer (Global\\_MSIExecute)")
		}
		if remaining < 5*time.Second {
			waitMs = uint32(remaining.Milliseconds())
		}

		event, err := windows.WaitForSingleObject(handle, waitMs)
		_ = windows.CloseHandle(handle)

		if event == windows.WAIT_OBJECT_0 || event == windows.WAIT_ABANDONED {
			return func() {}, nil
		}
		if event == uint32(windows.WAIT_TIMEOUT) {
			if time.Now().After(deadline) {
				return func() {}, fmt.Errorf("timed out waiting for Windows Installer (Global\\_MSIExecute)")
			}
			continue
		}
		return func() {}, fmt.Errorf("error waiting for Windows Installer mutex: %v", err)
	}
}

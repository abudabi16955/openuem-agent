//go:build windows

package selfservice

import "golang.org/x/sys/windows"

// closeToken safely closes a Windows handle if non-zero.
func closeToken(token uintptr) {
	if token != 0 {
		_ = windows.CloseHandle(windows.Handle(token))
	}
}

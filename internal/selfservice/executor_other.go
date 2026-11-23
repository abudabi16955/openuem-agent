//go:build !windows

package selfservice

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WindowsExecutor is a stub executor for non-Windows platforms (e.g. Linux development/testing).
type WindowsExecutor struct {
	timeout time.Duration
}

// NewWindowsExecutor creates a new stub executor.
func NewWindowsExecutor(timeout time.Duration) *WindowsExecutor {
	return &WindowsExecutor{timeout: timeout}
}

// Execute handles mock execution for tests and non-Windows builds.
func (e *WindowsExecutor) Execute(pkg PackageItem, localPath string, userTokens ...uintptr) (string, error) {
	pkgType := strings.ToLower(strings.TrimSpace(pkg.PackageType))

	switch pkgType {
	case "msi", "exe", "winget":
		return fmt.Sprintf("mock executed %s [%s] successfully", pkg.Name, pkgType), nil

	case "zip":
		scratchDir := filepath.Join(GetSelfServiceDir(), "scratch", pkg.ID)
		if err := os.MkdirAll(scratchDir, 0700); err != nil {
			return "", fmt.Errorf("failed to create scratch directory: %w", err)
		}
		defer func() {
			_ = os.RemoveAll(scratchDir)
		}()

		if err := ExtractZip(localPath, scratchDir); err != nil {
			return "", fmt.Errorf("failed to extract zip: %w", err)
		}

		return fmt.Sprintf("mock executed zip package %s successfully", pkg.Name), nil

	default:
		return "", fmt.Errorf("unsupported package type: %s", pkg.PackageType)
	}
}

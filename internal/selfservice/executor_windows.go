//go:build windows

package selfservice

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/open-uem/openuem-agent/internal/commands/runtime"
	"golang.org/x/sys/windows"
)

var (
	modwtsapi32                  = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSQueryUserToken        = modwtsapi32.NewProc("WTSQueryUserToken")
	procWTSGetActiveConsoleSessionId = modwtsapi32.NewProc("WTSGetActiveConsoleSessionId")

	moduserenv                   = windows.NewLazySystemDLL("userenv.dll")
	procCreateEnvironmentBlock   = moduserenv.NewProc("CreateEnvironmentBlock")
	procDestroyEnvironmentBlock  = moduserenv.NewProc("DestroyEnvironmentBlock")
)

// WindowsExecutor implements package installation execution on Windows.
type WindowsExecutor struct {
	timeout time.Duration
}

// NewWindowsExecutor creates a new WindowsExecutor with default timeout.
func NewWindowsExecutor(timeout time.Duration) *WindowsExecutor {
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	return &WindowsExecutor{timeout: timeout}
}

// Execute executes a package installation based on its PackageType and ExecContext.
func (e *WindowsExecutor) Execute(pkg PackageItem, localPath string) (string, error) {
	pkgType := strings.ToLower(strings.TrimSpace(pkg.PackageType))
	execContext := strings.ToLower(strings.TrimSpace(pkg.ExecContext))
	if execContext == "" {
		execContext = "system"
	}

	switch pkgType {
	case "msi":
		args := BuildMSIArgs(localPath, pkg.SilentArgs)
		return e.runCommand("msiexec.exe", args, execContext)

	case "exe":
		args := BuildCustomArgs(pkg.SilentArgs)
		return e.runCommand(localPath, args, execContext)

	case "zip":
		scratchDir := filepath.Join(GetSelfServiceDir(), "scratch", pkg.ID)
		if err := os.MkdirAll(scratchDir, 0700); err != nil {
			return "", fmt.Errorf("failed to create scratch directory: %w", err)
		}
		defer func() {
			_ = os.RemoveAll(scratchDir)
		}()

		if err := ExtractZip(localPath, scratchDir); err != nil {
			return "", fmt.Errorf("failed to extract zip package: %w", err)
		}

		exePath, err := LocateExecutableInDir(scratchDir)
		if err != nil {
			return "", err
		}

		if strings.EqualFold(filepath.Ext(exePath), ".msi") {
			args := BuildMSIArgs(exePath, pkg.SilentArgs)
			return e.runCommand("msiexec.exe", args, execContext)
		}

		args := BuildCustomArgs(pkg.SilentArgs)
		return e.runCommand(exePath, args, execContext)

	case "winget":
		targetID := pkg.ID
		if pkg.DownloadURL != "" && !strings.HasPrefix(pkg.DownloadURL, "http") {
			// In case DownloadURL was used to hold winget package ID
			targetID = pkg.DownloadURL
		}
		args := BuildWingetArgs(targetID, pkg.SilentArgs)
		return e.runCommand("winget.exe", args, execContext)

	default:
		return "", fmt.Errorf("unsupported package type: %s", pkg.PackageType)
	}
}

func (e *WindowsExecutor) runCommand(cmdPath string, args []string, execContext string) (string, error) {
	if execContext == "user" {
		return e.runAsUser(cmdPath, args)
	}
	return e.runAsSystem(cmdPath, args)
}

func (e *WindowsExecutor) runAsSystem(cmdPath string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW,
	}

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	if err != nil {
		return output, fmt.Errorf("command execution failed (%w): %s", err, output)
	}
	return output, nil
}

func (e *WindowsExecutor) runAsUser(cmdPath string, args []string) (string, error) {
	// 1. Get active console session ID via WTSGetActiveConsoleSessionId
	sessionID, _, _ := procWTSGetActiveConsoleSessionId.Call()
	if sessionID == 0xFFFFFFFF {
		log.Println("[WARN]: no active console session found via WTSGetActiveConsoleSessionId, falling back to process token lookup")
		out, err := runtime.RunAsUserWithOutputAndTimeout(cmdPath, args, e.timeout)
		return string(out), err
	}

	// 2. Query user token for active session
	var userToken windows.Token
	r1, _, err := procWTSQueryUserToken.Call(sessionID, uintptr(unsafe.Pointer(&userToken)))
	if r1 == 0 {
		log.Printf("[WARN]: WTSQueryUserToken failed for session %d: %v, falling back to runtime user execution", sessionID, err)
		out, err := runtime.RunAsUserWithOutputAndTimeout(cmdPath, args, e.timeout)
		return string(out), err
	}
	defer userToken.Close()

	// 3. Duplicate token to Primary token
	var primaryToken windows.Token
	err = windows.DuplicateTokenEx(
		userToken,
		windows.MAXIMUM_ALLOWED,
		nil,
		windows.SecurityImpersonation,
		windows.TokenPrimary,
		&primaryToken,
	)
	if err != nil {
		log.Printf("[WARN]: DuplicateTokenEx failed: %v, falling back to original token", err)
		primaryToken = userToken
	} else {
		defer primaryToken.Close()
	}

	// 4. Create environment block for duplicated token
	var envBlock uintptr
	r1, _, _ = procCreateEnvironmentBlock.Call(uintptr(unsafe.Pointer(&envBlock)), uintptr(primaryToken), 0)
	if r1 != 0 && envBlock != 0 {
		defer procDestroyEnvironmentBlock.Call(envBlock)
	}

	// 5. Execute command using user token
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Token:         syscall.Token(primaryToken),
		CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_UNICODE_ENVIRONMENT,
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	if runErr != nil {
		return output, fmt.Errorf("user command execution failed (%w): %s", runErr, output)
	}
	return output, nil
}

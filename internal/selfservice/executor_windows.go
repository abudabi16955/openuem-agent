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

	"golang.org/x/sys/windows"
)

var (
	modwtsapi32           = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSQueryUserToken = modwtsapi32.NewProc("WTSQueryUserToken")

	moduserenv                  = windows.NewLazySystemDLL("userenv.dll")
	procCreateEnvironmentBlock  = moduserenv.NewProc("CreateEnvironmentBlock")
	procDestroyEnvironmentBlock = moduserenv.NewProc("DestroyEnvironmentBlock")
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

// parseEnvBlock parses a double-null-terminated block of UTF-16 strings (returned by CreateEnvironmentBlock)
// into a slice of strings in KEY=VALUE format.
func parseEnvBlock(block *uint16) []string {
	if block == nil {
		return nil
	}
	var env []string
	ptr := unsafe.Pointer(block)
	for {
		u16 := *(*uint16)(ptr)
		if u16 == 0 {
			break
		}
		start := ptr
		length := 0
		for {
			val := *(*uint16)(ptr)
			if val == 0 {
				break
			}
			length++
			ptr = unsafe.Pointer(uintptr(ptr) + 2)
		}
		if length > 0 {
			slice := unsafe.Slice((*uint16)(start), length)
			str := windows.UTF16ToString(slice)
			if str != "" {
				env = append(env, str)
			}
		}
		ptr = unsafe.Pointer(uintptr(ptr) + 2)
	}
	return env
}

// getUserTokenFromProcess finds a process by name (e.g. explorer.exe) and retrieves its token.
// If targetSessionID != 0xFFFFFFFF, it prefers a process running in that session.
func getUserTokenFromProcess(name string, targetSessionID uint32) (windows.Token, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, fmt.Errorf("CreateToolhelp32Snapshot failed: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	err = windows.Process32First(snapshot, &entry)
	if err != nil {
		return 0, fmt.Errorf("Process32First failed: %w", err)
	}

	var fallbackPID uint32
	var matchedPID uint32

	for {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), name) {
			var sessID uint32
			if errSess := windows.ProcessIdToSessionId(entry.ProcessID, &sessID); errSess == nil {
				if targetSessionID != 0xFFFFFFFF && sessID == targetSessionID {
					matchedPID = entry.ProcessID
					break
				}
				if sessID != 0 && fallbackPID == 0 {
					fallbackPID = entry.ProcessID
				}
			} else if fallbackPID == 0 {
				fallbackPID = entry.ProcessID
			}
		}
		err = windows.Process32Next(snapshot, &entry)
		if err != nil {
			break
		}
	}

	selectedPID := matchedPID
	if selectedPID == 0 {
		selectedPID = fallbackPID
	}
	if selectedPID == 0 {
		return 0, fmt.Errorf("process %s not found in active sessions", name)
	}

	hProc, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION, false, selectedPID)
	if err != nil {
		return 0, fmt.Errorf("OpenProcess failed for pid %d: %w", selectedPID, err)
	}
	defer windows.CloseHandle(hProc)

	var token windows.Token
	err = windows.OpenProcessToken(hProc, windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY|windows.TOKEN_ASSIGN_PRIMARY, &token)
	if err != nil {
		err = windows.OpenProcessToken(hProc, windows.TOKEN_ALL_ACCESS, &token)
		if err != nil {
			return 0, fmt.Errorf("OpenProcessToken failed for pid %d: %w", selectedPID, err)
		}
	}
	return token, nil
}

// acquireUserToken attempts to acquire a user token first via active console session (WTSQueryUserToken),
// and falls back to inspecting the explorer.exe process token if WTS is unavailable or fails.
func acquireUserToken() (windows.Token, error) {
	// 1. Get active console session ID via windows.WTSGetActiveConsoleSessionId
	sessionID := windows.WTSGetActiveConsoleSessionId()
	if sessionID != 0xFFFFFFFF {
		var userToken windows.Token
		r1, _, err := procWTSQueryUserToken.Call(uintptr(sessionID), uintptr(unsafe.Pointer(&userToken)))
		if r1 != 0 && userToken != 0 {
			return userToken, nil
		}
		log.Printf("[WARN]: WTSQueryUserToken failed for session %d: %v, falling back to process token lookup", sessionID, err)
	} else {
		log.Println("[WARN]: no active console session found via WTSGetActiveConsoleSessionId, falling back to process token lookup")
	}

	// 2. Fallback: query user token from explorer.exe process
	token, err := getUserTokenFromProcess("explorer.exe", sessionID)
	if err != nil {
		return 0, fmt.Errorf("failed to acquire user token (WTS session unavailable and process fallback failed: %w)", err)
	}
	return token, nil
}

func (e *WindowsExecutor) runAsUser(cmdPath string, args []string) (string, error) {
	// 1. Acquire user token (via WTS or fallback to explorer.exe)
	userToken, err := acquireUserToken()
	if err != nil {
		return "", fmt.Errorf("failed to acquire user token: %w", err)
	}
	defer userToken.Close()

	// 2. Duplicate token to Primary token
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

	// 3. Create environment block for duplicated token
	var envBlock *uint16
	r1, _, errEnv := procCreateEnvironmentBlock.Call(uintptr(unsafe.Pointer(&envBlock)), uintptr(primaryToken), 0)
	var userEnv []string
	if r1 != 0 && envBlock != nil {
		defer procDestroyEnvironmentBlock.Call(uintptr(unsafe.Pointer(envBlock)))
		userEnv = parseEnvBlock(envBlock)
	} else {
		log.Printf("[WARN]: CreateEnvironmentBlock failed: %v", errEnv)
	}

	// 4. Execute command using user token and environment
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Token:         syscall.Token(primaryToken),
		CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_UNICODE_ENVIRONMENT,
	}
	if len(userEnv) > 0 {
		cmd.Env = userEnv
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

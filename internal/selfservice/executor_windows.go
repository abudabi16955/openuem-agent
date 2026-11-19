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
	modadvapi32                 = windows.NewLazySystemDLL("advapi32.dll")
	procCreateProcessWithTokenW = modadvapi32.NewProc("CreateProcessWithTokenW")

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
	if r1 != 0 && envBlock != nil {
		defer procDestroyEnvironmentBlock.Call(uintptr(unsafe.Pointer(envBlock)))
	} else {
		log.Printf("[WARN]: CreateEnvironmentBlock failed: %v", errEnv)
		envBlock = nil
	}

	// 4. Prepare command line
	cmdLineArgs := append([]string{cmdPath}, args...)
	cmdLineStr := windows.ComposeCommandLine(cmdLineArgs)
	cmdLine16, err := windows.UTF16PtrFromString(cmdLineStr)
	if err != nil {
		return "", fmt.Errorf("failed to encode command line: %w", err)
	}

	// 5. Create pipe for stdout/stderr with permissive DACL
	sd, err := windows.SecurityDescriptorFromString("D:(A;;GA;;;WD)")
	if err != nil {
		return "", fmt.Errorf("failed to create security descriptor: %w", err)
	}
	sa := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		InheritHandle:      1,
		SecurityDescriptor: sd,
	}

	var readPipe, writePipe windows.Handle
	if err := windows.CreatePipe(&readPipe, &writePipe, &sa, 0); err != nil {
		return "", fmt.Errorf("failed to create pipe: %w", err)
	}
	defer windows.CloseHandle(readPipe)
	_ = windows.SetHandleInformation(readPipe, windows.HANDLE_FLAG_INHERIT, 0)

	// 6. Setup STARTUPINFO and PROCESS_INFORMATION
	si := windows.StartupInfo{
		Cb:         uint32(unsafe.Sizeof(windows.StartupInfo{})),
		Flags:      windows.STARTF_USESTDHANDLES | windows.STARTF_USESHOWWINDOW,
		ShowWindow: windows.SW_HIDE,
		StdOutput:  writePipe,
		StdErr:     writePipe,
	}
	var pi windows.ProcessInformation

	creationFlags := uint32(windows.CREATE_NO_WINDOW)
	var envPtr uintptr
	if envBlock != nil {
		creationFlags |= windows.CREATE_UNICODE_ENVIRONMENT
		envPtr = uintptr(unsafe.Pointer(envBlock))
	}

	// 7. Launch process using CreateProcessWithTokenW
	ret, _, procErr := procCreateProcessWithTokenW.Call(
		uintptr(primaryToken),
		0,
		0,
		uintptr(unsafe.Pointer(cmdLine16)),
		uintptr(creationFlags),
		envPtr,
		0,
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	// Immediately close the write end of the pipe in the parent process
	_ = windows.CloseHandle(writePipe)

	if ret == 0 {
		return "", fmt.Errorf("CreateProcessWithTokenW failed: %v", procErr)
	}
	defer windows.CloseHandle(pi.Process)
	defer windows.CloseHandle(pi.Thread)

	// 8. Concurrently read output from pipe until EOF
	var outBuf bytes.Buffer
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 4096)
		for {
			var bytesRead uint32
			err := windows.ReadFile(readPipe, buf, &bytesRead, nil)
			if bytesRead > 0 {
				outBuf.Write(buf[:bytesRead])
			}
			if err != nil {
				break
			}
		}
	}()

	// 9. Wait for process completion with timeout
	timeoutMs := uint32(windows.INFINITE)
	if e.timeout > 0 {
		timeoutMs = uint32(e.timeout / time.Millisecond)
	}

	event, waitErr := windows.WaitForSingleObject(pi.Process, timeoutMs)
	if waitErr != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		<-readDone
		return outBuf.String(), fmt.Errorf("WaitForSingleObject failed: %w", waitErr)
	}
	if event == uint32(windows.WAIT_TIMEOUT) {
		_ = windows.TerminateProcess(pi.Process, 1)
		<-readDone
		return outBuf.String(), fmt.Errorf("user command execution timed out after %v", e.timeout)
	}

	<-readDone

	var exitCode uint32
	if err := windows.GetExitCodeProcess(pi.Process, &exitCode); err != nil {
		return outBuf.String(), fmt.Errorf("failed to get exit code: %w", err)
	}

	output := outBuf.String()
	if exitCode != 0 {
		return output, fmt.Errorf("user command execution failed (exit status %d): %s", exitCode, output)
	}

	return output, nil
}

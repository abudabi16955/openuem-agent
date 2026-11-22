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
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modadvapi32                 = windows.NewLazySystemDLL("advapi32.dll")
	procCreateProcessWithTokenW = modadvapi32.NewProc("CreateProcessWithTokenW")

	moduserenv                  = windows.NewLazySystemDLL("userenv.dll")
	procCreateEnvironmentBlock  = moduserenv.NewProc("CreateEnvironmentBlock")
	procDestroyEnvironmentBlock = moduserenv.NewProc("DestroyEnvironmentBlock")

	activeTokenMu   sync.RWMutex
	activeUserToken windows.Token
)

// SetActiveUserToken stores a duplicate of the primary user token obtained directly from the IPC connection (S-13).
func SetActiveUserToken(token windows.Token) {
	activeTokenMu.Lock()
	defer activeTokenMu.Unlock()
	if activeUserToken != 0 {
		_ = activeUserToken.Close()
	}
	activeUserToken = token
}

// GetActiveUserToken returns a duplicated handle to the active user token obtained via Named Pipe IPC (S-13).
func GetActiveUserToken() (windows.Token, error) {
	activeTokenMu.RLock()
	defer activeTokenMu.RUnlock()

	if activeUserToken == 0 {
		return 0, fmt.Errorf("no active user token available from IPC client (runAsUser requires an active Named Pipe session)")
	}

	var dup windows.Token
	err := windows.DuplicateTokenEx(
		activeUserToken,
		windows.MAXIMUM_ALLOWED,
		nil,
		windows.SecurityImpersonation,
		windows.TokenPrimary,
		&dup,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to duplicate active user token: %w", err)
	}
	return dup, nil
}

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

// prepareUserPermissions grants Read & Execute permissions to the user's SID (S-14).
func (e *WindowsExecutor) prepareUserPermissions(path string) {
	token, err := GetActiveUserToken()
	if err != nil {
		return
	}
	defer token.Close()

	user, err := token.GetTokenUser()
	if err != nil || user.User.Sid == nil {
		return
	}
	userSID := user.User.Sid.String()

	dir := filepath.Dir(path)
	_ = GrantUserReadAndExecute(dir, userSID)
	_ = GrantUserReadAndExecute(path, userSID)
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
		if execContext == "user" && localPath != "" {
			e.prepareUserPermissions(localPath)
		}
		args := BuildMSIArgs(localPath, pkg.SilentArgs)
		return e.runCommand("msiexec.exe", args, execContext)

	case "exe":
		if execContext == "user" && localPath != "" {
			e.prepareUserPermissions(localPath)
		}
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

		if execContext == "user" {
			e.prepareUserPermissions(scratchDir)
			e.prepareUserPermissions(exePath)
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

func (e *WindowsExecutor) runAsUser(cmdPath string, args []string) (string, error) {
	// 1. Acquire user token directly from IPC Named Pipe client connection (S-13)
	primaryToken, err := GetActiveUserToken()
	if err != nil {
		return "", fmt.Errorf("failed to acquire user token: %w", err)
	}
	defer primaryToken.Close()

	// Grant Read & Execute permissions if cmdPath is a local file (S-14)
	if fi, err := os.Stat(cmdPath); err == nil && !fi.IsDir() {
		e.prepareUserPermissions(cmdPath)
	}

	// 2. Create environment block for user token
	var envBlock *uint16
	r1, _, errEnv := procCreateEnvironmentBlock.Call(uintptr(unsafe.Pointer(&envBlock)), uintptr(primaryToken), 0)
	if r1 != 0 && envBlock != nil {
		defer procDestroyEnvironmentBlock.Call(uintptr(unsafe.Pointer(envBlock)))
	} else {
		log.Printf("[WARN]: CreateEnvironmentBlock failed: %v", errEnv)
		envBlock = nil
	}

	// 3. Prepare command line
	cmdLineArgs := append([]string{cmdPath}, args...)
	cmdLineStr := windows.ComposeCommandLine(cmdLineArgs)
	cmdLine16, err := windows.UTF16PtrFromString(cmdLineStr)
	if err != nil {
		return "", fmt.Errorf("failed to encode command line: %w", err)
	}

	// 4. Create pipe for stdout/stderr with permissive DACL
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

	// 5. Setup STARTUPINFO and PROCESS_INFORMATION without modifying WinSta0 DACL (S-11, S-12)
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

	// 6. Launch process using CreateProcessWithTokenW with LOGON_WITH_PROFILE (value 1) (S-13)
	const LOGON_WITH_PROFILE = 1

	ret, _, procErr := procCreateProcessWithTokenW.Call(
		uintptr(primaryToken),
		uintptr(LOGON_WITH_PROFILE),
		0,
		uintptr(unsafe.Pointer(cmdLine16)),
		uintptr(creationFlags),
		envPtr,
		0,
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	_ = windows.CloseHandle(writePipe)

	if ret == 0 {
		return "", fmt.Errorf("CreateProcessWithTokenW failed: %v", procErr)
	}
	defer windows.CloseHandle(pi.Process)
	defer windows.CloseHandle(pi.Thread)

	// 7. Concurrently read output from pipe until EOF
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

	// 8. Wait for process completion with timeout
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

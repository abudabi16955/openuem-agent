//go:build windows

package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

var (
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	procCreateMutexW        = kernel32.NewProc("CreateMutexW")
	procCloseHandle         = kernel32.NewProc("CloseHandle")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procSendMessageW        = user32.NewProc("SendMessageW")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procSetWindowLongPtrW   = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW     = user32.NewProc("CallWindowProcW")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procLoadImageW          = user32.NewProc("LoadImageW")
	procLoadIconW                  = user32.NewProc("LoadIconW")
	procMessageBoxW                = user32.NewProc("MessageBoxW")
	procShellNotifyIconW           = shell32.NewProc("Shell_NotifyIconW")
	procSetWindowsHookExW          = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx        = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx             = user32.NewProc("CallNextHookEx")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procGetWindowLongPtrW          = user32.NewProc("GetWindowLongPtrW")
	procGetCurrentThreadId         = kernel32.NewProc("GetCurrentThreadId")
)

const (
	SW_HIDE    = 0
	SW_NORMAL  = 1
	SW_SHOW    = 5
	SW_RESTORE = 9

	MB_OK        = 0x00000000
	MB_ICONERROR = 0x00000010

	WM_DESTROY         = 0x0002
	WM_CLOSE           = 0x0010
	WM_QUERYENDSESSION = 0x0011
	WM_ENDSESSION      = 0x0016
	WM_SETICON         = 0x0080
	WM_COMMAND         = 0x0111
	WM_USER            = 0x0400

	WM_LBUTTONUP     = 0x0202
	WM_RBUTTONUP     = 0x0205
	WM_LBUTTONDBLCLK = 0x0203
	WM_CONTEXTMENU   = 0x007B

	WM_TRAY_CALLBACK = WM_USER + 100

	NIM_ADD    = 0x00000000
	NIM_MODIFY = 0x00000001
	NIM_DELETE = 0x00000002

	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004

	MF_STRING    = 0x00000000
	MF_SEPARATOR = 0x00000800

	TPM_RIGHTBUTTON = 0x0002
	TPM_RETURNCMD   = 0x0100

	GWLP_WNDPROC = -4
	GWL_EXSTYLE = ^uintptr(19)

	WH_CBT         = 5
	HCBT_CREATEWND = 3
	WS_EX_LAYERED  = 0x00080000
	LWA_ALPHA      = 0x00000002

	ID_OPEN = 1001
	ID_EXIT = 1002

	IMAGE_ICON      = 1
	LR_LOADFROMFILE = 0x00000010
	LR_DEFAULTSIZE  = 0x00000040
	IDI_APPLICATION = 32512

	ICON_SMALL = 0
	ICON_BIG   = 1

	singleInstanceMutexName = `Local\OpenUEM_SelfService_Tray_SingleInstance`
	windowTitle             = "OpenUEM Self-Service Portal"
)

type POINT struct {
	X int32
	Y int32
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

var (
	globalNID       NOTIFYICONDATAW
	globalTrayAdded bool
	globalWV        webview2.WebView
	globalHWnd      uintptr
	origWndProc     uintptr
	cbtHookHandle   uintptr
)

func cbtHookProc(nCode int32, wParam uintptr, lParam uintptr) uintptr {
	if nCode == HCBT_CREATEWND {
		hwnd := wParam
		curStyle, _, _ := procGetWindowLongPtrW.Call(hwnd, GWL_EXSTYLE)
		procSetWindowLongPtrW.Call(hwnd, GWL_EXSTYLE, curStyle|WS_EX_LAYERED)
		procSetLayeredWindowAttributes.Call(hwnd, 0, 0, LWA_ALPHA)
	}
	ret, _, _ := procCallNextHookEx.Call(cbtHookHandle, uintptr(nCode), wParam, lParam)
	return ret
}

func setWindowLongPtr(hwnd uintptr, nIndex int, newProc uintptr) uintptr {
	if procSetWindowLongPtrW.Find() == nil {
		ret, _, _ := procSetWindowLongPtrW.Call(hwnd, uintptr(nIndex), newProc)
		return ret
	}
	procSetWindowLongW := user32.NewProc("SetWindowLongW")
	ret, _, _ := procSetWindowLongW.Call(hwnd, uintptr(nIndex), newProc)
	return ret
}

func getOrExtractIcon() uintptr {
	icoData, err := assetsFS.ReadFile("assets/openuem.ico")
	if err == nil && len(icoData) > 0 {
		tempDir := os.TempDir()
		iconPath := filepath.Join(tempDir, "openuem.ico")
		_ = os.WriteFile(iconPath, icoData, 0644)
		iconPathUTF16, err := windows.UTF16PtrFromString(iconPath)
		if err == nil {
			hIcon, _, _ := procLoadImageW.Call(
				0,
				uintptr(unsafe.Pointer(iconPathUTF16)),
				IMAGE_ICON,
				0,
				0,
				LR_LOADFROMFILE|LR_DEFAULTSIZE,
			)
			if hIcon != 0 {
				return hIcon
			}
		}
	}
	hIcon, _, _ := procLoadIconW.Call(0, IDI_APPLICATION)
	return hIcon
}

func addTrayIcon(hwnd uintptr, hIcon uintptr) error {
	globalNID = NOTIFYICONDATAW{
		HWnd:             hwnd,
		UID:              1,
		UFlags:           NIF_MESSAGE | NIF_ICON | NIF_TIP,
		UCallbackMessage: WM_TRAY_CALLBACK,
		HIcon:            hIcon,
	}
	globalNID.CbSize = uint32(unsafe.Sizeof(globalNID))
	tip := windows.StringToUTF16(windowTitle)
	copy(globalNID.SzTip[:], tip)

	ret, _, err := procShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&globalNID)))
	if ret == 0 {
		return fmt.Errorf("Shell_NotifyIconW(NIM_ADD) failed: %v", err)
	}
	globalTrayAdded = true
	return nil
}

func removeTrayIcon() {
	if globalTrayAdded {
		_, _, _ = procShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&globalNID)))
		globalTrayAdded = false
	}
}

func showContextMenu(hwnd uintptr) {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	openTitle, _ := windows.UTF16PtrFromString("Open Self-Service Portal")
	exitTitle, _ := windows.UTF16PtrFromString("Exit")

	procAppendMenuW.Call(hMenu, MF_STRING, ID_OPEN, uintptr(unsafe.Pointer(openTitle)))
	procAppendMenuW.Call(hMenu, MF_SEPARATOR, 0, 0)
	procAppendMenuW.Call(hMenu, MF_STRING, ID_EXIT, uintptr(unsafe.Pointer(exitTitle)))

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(hwnd)

	cmd, _, _ := procTrackPopupMenu.Call(hMenu, TPM_RIGHTBUTTON|TPM_RETURNCMD, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	procPostMessageW.Call(hwnd, 0, 0, 0)

	if cmd == ID_OPEN {
		procShowWindow.Call(hwnd, SW_RESTORE)
		procSetForegroundWindow.Call(hwnd)
	} else if cmd == ID_EXIT {
		removeTrayIcon()
		if globalWV != nil {
			globalWV.Terminate()
		}
	}
}

func customWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_CLOSE:
		// Intercept WM_CLOSE: hide window instead of closing/destroying process
		procShowWindow.Call(hwnd, SW_HIDE)
		return 0

	case WM_TRAY_CALLBACK:
		switch lParam {
		case WM_LBUTTONUP, WM_LBUTTONDBLCLK:
			procShowWindow.Call(hwnd, SW_RESTORE)
			procSetForegroundWindow.Call(hwnd)
		case WM_RBUTTONUP, WM_CONTEXTMENU:
			showContextMenu(hwnd)
		}
		return 0

	case WM_COMMAND:
		cmdID := wParam & 0xFFFF
		if cmdID == ID_OPEN {
			procShowWindow.Call(hwnd, SW_RESTORE)
			procSetForegroundWindow.Call(hwnd)
			return 0
		} else if cmdID == ID_EXIT {
			removeTrayIcon()
			if globalWV != nil {
				globalWV.Terminate()
			}
			return 0
		}

	case WM_QUERYENDSESSION:
		return 1

	case WM_ENDSESSION:
		if wParam != 0 {
			removeTrayIcon()
			if globalWV != nil {
				globalWV.Terminate()
			}
		}
		return 0

	case WM_DESTROY:
		removeTrayIcon()
	}

	r, _, _ := procCallWindowProcW.Call(origWndProc, hwnd, msg, wParam, lParam)
	return r
}

func main() {
	portFlag := flag.Int("port", 0, "HTTP server port (0 for random)")
	openBrowser := flag.Bool("open", true, "Open portal UI in browser automatically on launch")
	flag.Parse()

	// 1. Single Instance Named Mutex
	mutexName, err := windows.UTF16PtrFromString(singleInstanceMutexName)
	if err != nil {
		log.Fatalf("[FATAL]: invalid mutex name: %v", err)
	}
	hMutex, _, err := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(mutexName)))
	if hMutex == 0 {
		log.Fatalf("[FATAL]: failed to create single instance mutex: %v", err)
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || err == windows.ERROR_ALREADY_EXISTS {
		titlePtr, _ := windows.UTF16PtrFromString(windowTitle)
		targetHwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
		if targetHwnd != 0 {
			procShowWindow.Call(targetHwnd, SW_RESTORE)
			procSetForegroundWindow.Call(targetHwnd)
		}
		procCloseHandle.Call(hMutex)
		return
	}
	defer procCloseHandle.Call(hMutex)

	// 2. Start tray HTTP bridge server
	server, err := StartTrayServer(*portFlag)
	if err != nil {
		log.Fatalf("[FATAL]: failed to start tray portal server: %v", err)
	}
	defer server.Close()

	portalURL := server.GetURL()
	log.Printf("[INFO]: OpenUEM Self-Service Portal started at %s", portalURL)

	// 3. Initialize WebView2 window
	// If starting hidden (-open=false), hook window creation to set 0% opacity immediately,
	// eliminating any white window flash on autostart before SW_HIDE takes effect.
	var hHook uintptr
	if !*openBrowser {
		threadID, _, _ := procGetCurrentThreadId.Call()
		hookCb := windows.NewCallback(cbtHookProc)
		hHook, _, _ = procSetWindowsHookExW.Call(WH_CBT, hookCb, 0, threadID)
		cbtHookHandle = hHook
	}

	wv := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug: false,
		WindowOptions: webview2.WindowOptions{
			Title:  windowTitle,
			Width:  1024,
			Height: 720,
			Center: true,
		},
	})

	if hHook != 0 {
		procUnhookWindowsHookEx.Call(hHook)
		cbtHookHandle = 0
	}

	if wv == nil {
		msgPtr, _ := windows.UTF16PtrFromString("Microsoft Edge WebView2 Runtime is required to run the OpenUEM Self-Service Portal. Please install WebView2 Runtime and try again.")
		titlePtr, _ := windows.UTF16PtrFromString(windowTitle)
		procMessageBoxW.Call(
			0,
			uintptr(unsafe.Pointer(msgPtr)),
			uintptr(unsafe.Pointer(titlePtr)),
			uintptr(MB_OK|MB_ICONERROR),
		)
		log.Fatalf("[FATAL]: Microsoft Edge WebView2 Runtime is required to run the OpenUEM Self-Service Portal. Please install WebView2 Runtime and try again.")
	}

	hwnd := uintptr(wv.Window())
	globalHWnd = hwnd
	globalWV = wv

	if !*openBrowser {
		procShowWindow.Call(hwnd, SW_HIDE)
		procSetLayeredWindowAttributes.Call(hwnd, 0, 255, LWA_ALPHA)
		curStyle, _, _ := procGetWindowLongPtrW.Call(hwnd, GWL_EXSTYLE)
		procSetWindowLongPtrW.Call(hwnd, GWL_EXSTYLE, curStyle&^WS_EX_LAYERED)
	}

	// 4. Set window and tray icon
	hIcon := getOrExtractIcon()
	if hIcon != 0 {
		procSendMessageW.Call(hwnd, WM_SETICON, ICON_SMALL, hIcon)
		procSendMessageW.Call(hwnd, WM_SETICON, ICON_BIG, hIcon)
	}

	// 5. Subclass window procedure to handle WM_CLOSE, tray callbacks and context menu
	cb := windows.NewCallback(customWndProc)
	origWndProc = setWindowLongPtr(hwnd, GWLP_WNDPROC, cb)

	// 6. Register tray icon in Windows system tray
	if err := addTrayIcon(hwnd, hIcon); err != nil {
		log.Printf("[WARN]: failed to add tray icon: %v", err)
	}
	defer removeTrayIcon()

	// 7. Navigate to portal URL
	wv.Navigate(portalURL)

	// 8. Control initial visibility based on -open flag
	if !*openBrowser {
		procShowWindow.Call(hwnd, SW_HIDE)
	} else {
		procShowWindow.Call(hwnd, SW_RESTORE)
		procSetForegroundWindow.Call(hwnd)
	}

	// 9. Handle termination signals cleanly
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		removeTrayIcon()
		if wv != nil {
			wv.Terminate()
		}
	}()

	// 10. Run Win32 / WebView2 message loop
	wv.Run()
	wv.Destroy()

	log.Println("[INFO]: OpenUEM Self-Service Portal tray application exiting")
}

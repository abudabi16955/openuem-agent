//go:build windows

package selfservice

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// IPCServer manages the Windows Named Pipe server instance.
type IPCServer struct {
	pipeName   string
	dispatcher *IPCDispatcher
	quitChan   chan struct{}
	wg         sync.WaitGroup
	running    bool
	mu         sync.Mutex
}

// NewIPCServer initializes the Named Pipe server for self-service requests.
func NewIPCServer(pipeName string, dispatcher *IPCDispatcher) *IPCServer {
	if pipeName == "" {
		pipeName = DefaultNamedPipeName
	}
	return &IPCServer{
		pipeName:   pipeName,
		dispatcher: dispatcher,
		quitChan:   make(chan struct{}),
	}
}

// Start creates the named pipe with DACL granting Authenticated Users Read/Write
// and Administrators/SYSTEM Full Control, and starts accepting client connections.
func (s *IPCServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}
	s.running = true

	s.wg.Add(1)
	go s.listenLoop()

	log.Printf("[INFO]: Self-service Named Pipe server listening on %s", s.pipeName)
	return nil
}

// Stop terminates the Named Pipe server.
func (s *IPCServer) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.quitChan)
	s.mu.Unlock()

	// Unblock ConnectNamedPipe by sending a dummy connection
	go func() {
		pipeW, err := windows.UTF16PtrFromString(s.pipeName)
		if err == nil {
			h, err := windows.CreateFile(
				pipeW,
				windows.GENERIC_READ|windows.GENERIC_WRITE,
				0,
				nil,
				windows.OPEN_EXISTING,
				0,
				0,
			)
			if err == nil {
				_ = windows.CloseHandle(h)
			}
		}
	}()

	s.wg.Wait()
	log.Println("[INFO]: Self-service Named Pipe server stopped")
}

func (s *IPCServer) listenLoop() {
	defer s.wg.Done()

	pipeW, err := windows.UTF16PtrFromString(s.pipeName)
	if err != nil {
		log.Printf("[ERROR]: invalid pipe name %s: %v", s.pipeName, err)
		return
	}

	// DACL: Authenticated Users (AU: GRGW), Administrators (BA: GA), SYSTEM (SY: GA)
	sd, err := windows.SecurityDescriptorFromString("D:(A;;GRGW;;;AU)(A;;GA;;;BA)(A;;GA;;;SY)")
	if err != nil {
		log.Printf("[ERROR]: failed to create security descriptor for named pipe: %v", err)
		return
	}

	sa := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
		InheritHandle:      0,
	}

	for {
		select {
		case <-s.quitChan:
			return
		default:
		}

		// Create Named Pipe with unlimited instances
		handle, err := windows.CreateNamedPipe(
			pipeW,
			windows.PIPE_ACCESS_DUPLEX,
			windows.PIPE_TYPE_MESSAGE|windows.PIPE_READMODE_MESSAGE|windows.PIPE_WAIT,
			windows.PIPE_UNLIMITED_INSTANCES,
			65536,
			65536,
			0,
			&sa,
		)
		if err != nil {
			select {
			case <-s.quitChan:
				return
			default:
				log.Printf("[ERROR]: CreateNamedPipe failed: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}
		}

		// Wait for client connection
		err = windows.ConnectNamedPipe(handle, nil)
		if err != nil && err != windows.ERROR_PIPE_CONNECTED {
			_ = windows.CloseHandle(handle)
			select {
			case <-s.quitChan:
				return
			default:
				continue
			}
		}

		select {
		case <-s.quitChan:
			_ = windows.CloseHandle(handle)
			return
		default:
			go s.handleConnection(handle)
		}
	}
}

func (s *IPCServer) handleConnection(handle windows.Handle) {
	defer windows.CloseHandle(handle)

	buf := make([]byte, 65536)
	var bytesRead uint32

	err := windows.ReadFile(handle, buf, &bytesRead, nil)
	if err != nil || bytesRead == 0 {
		return
	}

	responseBytes := s.dispatcher.HandleMessage(buf[:bytesRead])

	var bytesWritten uint32
	_ = windows.WriteFile(handle, responseBytes, &bytesWritten, nil)
	_ = windows.FlushFileBuffers(handle)
	_ = windows.DisconnectNamedPipe(handle)
}

// CallIPC connects to the named pipe, sends an IPCRequest, and decodes the IPCResponse.
func CallIPC(pipeName string, req IPCRequest) (*IPCResponse, error) {
	if pipeName == "" {
		pipeName = DefaultNamedPipeName
	}

	pipeW, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		return nil, fmt.Errorf("invalid pipe name: %w", err)
	}

	// Try connecting with retries if pipe busy
	deadline := time.Now().Add(5 * time.Second)
	var handle windows.Handle

	for {
		handle, err = windows.CreateFile(
			pipeW,
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			0,
			nil,
			windows.OPEN_EXISTING,
			0,
			0,
		)
		if err == nil {
			break
		}

		if err == windows.ERROR_PIPE_BUSY {
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("pipe is busy, connection timed out")
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}

		return nil, fmt.Errorf("failed to open named pipe %s: %w", pipeName, err)
	}
	defer windows.CloseHandle(handle)

	// Set message mode on handle
	mode := uint32(windows.PIPE_READMODE_MESSAGE)
	_ = windows.SetNamedPipeHandleState(handle, &mode, nil, nil)

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	var bytesWritten uint32
	if err := windows.WriteFile(handle, data, &bytesWritten, nil); err != nil {
		return nil, fmt.Errorf("failed to write to named pipe: %w", err)
	}

	buf := make([]byte, 65536)
	var bytesRead uint32
	if err := windows.ReadFile(handle, buf, &bytesRead, nil); err != nil {
		return nil, fmt.Errorf("failed to read from named pipe: %w", err)
	}

	var resp IPCResponse
	if err := json.Unmarshal(buf[:bytesRead], &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal IPC response: %w", err)
	}

	return &resp, nil
}

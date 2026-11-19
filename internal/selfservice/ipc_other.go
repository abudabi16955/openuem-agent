//go:build !windows

package selfservice

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var defaultUnixSocketPath = filepath.Join(os.TempDir(), "openuem-selfservice.sock")

// IPCServer provides socket-based IPC for non-Windows platforms.
type IPCServer struct {
	socketPath string
	dispatcher *IPCDispatcher
	listener   net.Listener
	quitChan   chan struct{}
	wg         sync.WaitGroup
	running    bool
	mu         sync.Mutex
}

// NewIPCServer initializes the IPC server for non-Windows platforms.
func NewIPCServer(socketPath string, dispatcher *IPCDispatcher) *IPCServer {
	if socketPath == "" {
		socketPath = defaultUnixSocketPath
	}
	return &IPCServer{
		socketPath: socketPath,
		dispatcher: dispatcher,
		quitChan:   make(chan struct{}),
	}
}

// Start listens on a Unix domain socket and handles incoming connections.
func (s *IPCServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	_ = os.Remove(s.socketPath)
	l, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on unix socket %s: %w", s.socketPath, err)
	}

	_ = os.Chmod(s.socketPath, 0666)
	s.listener = l
	s.running = true

	s.wg.Add(1)
	go s.listenLoop()

	log.Printf("[INFO]: Mock IPC server listening on unix socket %s", s.socketPath)
	return nil
}

// Stop closes the listener and removes the socket file.
func (s *IPCServer) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.quitChan)
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.mu.Unlock()

	s.wg.Wait()
	_ = os.Remove(s.socketPath)
	log.Println("[INFO]: Mock IPC server stopped")
}

func (s *IPCServer) listenLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.quitChan:
				return
			default:
				time.Sleep(50 * time.Millisecond)
				continue
			}
		}

		go s.handleConnection(conn)
	}
}

func (s *IPCServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 65536)
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		return
	}

	if n > 0 {
		responseBytes := s.dispatcher.HandleMessage(buf[:n])
		_, _ = conn.Write(responseBytes)
	}
}

// CallIPC connects to the mock IPC server, sends IPCRequest, and decodes IPCResponse.
func CallIPC(socketPath string, req IPCRequest) (*IPCResponse, error) {
	if socketPath == "" {
		socketPath = defaultUnixSocketPath
	}

	conn, err := net.DialTimeout("unix", socketPath, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to unix socket %s: %w", socketPath, err)
	}
	defer conn.Close()

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	buf := make([]byte, 65536)
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp IPCResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &resp, nil
}

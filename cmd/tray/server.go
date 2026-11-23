package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/open-uem/openuem-agent/internal/selfservice"
)

//go:embed assets/*
var assetsFS embed.FS

// TrayServer serves the embedded UI and proxies requests to the OpenUEM named pipe.
type TrayServer struct {
	listener net.Listener
	port     int
	apiToken string
}

// StartTrayServer starts the local HTTP bridge server for the tray UI with Bearer token authentication (S-15).
func StartTrayServer(preferredPort int) (*TrayServer, error) {
	// Generate a cryptographically secure random session token (S-15)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate secure tray token: %w", err)
	}
	apiToken := hex.EncodeToString(tokenBytes)

	addr := fmt.Sprintf("127.0.0.1:%d", preferredPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		// Fallback to random free port
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("failed to bind tray http server: %w", err)
		}
	}

	port := listener.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()

	// Load and prepare embedded index.html with injected session token
	rawIndex, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded index.html: %w", err)
	}
	indexHTML := strings.Replace(string(rawIndex), "{{TRAY_AUTH_TOKEN}}", apiToken, 1)

	// Serve embedded assets
	subFS, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return nil, fmt.Errorf("failed to locate embedded assets: %w", err)
	}
	fileServer := http.FileServer(http.FS(subFS))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(indexHTML))
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	// S-15: Authentication middleware requiring Authorization: Bearer <token>
	requireAuth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			expectedHeader := "Bearer " + apiToken
			if authHeader == "" || authHeader != expectedHeader {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": false,
					"error":   "Unauthorized",
				})
				return
			}
			next(w, r)
		}
	}

	// API: Get Catalog
	mux.HandleFunc("/api/catalog", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp, err := selfservice.CallIPC("", selfservice.IPCRequest{Action: "get_catalog"})
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))

	// API: Install Package
	mux.HandleFunc("/api/install", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			PackageID string `json:"package_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "invalid json payload"})
			return
		}

		resp, err := selfservice.CallIPC("", selfservice.IPCRequest{
			Action:    "install",
			PackageID: payload.PackageID,
		})
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))

	// API: Task & Package Status
	mux.HandleFunc("/api/status", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		taskID := r.URL.Query().Get("task_id")
		packageID := r.URL.Query().Get("package_id")

		resp, err := selfservice.CallIPC("", selfservice.IPCRequest{
			Action:    "get_status",
			TaskID:    taskID,
			PackageID: packageID,
		})
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))

	server := &http.Server{
		Handler: mux,
	}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[ERROR]: tray http server stopped: %v", err)
		}
	}()

	log.Printf("[INFO]: OpenUEM Tray Portal UI running on http://127.0.0.1:%d", port)
	return &TrayServer{
		listener: listener,
		port:     port,
		apiToken: apiToken,
	}, nil
}

// GetURL returns the local browser URL for the UI.
func (s *TrayServer) GetURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", s.port)
}

// GetAPIToken returns the generated session bearer token.
func (s *TrayServer) GetAPIToken() string {
	return s.apiToken
}

// Close stops the HTTP listener.
func (s *TrayServer) Close() error {
	return s.listener.Close()
}

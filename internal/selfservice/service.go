package selfservice

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/nats-io/nats.go"
)

// ServiceConfig holds the parameters to initialize the Self-Service module.
type ServiceConfig struct {
	BaseDir        string
	ConsoleBaseURL string
	AgentCert      string
	AgentKey       string
	CACert         string
	NATSConn       *nats.Conn
	AgentUUID      string
	RefreshReport  func()
}

// Service manages all self-service portal functionality inside openuem-agent.
type Service struct {
	config          ServiceConfig
	catalogMgr      *CatalogManager
	downloader      *Downloader
	executor        Executor
	reporter        DeployReporter
	queue           *InstallationQueue
	installedStore  *InstalledStore
	ipcServer       *IPCServer
	legacyIPCServer *IPCServer
	natsSub         *nats.Subscription
	pruneTicker     *time.Ticker
	quitChan        chan struct{}
}

// NewService creates a new SelfService service instance.
func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.BaseDir == "" {
		cfg.BaseDir = GetSelfServiceDir()
	}

	catalogMgr, err := NewCatalogManager(cfg.BaseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to init catalog manager: %w", err)
	}

	installedStore, err := NewInstalledStore(filepath.Join(cfg.BaseDir, "installed_packages.json"))
	if err != nil {
		log.Printf("[WARN]: failed to init installed store: %v", err)
	}

	// Setup mTLS client for package downloads if certificates exist
	var httpClient *http.Client
	if cfg.AgentCert != "" && cfg.AgentKey != "" && cfg.CACert != "" {
		if client, err := BuildMTLSClient(cfg.AgentCert, cfg.AgentKey, cfg.CACert); err == nil {
			httpClient = client
		} else {
			log.Printf("[WARN]: failed to build mTLS client for selfservice: %v, falling back to default client", err)
			httpClient = &http.Client{Timeout: 15 * time.Minute}
		}
	} else {
		httpClient = &http.Client{
			Timeout: 15 * time.Minute,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
			},
		}
	}

	cacheDir := filepath.Join(cfg.BaseDir, "cache")
	downloader, err := NewDownloader(cacheDir, httpClient)
	if err != nil {
		return nil, fmt.Errorf("failed to init downloader: %w", err)
	}

	executor := NewWindowsExecutor(30 * time.Minute)
	reporter := NewNATSReporter(cfg.NATSConn, cfg.RefreshReport)

	queue := NewInstallationQueue(downloader, executor, reporter, cfg.AgentUUID, cfg.ConsoleBaseURL, installedStore)
	dispatcher := NewIPCDispatcher(catalogMgr, queue, installedStore)
	ipcServer := NewIPCServer(DefaultNamedPipeName, dispatcher)
	legacyIPCServer := NewIPCServer(LegacyNamedPipeName, dispatcher)

	return &Service{
		config:          cfg,
		catalogMgr:      catalogMgr,
		downloader:      downloader,
		executor:        executor,
		reporter:        reporter,
		queue:           queue,
		installedStore:  installedStore,
		ipcServer:       ipcServer,
		legacyIPCServer: legacyIPCServer,
		quitChan:        make(chan struct{}),
		pruneTicker:     time.NewTicker(24 * time.Hour),
	}, nil
}

// Start activates the named pipe server, NATS subscriptions, and background tasks.
func (s *Service) Start() error {
	if err := s.ipcServer.Start(); err != nil {
		return fmt.Errorf("failed to start IPC server: %w", err)
	}
	if s.legacyIPCServer != nil {
		if err := s.legacyIPCServer.Start(); err != nil {
			log.Printf("[WARN]: failed to start legacy IPC server on %s: %v", LegacyNamedPipeName, err)
		}
	}

	// Subscribe to NATS self-service update notifications
	if s.config.NATSConn != nil && s.config.NATSConn.IsConnected() {
		sub, err := s.config.NATSConn.Subscribe("agent.selfservice.update", func(msg *nats.Msg) {
			log.Println("[INFO]: received agent.selfservice.update notification via NATS")
			go s.SyncCatalog()
		})
		if err != nil {
			log.Printf("[ERROR]: failed to subscribe to agent.selfservice.update: %v", err)
		} else {
			s.natsSub = sub
			log.Println("[INFO]: subscribed to agent.selfservice.update")
		}

		// Also subscribe to wildcard agent.selfservice.> for extensibility
		_, _ = s.config.NATSConn.Subscribe("agent.selfservice.>", func(msg *nats.Msg) {
			if msg.Subject != "agent.selfservice.update" {
				log.Printf("[INFO]: received selfservice subject %s", msg.Subject)
			}
		})
	}

	// Initial sync from console if URL is available
	go s.SyncCatalog()

	// Start cache maintenance routine
	go s.maintenanceLoop()

	log.Println("[INFO]: Self-service service started successfully")
	return nil
}

// Stop gracefully shuts down all self-service components.
func (s *Service) Stop() {
	if s.pruneTicker != nil {
		s.pruneTicker.Stop()
	}

	close(s.quitChan)

	if s.natsSub != nil {
		_ = s.natsSub.Unsubscribe()
	}

	if s.ipcServer != nil {
		s.ipcServer.Stop()
	}

	if s.legacyIPCServer != nil {
		s.legacyIPCServer.Stop()
	}

	if s.queue != nil {
		s.queue.Stop()
	}

	log.Println("[INFO]: Self-service service stopped")
}

// SyncCatalog refreshes the local package catalog from console.
func (s *Service) SyncCatalog() {
	if s.config.ConsoleBaseURL == "" {
		return
	}

	log.Printf("[INFO]: syncing self-service catalog from %s", s.config.ConsoleBaseURL)
	if err := s.catalogMgr.SyncFromConsole(s.config.ConsoleBaseURL, s.downloader.httpClient); err != nil {
		log.Printf("[WARN]: failed to sync catalog from console: %v", err)
	} else {
		log.Println("[INFO]: self-service catalog synced successfully")
	}
}

// UpdateNATSConnection updates the active NATS connection and re-subscribes.
func (s *Service) UpdateNATSConnection(conn *nats.Conn) {
	s.config.NATSConn = conn
	if s.reporter != nil {
		s.reporter = NewNATSReporter(conn, s.config.RefreshReport)
		s.queue.reporter = s.reporter
	}

	if conn != nil && conn.IsConnected() {
		if s.natsSub != nil {
			_ = s.natsSub.Unsubscribe()
		}
		sub, err := conn.Subscribe("agent.selfservice.update", func(msg *nats.Msg) {
			log.Println("[INFO]: received agent.selfservice.update notification via NATS")
			go s.SyncCatalog()
		})
		if err == nil {
			s.natsSub = sub
		}
	}
}

func (s *Service) maintenanceLoop() {
	for {
		select {
		case <-s.quitChan:
			return
		case <-s.pruneTicker.C:
			// Prune cache items older than 7 days
			if s.downloader != nil {
				_ = s.downloader.PruneCache(7 * 24 * time.Hour)
			}
		}
	}
}

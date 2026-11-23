package selfservice

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PackageItem represents a single package available in the self-service catalog.
type PackageItem struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description,omitempty"`
	Version          string `json:"version,omitempty"`
	InstalledVersion string `json:"installed_version,omitempty"`
	Publisher        string `json:"publisher,omitempty"`
	Category         string `json:"category,omitempty"`
	IconURL          string `json:"icon_url,omitempty"`
	PackageType      string `json:"package_type"` // msi, exe, zip, winget
	SourceType       string `json:"source_type"`  // upload, https
	DownloadURL      string `json:"download_url,omitempty"`
	Checksum         string `json:"checksum,omitempty"`
	SHA256           string `json:"sha256,omitempty"`
	SilentArgs       string `json:"silent_args,omitempty"`
	ExecContext      string `json:"exec_context"` // system, user
	Enabled          bool   `json:"enabled"`
}

// GetChecksum returns SHA256 or Checksum, whichever is provided.
func (p *PackageItem) GetChecksum() string {
	if p.SHA256 != "" {
		return p.SHA256
	}
	return p.Checksum
}

// Catalog represents the cached collection of self-service packages.
type Catalog struct {
	UpdatedAt time.Time     `json:"updated_at"`
	Packages  []PackageItem `json:"packages"`
}

// CatalogManager provides thread-safe access and persistence for the catalog.
type CatalogManager struct {
	mu          sync.RWMutex
	baseDir     string
	catalogPath string
	catalog     *Catalog
}

// GetSelfServiceDir determines the root directory for self-service portal data.
func GetSelfServiceDir() string {
	if override := os.Getenv("OPENUEM_SELFSERVICE_DIR"); override != "" {
		return override
	}
	programData := os.Getenv("ProgramData")
	if programData != "" {
		return filepath.Join(programData, "OpenUEM", "selfservice")
	}
	return filepath.Join(os.TempDir(), "openuem", "selfservice")
}

// NewCatalogManager initializes a CatalogManager for the given base directory.
func NewCatalogManager(baseDir string) (*CatalogManager, error) {
	if baseDir == "" {
		baseDir = GetSelfServiceDir()
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create selfservice dir: %w", err)
	}

	// Apply strict permissions to catalog base directory
	_ = SetStrictSystemAdminPermissions(baseDir)

	cm := &CatalogManager{
		baseDir:     baseDir,
		catalogPath: filepath.Join(baseDir, "catalog.json"),
		catalog: &Catalog{
			UpdatedAt: time.Now(),
			Packages:  []PackageItem{},
		},
	}

	// Load existing catalog if present
	if _, err := os.Stat(cm.catalogPath); err == nil {
		if err := cm.Load(); err != nil {
			log.Printf("[WARN]: could not load existing catalog.json: %v", err)
		}
	}

	return cm, nil
}

// Load reads and parses catalog.json from disk.
func (cm *CatalogManager) Load() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := os.ReadFile(cm.catalogPath)
	if err != nil {
		return err
	}

	var cat Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return err
	}

	cm.catalog = &cat
	return nil
}

// Save atomically writes the current catalog to catalog.json.
func (cm *CatalogManager) Save() error {
	cm.mu.RLock()
	data, err := json.MarshalIndent(cm.catalog, "", "  ")
	cm.mu.RUnlock()
	if err != nil {
		return err
	}

	tmpPath := cm.catalogPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	_ = SetStrictSystemAdminPermissions(tmpPath)

	if err := os.Rename(tmpPath, cm.catalogPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	_ = SetStrictSystemAdminPermissions(cm.catalogPath)
	return nil
}

// GetCatalog returns a copy of the current Catalog.
func (cm *CatalogManager) GetCatalog() Catalog {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	packagesCopy := make([]PackageItem, len(cm.catalog.Packages))
	copy(packagesCopy, cm.catalog.Packages)

	return Catalog{
		UpdatedAt: cm.catalog.UpdatedAt,
		Packages:  packagesCopy,
	}
}

// FindPackage searches the catalog for an enabled package by ID.
func (cm *CatalogManager) FindPackage(id string) (*PackageItem, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for _, p := range cm.catalog.Packages {
		if p.ID == id {
			pkgCopy := p
			return &pkgCopy, true
		}
	}
	return nil, false
}

// UpdatePackages updates the catalog packages list and saves to disk.
func (cm *CatalogManager) UpdatePackages(packages []PackageItem) error {
	cm.mu.Lock()
	cm.catalog.Packages = packages
	cm.catalog.UpdatedAt = time.Now()
	cm.mu.Unlock()

	return cm.Save()
}

// SyncFromConsole fetches packages list from the console API (/api/v1/packages) and updates catalog.
func (cm *CatalogManager) SyncFromConsole(consoleBaseURL string, httpClient *http.Client) error {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	url := fmt.Sprintf("%s/api/v1/packages", consoleBaseURL)
	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch packages from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to fetch packages, status %d: %s", resp.StatusCode, string(body))
	}

	var packages []PackageItem
	if err := json.NewDecoder(resp.Body).Decode(&packages); err != nil {
		return fmt.Errorf("failed to decode packages json: %w", err)
	}

	// Normalize checksum/sha256
	for i := range packages {
		if packages[i].SHA256 == "" && packages[i].Checksum != "" {
			packages[i].SHA256 = packages[i].Checksum
		}
		if packages[i].Checksum == "" && packages[i].SHA256 != "" {
			packages[i].Checksum = packages[i].SHA256
		}
	}

	return cm.UpdatePackages(packages)
}

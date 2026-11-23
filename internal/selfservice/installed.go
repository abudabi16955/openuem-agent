package selfservice

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var scanRegistryInstalled = ScanRegistryInstalled

// InstalledPackageRecord stores metadata about an installed package.
type InstalledPackageRecord struct {
	PackageID   string    `json:"package_id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installed_at"`
}

// InstalledStore manages persistent storage of installed packages in installed_packages.json.
type InstalledStore struct {
	mu       sync.RWMutex
	filePath string
	records  map[string]InstalledPackageRecord // key: PackageID
}

// NewInstalledStore creates or opens an existing installed packages store.
func NewInstalledStore(filePath string) (*InstalledStore, error) {
	if filePath == "" {
		filePath = filepath.Join(GetSelfServiceDir(), "installed_packages.json")
	}

	store := &InstalledStore{
		filePath: filePath,
		records:  make(map[string]InstalledPackageRecord),
	}

	if err := store.load(); err != nil && !os.IsNotExist(err) {
		log.Printf("[WARN]: failed to load installed packages from %s: %v", filePath, err)
	}

	return store, nil
}

func (s *InstalledStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	var recs map[string]InstalledPackageRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		// Try unmarshaling as slice for backward compatibility
		var list []InstalledPackageRecord
		if errSlice := json.Unmarshal(data, &list); errSlice == nil {
			s.records = make(map[string]InstalledPackageRecord)
			for _, item := range list {
				s.records[item.PackageID] = item
			}
			return nil
		}
		return fmt.Errorf("failed to unmarshal installed packages: %w", err)
	}

	s.records = recs
	return nil
}

func (s *InstalledStore) saveLocked() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory for installed packages store: %w", err)
	}

	data, err := json.MarshalIndent(s.records, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal installed packages: %w", err)
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", s.filePath, time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write tmp installed packages: %w", err)
	}

	_ = SetStrictSystemAdminPermissions(tmpFile)

	if err := os.Rename(tmpFile, s.filePath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to commit installed packages store: %w", err)
	}

	_ = SetStrictSystemAdminPermissions(s.filePath)
	return nil
}

// RecordInstalled records or updates an installed package in the store.
func (s *InstalledStore) RecordInstalled(pkg PackageItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.records[pkg.ID] = InstalledPackageRecord{
		PackageID:   pkg.ID,
		Name:        pkg.Name,
		Version:     pkg.Version,
		InstalledAt: time.Now(),
	}

	return s.saveLocked()
}

// GetInstalled retrieves the record for a specific package ID.
func (s *InstalledStore) GetInstalled(packageID string) (InstalledPackageRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.records[packageID]
	return rec, ok
}

// GetInstalledByName retrieves the record matching a package name (case-insensitive).
func (s *InstalledStore) GetInstalledByName(name string) (InstalledPackageRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clean := strings.ToLower(strings.TrimSpace(name))
	for _, rec := range s.records {
		if strings.ToLower(strings.TrimSpace(rec.Name)) == clean {
			return rec, true
		}
	}
	return InstalledPackageRecord{}, false
}

// GetAllInstalled returns a map copy of all recorded installations.
func (s *InstalledStore) GetAllInstalled() map[string]InstalledPackageRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]InstalledPackageRecord, len(s.records))
	for k, v := range s.records {
		out[k] = v
	}
	return out
}

// DetectInstalledVersion performs hybrid detection by querying installed_packages.json
// and scanning the Windows registry (HKLM, HKCU, and Wow6432Node Uninstall keys).
func DetectInstalledVersion(pkg PackageItem, store *InstalledStore) string {
	var versionStore string
	if store != nil {
		if rec, found := store.GetInstalled(pkg.ID); found && rec.Version != "" {
			versionStore = rec.Version
		} else if rec, found := store.GetInstalledByName(pkg.Name); found && rec.Version != "" {
			versionStore = rec.Version
		}
	}

	var versionReg string
	regApps, err := scanRegistryInstalled()
	if err == nil && len(regApps) > 0 {
		cleanPkgName := strings.ToLower(strings.TrimSpace(pkg.Name))
		cleanPkgID := strings.ToLower(strings.TrimSpace(pkg.ID))

		// 1. Direct match on package ID or name
		if v, ok := regApps[cleanPkgID]; ok && v != "" {
			versionReg = v
		} else if v, ok := regApps[cleanPkgName]; ok && v != "" {
			versionReg = v
		} else {
			// 2. Substring match on app display name: registry appName must contain full cleanPkgName, with minimum length >= 4 (S-17)
			if len(cleanPkgName) >= 4 {
				for appName, appVer := range regApps {
					if len(appName) >= 4 && strings.Contains(appName, cleanPkgName) {
						if appVer != "" {
							versionReg = appVer
							break
						}
					}
				}
			}
		}
	}

	if versionStore != "" && versionReg != "" {
		if CompareVersions(versionReg, versionStore) > 0 {
			return versionReg
		}
		return versionStore
	}

	if versionReg != "" {
		return versionReg
	}

	return versionStore
}

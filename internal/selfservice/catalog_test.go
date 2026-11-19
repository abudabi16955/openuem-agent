package selfservice

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogManager_SaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	cm, err := NewCatalogManager(tempDir)
	require.NoError(t, err)

	packages := []PackageItem{
		{
			ID:          "pkg-1",
			Name:        "App One",
			Version:     "1.0.0",
			PackageType: "msi",
			Enabled:     true,
		},
		{
			ID:          "pkg-2",
			Name:        "App Two",
			Version:     "2.1.0",
			PackageType: "exe",
			Enabled:     false,
		},
	}

	err = cm.UpdatePackages(packages)
	require.NoError(t, err)

	// Verify file exists on disk
	catalogFile := filepath.Join(tempDir, "catalog.json")
	assert.FileExists(t, catalogFile)

	// Create new CatalogManager loading from same path
	cm2, err := NewCatalogManager(tempDir)
	require.NoError(t, err)

	cat := cm2.GetCatalog()
	assert.Len(t, cat.Packages, 2)
	assert.Equal(t, "App One", cat.Packages[0].Name)

	// Test FindPackage
	pkg1, found := cm2.FindPackage("pkg-1")
	assert.True(t, found)
	assert.Equal(t, "App One", pkg1.Name)

	_, found = cm2.FindPackage("non-existent")
	assert.False(t, found)
}

func TestCatalogManager_ConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	cm, err := NewCatalogManager(tempDir)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = cm.UpdatePackages([]PackageItem{
				{
					ID:          "concurrent-pkg",
					Name:        "Test",
					PackageType: "msi",
					Enabled:     true,
				},
			})
			_ = cm.GetCatalog()
			_, _ = cm.FindPackage("concurrent-pkg")
		}(i)
	}
	wg.Wait()
}

func TestCatalogManager_SyncFromConsole(t *testing.T) {
	tempDir := t.TempDir()
	cm, err := NewCatalogManager(tempDir)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/packages", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]PackageItem{
			{
				ID:          "api-pkg-1",
				Name:        "Console App",
				Version:     "3.0",
				PackageType: "zip",
				Checksum:    "abcdef123456",
				Enabled:     true,
			},
		})
	}))
	defer server.Close()

	err = cm.SyncFromConsole(server.URL, server.Client())
	require.NoError(t, err)

	cat := cm.GetCatalog()
	require.Len(t, cat.Packages, 1)
	assert.Equal(t, "Console App", cat.Packages[0].Name)
	assert.Equal(t, "abcdef123456", cat.Packages[0].SHA256)
}

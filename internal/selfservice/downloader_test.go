package selfservice

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloader_SuccessAndVerification(t *testing.T) {
	cacheDir := t.TempDir()
	content := []byte("installer binary test payload 12345")
	hasher := sha256.New()
	hasher.Write(content)
	correctChecksum := hex.EncodeToString(hasher.Sum(nil))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", "attachment; filename=\"custom_setup.exe\"")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	dl, err := NewDownloader(cacheDir, server.Client())
	require.NoError(t, err)

	pkg := PackageItem{
		ID:          "pkg-100",
		Name:        "TestApp",
		PackageType: "exe",
		DownloadURL: server.URL + "/download",
		Checksum:    correctChecksum,
	}

	path, err := dl.Download(pkg, "")
	require.NoError(t, err)
	assert.FileExists(t, path)
	assert.Equal(t, "custom_setup.exe", filepath.Base(path))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, data)

	// Second download should reuse cached file
	path2, err := dl.Download(pkg, "")
	require.NoError(t, err)
	assert.Equal(t, path, path2)
}

func TestDownloader_ChecksumMismatchRemovesFile(t *testing.T) {
	cacheDir := t.TempDir()
	content := []byte("some content that does not match expected checksum")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	dl, err := NewDownloader(cacheDir, server.Client())
	require.NoError(t, err)

	pkg := PackageItem{
		ID:          "pkg-corrupt",
		Name:        "CorruptApp",
		PackageType: "msi",
		DownloadURL: server.URL + "/download",
		Checksum:    "deadbeef00000000000000000000000000000000000000000000000000000000",
	}

	path, err := dl.Download(pkg, "")
	require.Error(t, err)
	assert.Empty(t, path)
	assert.Contains(t, err.Error(), "checksum mismatch")

	// Ensure no lingering temporary or final files in cache
	pkgDir := filepath.Join(cacheDir, pkg.ID)
	entries, _ := os.ReadDir(pkgDir)
	assert.Empty(t, entries, "corrupted file must be deleted immediately from cache")
}

func TestDownloader_PruneCache(t *testing.T) {
	cacheDir := t.TempDir()
	dl, err := NewDownloader(cacheDir, nil)
	require.NoError(t, err)

	// Create old cache folder and recent cache folder
	oldDir := filepath.Join(cacheDir, "old-pkg")
	recentDir := filepath.Join(cacheDir, "recent-pkg")
	require.NoError(t, os.MkdirAll(oldDir, 0700))
	require.NoError(t, os.MkdirAll(recentDir, 0700))

	// Fake mtime for old directory
	oldTime := time.Now().Add(-10 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(oldDir, oldTime, oldTime))

	err = dl.PruneCache(7 * 24 * time.Hour)
	require.NoError(t, err)

	assert.NoDirExists(t, oldDir, "old cache entry should be pruned")
	assert.DirExists(t, recentDir, "recent cache entry should be kept")
}

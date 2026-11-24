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
	content := append([]byte{'M', 'Z'}, []byte("installer binary test payload 12345")...)
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

func TestDownloader_SanityCheckCorruptExeRemovesCache(t *testing.T) {
	cacheDir := t.TempDir()
	// An exe file without MZ header
	content := []byte("INVALID_NOT_MZ_HEADER_PAYLOAD")
	hasher := sha256.New()
	hasher.Write(content)
	correctChecksum := hex.EncodeToString(hasher.Sum(nil))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", "attachment; filename=\"bad_setup.exe\"")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	dl, err := NewDownloader(cacheDir, server.Client())
	require.NoError(t, err)

	pkg := PackageItem{
		ID:          "pkg-bad-exe",
		Name:        "BadExeApp",
		PackageType: "exe",
		DownloadURL: server.URL + "/download",
		Checksum:    correctChecksum,
	}

	path, err := dl.Download(pkg, "")
	require.Error(t, err)
	assert.Empty(t, path)
	assert.Contains(t, err.Error(), "sanity check failed")

	// Ensure cache directory has been cleaned up
	pkgDir := filepath.Join(cacheDir, pkg.ID)
	assert.NoDirExists(t, pkgDir, "corrupt executable cache folder must be removed")
}

func TestValidateInstaller(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("non-existent file", func(t *testing.T) {
		err := ValidateInstaller(filepath.Join(tmpDir, "missing.exe"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("0-byte file", func(t *testing.T) {
		emptyPath := filepath.Join(tmpDir, "empty.exe")
		require.NoError(t, os.WriteFile(emptyPath, []byte{}, 0644))
		err := ValidateInstaller(emptyPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty (0 bytes)")
	})

	t.Run("valid exe with MZ signature", func(t *testing.T) {
		validExePath := filepath.Join(tmpDir, "valid.exe")
		require.NoError(t, os.WriteFile(validExePath, []byte{'M', 'Z', 0x90, 0x00, 0x03}, 0755))
		err := ValidateInstaller(validExePath)
		assert.NoError(t, err)
	})

	t.Run("corrupt exe missing MZ signature", func(t *testing.T) {
		corruptExePath := filepath.Join(tmpDir, "corrupt.exe")
		require.NoError(t, os.WriteFile(corruptExePath, []byte{0x7F, 'E', 'L', 'F'}, 0755))
		err := ValidateInstaller(corruptExePath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing MZ PE header")
	})

	t.Run("valid non-exe file (msi/zip)", func(t *testing.T) {
		msiPath := filepath.Join(tmpDir, "installer.msi")
		require.NoError(t, os.WriteFile(msiPath, []byte{0xD0, 0xCF, 0x11, 0xE0}, 0644))
		err := ValidateInstaller(msiPath)
		assert.NoError(t, err)
	})
}

func TestRemoveCachedFile(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "selfservice", "cache", "test-pkg-id")
	require.NoError(t, os.MkdirAll(cacheDir, 0700))

	filePath := filepath.Join(cacheDir, "installer.exe")
	require.NoError(t, os.WriteFile(filePath, []byte("test"), 0644))

	err := RemoveCachedFile(filePath)
	assert.NoError(t, err)
	assert.NoFileExists(t, filePath)
	assert.NoDirExists(t, cacheDir)

	// Calling on empty string does nothing
	assert.NoError(t, RemoveCachedFile(""))
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

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

// buildRealisticPEHeader creates a realistic DOS header of at least 64 bytes with MZ signature.
func buildRealisticPEHeader(totalSize int) []byte {
	if totalSize < 64 {
		totalSize = 64
	}
	buf := make([]byte, totalSize)
	buf[0] = 'M'
	buf[1] = 'Z'
	// DOS header magic and minimal fields
	buf[0x3C] = 0x40 // e_lfanew pointing to offset 64
	return buf
}

// buildRealisticMSIHeader creates a realistic OLE CFBF header of at least 512 bytes.
func buildRealisticMSIHeader(totalSize int) []byte {
	if totalSize < 512 {
		totalSize = 512
	}
	buf := make([]byte, totalSize)
	// OLE CFBF magic: 0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1
	buf[0] = 0xD0
	buf[1] = 0xCF
	buf[2] = 0x11
	buf[3] = 0xE0
	buf[4] = 0xA1
	buf[5] = 0xB1
	buf[6] = 0x1A
	buf[7] = 0xE1
	return buf
}

// buildRealisticZIPHeader creates a minimal valid ZIP end-of-central-directory record (>= 22 bytes).
func buildRealisticZIPHeader(totalSize int) []byte {
	if totalSize < 22 {
		totalSize = 22
	}
	buf := make([]byte, totalSize)
	buf[0] = 'P'
	buf[1] = 'K'
	buf[2] = 0x05
	buf[3] = 0x06
	return buf
}

func TestDownloader_SuccessAndVerification(t *testing.T) {
	cacheDir := t.TempDir()
	content := append(buildRealisticPEHeader(128), []byte("installer binary test payload 12345")...)
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
	content := make([]byte, 128)
	copy(content, "INVALID_NOT_MZ_HEADER_PAYLOAD")
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

	t.Run("valid exe with full DOS header", func(t *testing.T) {
		validExePath := filepath.Join(tmpDir, "valid.exe")
		require.NoError(t, os.WriteFile(validExePath, buildRealisticPEHeader(128), 0755))
		err := ValidateInstaller(validExePath)
		assert.NoError(t, err)
	})

	t.Run("valid exe with uppercase extension", func(t *testing.T) {
		validExePath := filepath.Join(tmpDir, "VALID_UPPER.EXE")
		require.NoError(t, os.WriteFile(validExePath, buildRealisticPEHeader(128), 0755))
		err := ValidateInstaller(validExePath)
		assert.NoError(t, err)
	})

	t.Run("corrupt exe too short (<64 bytes DOS header)", func(t *testing.T) {
		shortExePath := filepath.Join(tmpDir, "short.exe")
		require.NoError(t, os.WriteFile(shortExePath, []byte{'M', 'Z', 0x90, 0x00}, 0755))
		err := ValidateInstaller(shortExePath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "smaller than minimum DOS header")
	})

	t.Run("corrupt exe missing MZ signature", func(t *testing.T) {
		corruptExePath := filepath.Join(tmpDir, "corrupt.exe")
		corruptContent := make([]byte, 128)
		copy(corruptContent, []byte{0x7F, 'E', 'L', 'F'})
		require.NoError(t, os.WriteFile(corruptExePath, corruptContent, 0755))
		err := ValidateInstaller(corruptExePath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing MZ PE header")
	})

	t.Run("valid msi with OLE header (>= 512 bytes)", func(t *testing.T) {
		msiPath := filepath.Join(tmpDir, "installer.msi")
		require.NoError(t, os.WriteFile(msiPath, buildRealisticMSIHeader(512), 0644))
		err := ValidateInstaller(msiPath)
		assert.NoError(t, err)
	})

	t.Run("valid msi with uppercase extension", func(t *testing.T) {
		msiPath := filepath.Join(tmpDir, "INSTALLER_UPPER.MSI")
		require.NoError(t, os.WriteFile(msiPath, buildRealisticMSIHeader(512), 0644))
		err := ValidateInstaller(msiPath)
		assert.NoError(t, err)
	})

	t.Run("corrupt msi too short (<512 bytes)", func(t *testing.T) {
		shortMsiPath := filepath.Join(tmpDir, "short.msi")
		require.NoError(t, os.WriteFile(shortMsiPath, []byte{0xD0, 0xCF, 0x11, 0xE0}, 0644))
		err := ValidateInstaller(shortMsiPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "smaller than minimum OLE sector")
	})

	t.Run("corrupt msi wrong magic", func(t *testing.T) {
		corruptMsiPath := filepath.Join(tmpDir, "corrupt.msi")
		corruptContent := make([]byte, 512)
		copy(corruptContent, "NOT_AN_OLE_COMPOUND_HEADER")
		require.NoError(t, os.WriteFile(corruptMsiPath, corruptContent, 0644))
		err := ValidateInstaller(corruptMsiPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing OLE compound file header")
	})

	t.Run("valid zip archive (>= 22 bytes)", func(t *testing.T) {
		zipPath := filepath.Join(tmpDir, "bundle.zip")
		require.NoError(t, os.WriteFile(zipPath, buildRealisticZIPHeader(32), 0644))
		err := ValidateInstaller(zipPath)
		assert.NoError(t, err)
	})

	t.Run("valid binary detected by magic without extension", func(t *testing.T) {
		noExtPath := filepath.Join(tmpDir, "binary_without_extension")
		require.NoError(t, os.WriteFile(noExtPath, buildRealisticPEHeader(128), 0755))
		err := ValidateInstaller(noExtPath)
		assert.NoError(t, err)
	})
}

func TestRemoveCachedFile_SafetyRules(t *testing.T) {
	t.Run("authorized package cache removal removes file and package dir", func(t *testing.T) {
		cacheRoot := t.TempDir()
		RegisterAllowedCacheDir(cacheRoot)

		pkgDir := filepath.Join(cacheRoot, "pkg-abc")
		require.NoError(t, os.MkdirAll(pkgDir, 0700))

		installerFile := filepath.Join(pkgDir, "setup.exe")
		require.NoError(t, os.WriteFile(installerFile, []byte("payload"), 0644))

		err := RemoveCachedFile(installerFile)
		assert.NoError(t, err)
		assert.NoFileExists(t, installerFile)
		assert.NoDirExists(t, pkgDir, "enclosing package subdirectory in cache should be deleted")
		assert.DirExists(t, cacheRoot, "cache root itself must remain intact")
	})

	t.Run("CRITICAL: outside directory must NOT be removed", func(t *testing.T) {
		outsideDir := t.TempDir()
		otherFile := filepath.Join(outsideDir, "critical_system_file.txt")
		require.NoError(t, os.WriteFile(otherFile, []byte("precious system data"), 0644))

		installerOutside := filepath.Join(outsideDir, "installer.exe")
		require.NoError(t, os.WriteFile(installerOutside, []byte("temporary installer"), 0644))

		err := RemoveCachedFile(installerOutside)
		assert.NoError(t, err)
		assert.NoFileExists(t, installerOutside, "the specific installer file should be removed")
		assert.DirExists(t, outsideDir, "outside directory must NEVER be removed!")
		assert.FileExists(t, otherFile, "other files in outside directory must NEVER be deleted!")
	})

	t.Run("empty string does nothing", func(t *testing.T) {
		assert.NoError(t, RemoveCachedFile(""))
	})
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
	assert.DirExists(t, recentDir, "recent cache entry should not be pruned")
}

package selfservice

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Downloader handles secure package downloading, cache management, and checksum verification.
type Downloader struct {
	cacheDir   string
	httpClient *http.Client
}

// NewDownloader creates a new Downloader with strict cache directory permissions.
func NewDownloader(cacheDir string, httpClient *http.Client) (*Downloader, error) {
	if cacheDir == "" {
		cacheDir = filepath.Join(GetSelfServiceDir(), "cache")
	}

	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Strictly restrict cache folder access to SYSTEM and Admins (no Users access to prevent TOCTOU)
	_ = SetCacheExclusivePermissions(cacheDir)

	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 15 * time.Minute,
		}
	}

	return &Downloader{
		cacheDir:   cacheDir,
		httpClient: httpClient,
	}, nil
}

// BuildMTLSClient builds an http.Client configured with client certificate and CA certificate for mTLS.
func BuildMTLSClient(certPath, keyPath, caPath string) (*http.Client, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load client key pair: %w", err)
	}

	caCertBytes, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read ca certificate: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertBytes) {
		return nil, fmt.Errorf("failed to append ca certificate to pool")
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS12,
	}

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   15 * time.Minute,
	}, nil
}

// Download downloads a package to the secure cache, verifies SHA-256, and returns the local file path.
func (d *Downloader) Download(pkg PackageItem, consoleBaseURL string) (string, error) {
	downloadURL := pkg.DownloadURL
	if !strings.HasPrefix(downloadURL, "http://") && !strings.HasPrefix(downloadURL, "https://") {
		if consoleBaseURL != "" {
			downloadURL = strings.TrimRight(consoleBaseURL, "/") + "/" + strings.TrimLeft(downloadURL, "/")
		} else {
			return "", fmt.Errorf("invalid package download URL: %s", downloadURL)
		}
	}

	pkgCacheDir := filepath.Join(d.cacheDir, pkg.ID)
	if err := os.MkdirAll(pkgCacheDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create package cache dir: %w", err)
	}
	_ = SetCacheExclusivePermissions(pkgCacheDir)

	// Determine destination filename
	ext := pkg.PackageType
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	defaultFilename := fmt.Sprintf("%s%s", pkg.ID, ext)

	// Check if already cached and valid
	expectedChecksum := strings.ToLower(strings.TrimSpace(pkg.GetChecksum()))
	cachedPath := filepath.Join(pkgCacheDir, defaultFilename)
	if fi, err := os.Stat(cachedPath); err == nil && fi.Size() > 0 && expectedChecksum != "" {
		if verifyChecksum(cachedPath, expectedChecksum) {
			log.Printf("[INFO]: package %s found in cache with valid checksum", pkg.Name)
			return cachedPath, nil
		}
		// Stale or corrupted, remove
		_ = os.Remove(cachedPath)
	}

	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create download request: %w", err)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("download failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Try extracting filename from Content-Disposition if present
	filename := defaultFilename
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if fn, ok := params["filename"]; ok && fn != "" {
				filename = filepath.Base(fn)
			}
		}
	}

	destPath := filepath.Join(pkgCacheDir, filename)
	tmpPath := destPath + ".tmp"

	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("failed to create temp cache file: %w", err)
	}

	hasher := sha256.New()
	multiWriter := io.MultiWriter(tmpFile, hasher)

	_, err = io.Copy(multiWriter, resp.Body)
	_ = tmpFile.Close()

	if err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write package data: %w", err)
	}

	actualChecksum := hex.EncodeToString(hasher.Sum(nil))

	// Verify SHA-256 checksum if provided
	if expectedChecksum != "" && actualChecksum != expectedChecksum {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("checksum mismatch for package %s: expected %s, got %s", pkg.Name, expectedChecksum, actualChecksum)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to finalize cache file: %w", err)
	}

	_ = SetCacheExclusivePermissions(destPath)
	return destPath, nil
}

// verifyChecksum checks if a file on disk matches the expected SHA-256 hash.
func verifyChecksum(filePath, expectedHex string) bool {
	f, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return false
	}

	actualHex := hex.EncodeToString(hasher.Sum(nil))
	return strings.EqualFold(actualHex, expectedHex)
}

// PruneCache deletes cached packages older than maxAge.
func (d *Downloader) PruneCache(maxAge time.Duration) error {
	entries, err := os.ReadDir(d.cacheDir)
	if err != nil {
		return err
	}

	threshold := time.Now().Add(-maxAge)
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(threshold) {
			targetPath := filepath.Join(d.cacheDir, entry.Name())
			_ = os.RemoveAll(targetPath)
			log.Printf("[INFO]: pruned old package cache: %s", targetPath)
		}
	}
	return nil
}

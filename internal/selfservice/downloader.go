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
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	allowedCacheDirsMu sync.RWMutex
	allowedCacheDirs   = make(map[string]struct{})
)

// RegisterAllowedCacheDir registers an authorized cache directory path for RemoveCachedFile.
func RegisterAllowedCacheDir(cacheDir string) {
	if cacheDir == "" {
		return
	}
	clean := filepath.Clean(cacheDir)
	allowedCacheDirsMu.Lock()
	defer allowedCacheDirsMu.Unlock()
	allowedCacheDirs[clean] = struct{}{}
}

// IsAllowedCacheSubdir checks whether candidateDir is a direct subdirectory of an authorized cache directory.
func IsAllowedCacheSubdir(candidateDir string) bool {
	cleanCandidate := filepath.Clean(candidateDir)
	parentOfCandidate := filepath.Clean(filepath.Dir(cleanCandidate))

	// 1. Check default cache dir
	defaultCacheDir := filepath.Clean(filepath.Join(GetSelfServiceDir(), "cache"))
	if strings.EqualFold(parentOfCandidate, defaultCacheDir) {
		return true
	}

	// 2. Check registered cache dirs
	allowedCacheDirsMu.RLock()
	defer allowedCacheDirsMu.RUnlock()
	for allowed := range allowedCacheDirs {
		if strings.EqualFold(parentOfCandidate, allowed) {
			return true
		}
	}
	return false
}

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
	RegisterAllowedCacheDir(cacheDir)

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

// DefaultConsolePort is the standard OpenUEM console HTTPS port.
const DefaultConsolePort = 1323

// DeriveConsoleURL derives a console HTTPS base URL from a NATS server address or host string.
// Examples:
//   "nats://192.168.1.140:4222" -> "https://192.168.1.140:1323"
//   "192.168.1.140:4222"        -> "https://192.168.1.140:1323"
//   "localhost"                 -> "https://localhost:1323"
//   ""                          -> "https://localhost:1323"
func DeriveConsoleURL(natsServerAddr string) string {
	raw := strings.TrimSpace(natsServerAddr)
	if raw == "" {
		if env := strings.TrimSpace(os.Getenv("OPENUEM_CONSOLE_URL")); env != "" {
			return strings.TrimRight(env, "/")
		}
		return fmt.Sprintf("https://localhost:%d", DefaultConsolePort)
	}

	// If multiple NATS servers are configured (comma-separated), pick the first one
	if idx := strings.Index(raw, ","); idx != -1 {
		raw = strings.TrimSpace(raw[:idx])
	}

	// Strip scheme if present (e.g. nats://, tls://, http://, https://)
	if idx := strings.Index(raw, "://"); idx != -1 {
		raw = raw[idx+3:]
	}

	// Strip userinfo if present (e.g. user:pass@host:port)
	if idx := strings.LastIndex(raw, "@"); idx != -1 {
		raw = raw[idx+1:]
	}

	// Strip path if present (e.g. host:port/path)
	if idx := strings.Index(raw, "/"); idx != -1 {
		raw = raw[:idx]
	}

	// Extract host part (removing port if present)
	host := raw
	if h, _, err := net.SplitHostPort(raw); err == nil && h != "" {
		host = h
	} else if strings.Contains(raw, ":") && !strings.Contains(raw, "]") {
		// IPv4:port
		parts := strings.Split(raw, ":")
		host = parts[0]
	}

	host = strings.TrimSpace(host)
	if host == "" {
		host = "localhost"
	}

	return fmt.Sprintf("https://%s:%d", host, DefaultConsolePort)
}

// Download downloads a package to the secure cache, verifies SHA-256, and returns the local file path.
func (d *Downloader) Download(pkg PackageItem, consoleBaseURL string) (string, error) {
	downloadURL := pkg.DownloadURL
	if !strings.HasPrefix(downloadURL, "http://") && !strings.HasPrefix(downloadURL, "https://") {
		baseURL := strings.TrimSpace(consoleBaseURL)
		if baseURL == "" {
			baseURL = DeriveConsoleURL("")
		}
		downloadURL = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(downloadURL, "/")
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

	// Check if already cached, valid checksum and passes sanity check
	expectedChecksum := strings.ToLower(strings.TrimSpace(pkg.GetChecksum()))
	cachedPath := filepath.Join(pkgCacheDir, defaultFilename)
	if fi, err := os.Stat(cachedPath); err == nil && fi.Size() > 0 && expectedChecksum != "" {
		if verifyChecksum(cachedPath, expectedChecksum) {
			if err := ValidateInstaller(cachedPath); err == nil {
				log.Printf("[INFO]: package %s found in cache with valid checksum and sanity check", pkg.Name)
				return cachedPath, nil
			}
		}
		// Stale or corrupted, remove
		_ = RemoveCachedFile(cachedPath)
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

	// Sanity check before finalizing cache file
	if err := ValidateInstaller(tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		_ = RemoveCachedFile(destPath)
		return "", fmt.Errorf("package sanity check failed for %s: %w", pkg.Name, err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		_ = RemoveCachedFile(destPath)
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

// ValidateInstaller checks if an installer file is valid:
// - Verifies that the file exists and has size > 0.
// - Handles filenames with or without extension, and case-insensitive extensions (.exe, .msi, .zip, etc.).
// - For PE (.exe): verifies minimum size (at least 64 bytes for DOS header) and MZ signature (0x4D, 0x5A).
// - For MSI (.msi): verifies minimum size (at least 512 bytes for OLE CFBF header) and OLE signature (0xD0, 0xCF, 0x11, 0xE0).
// - For ZIP (.zip): verifies minimum size (at least 22 bytes for EOCD) and PK signature (0x50, 0x4B).
// - For files without extension, detects type by header magic.
func ValidateInstaller(filePath string) error {
	fi, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("installer file not found: %w", err)
	}
	if fi.Size() == 0 {
		return fmt.Errorf("installer file is empty (0 bytes)")
	}

	cleanPath := filePath
	if strings.HasSuffix(strings.ToLower(cleanPath), ".tmp") {
		cleanPath = cleanPath[:len(cleanPath)-4]
	}
	ext := strings.ToLower(filepath.Ext(cleanPath))

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open installer file: %w", err)
	}
	defer f.Close()

	var header [512]byte
	n, readErr := io.ReadFull(f, header[:])
	if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
		return fmt.Errorf("failed to read installer header: %w", readErr)
	}

	// Check based on extension or detected magic
	isExe := ext == ".exe" || (ext == "" && n >= 2 && header[0] == 0x4D && header[1] == 0x5A)
	isMsi := ext == ".msi" || (ext == "" && n >= 4 && header[0] == 0xD0 && header[1] == 0xCF && header[2] == 0x11 && header[3] == 0xE0)
	isZip := ext == ".zip" || (ext == "" && n >= 2 && header[0] == 0x50 && header[1] == 0x4B)

	if isExe {
		if fi.Size() < 64 || n < 64 {
			return fmt.Errorf("corrupt executable: file size (%d bytes) smaller than minimum DOS header (64 bytes)", fi.Size())
		}
		if header[0] != 0x4D || header[1] != 0x5A { // M, Z
			return fmt.Errorf("corrupt executable: missing MZ PE header signature (got 0x%02X, 0x%02X)", header[0], header[1])
		}
		return nil
	}

	if isMsi {
		if fi.Size() < 512 || n < 512 {
			return fmt.Errorf("corrupt MSI: file size (%d bytes) smaller than minimum OLE sector (512 bytes)", fi.Size())
		}
		if header[0] != 0xD0 || header[1] != 0xCF || header[2] != 0x11 || header[3] != 0xE0 {
			return fmt.Errorf("corrupt MSI: missing OLE compound file header (got 0x%02X, 0x%02X, 0x%02X, 0x%02X)", header[0], header[1], header[2], header[3])
		}
		return nil
	}

	if isZip {
		if fi.Size() < 22 || n < 2 {
			return fmt.Errorf("corrupt ZIP: file size (%d bytes) smaller than minimum ZIP archive (22 bytes)", fi.Size())
		}
		if header[0] != 0x50 || header[1] != 0x4B { // P, K
			return fmt.Errorf("corrupt ZIP: missing PK header signature (got 0x%02X, 0x%02X)", header[0], header[1])
		}
		return nil
	}

	return nil
}

// RemoveCachedFile removes a faulty or failed installer file.
// The enclosing package directory (cleanDir) is removed ONLY if it is confirmed to be a direct
// subdirectory of an authorized agent cache directory (e.g. GetSelfServiceDir()/cache, a registered cache dir, or customCacheDirs).
// Under no circumstances will a parent directory outside the cache be removed.
func RemoveCachedFile(filePath string, customCacheDirs ...string) error {
	if filePath == "" {
		return nil
	}

	// Always remove the file first
	fileErr := os.Remove(filePath)

	dir := filepath.Dir(filePath)
	cleanDir := filepath.Clean(dir)

	// Guard against root, current dir, empty or volume roots
	if cleanDir == "" || cleanDir == "." || cleanDir == ".." || cleanDir == "/" || cleanDir == `\` ||
		cleanDir == filepath.VolumeName(cleanDir) || cleanDir == filepath.VolumeName(cleanDir)+`\` || cleanDir == filepath.VolumeName(cleanDir)+"/" {
		if fileErr != nil && !os.IsNotExist(fileErr) {
			return fileErr
		}
		return nil
	}

	parentOfDir := filepath.Clean(filepath.Dir(cleanDir))

	isAuthorized := false

	// Check customCacheDirs if provided
	for _, cDir := range customCacheDirs {
		if cDir != "" && strings.EqualFold(parentOfDir, filepath.Clean(cDir)) {
			isAuthorized = true
			break
		}
	}

	// Check default and registered cache dirs
	if !isAuthorized && IsAllowedCacheSubdir(cleanDir) {
		isAuthorized = true
	}

	if isAuthorized {
		if err := os.RemoveAll(cleanDir); err != nil {
			return err
		}
	}

	if fileErr != nil && !os.IsNotExist(fileErr) {
		return fileErr
	}
	return nil
}

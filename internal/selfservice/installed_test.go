package selfservice

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInstalledStore(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "installed_packages.json")

	store, err := NewInstalledStore(storePath)
	if err != nil {
		t.Fatalf("NewInstalledStore failed: %v", err)
	}

	pkg := PackageItem{
		ID:      "pkg-7zip",
		Name:    "7-Zip",
		Version: "23.01",
	}

	if err := store.RecordInstalled(pkg); err != nil {
		t.Fatalf("RecordInstalled failed: %v", err)
	}

	rec, ok := store.GetInstalled("pkg-7zip")
	if !ok {
		t.Fatalf("expected pkg-7zip to be installed")
	}
	if rec.Version != "23.01" {
		t.Fatalf("expected version 23.01, got %s", rec.Version)
	}

	recByName, ok := store.GetInstalledByName("7-zip")
	if !ok {
		t.Fatalf("expected 7-zip by name to be installed")
	}
	if recByName.Version != "23.01" {
		t.Fatalf("expected version 23.01, got %s", recByName.Version)
	}

	// Verify persistence by reloading
	store2, err := NewInstalledStore(storePath)
	if err != nil {
		t.Fatalf("NewInstalledStore reload failed: %v", err)
	}
	recReloaded, ok := store2.GetInstalled("pkg-7zip")
	if !ok || recReloaded.Version != "23.01" {
		t.Fatalf("reloaded store missing or wrong version")
	}

	// Verify DetectInstalledVersion
	detected := DetectInstalledVersion(pkg, store2)
	if detected != "23.01" {
		t.Fatalf("expected detected version 23.01, got %s", detected)
	}

	pkgUnknown := PackageItem{
		ID:   "unknown",
		Name: "Unknown App",
	}
	detectedUnknown := DetectInstalledVersion(pkgUnknown, store2)
	if detectedUnknown != "" {
		t.Fatalf("expected empty detected version for unknown app, got %s", detectedUnknown)
	}

	_ = os.Remove(storePath)
}

func TestDetectInstalledVersion_S17(t *testing.T) {
	// Backup original scan function and restore after test
	origScan := scanRegistryInstalled
	defer func() {
		scanRegistryInstalled = origScan
	}()

	mockRegistry := map[string]string{
		"go":                              "1.21.4",
		"7":                               "7.0.0",
		"google chrome":                   "120.0.6099.109",
		"7-zip 23.01 (x64 edition)":       "23.01",
		"mozilla firefox (x64 pl)":        "121.0",
	}

	scanRegistryInstalled = func() (map[string]string, error) {
		return mockRegistry, nil
	}

	// 1. Package "Google Chrome" should NOT match "go"
	// Before S-17: "google chrome" contained "go", causing false positive match to "go" (1.21.4)
	chromePkg := PackageItem{
		ID:   "google-chrome",
		Name: "Google Chrome",
	}
	chromeVer := DetectInstalledVersion(chromePkg, nil)
	assert.Equal(t, "120.0.6099.109", chromeVer)

	// A package named "Go Programming" should NOT match registry key "go" through substring
	// because cleanPkgName len >= 4 and strings.Contains("go", "go programming") is false
	goPkg := PackageItem{
		ID:   "pkg-golang",
		Name: "Go Programming Language",
	}
	goVer := DetectInstalledVersion(goPkg, nil)
	assert.Empty(t, goVer, "package name longer than registry app name must not match falsely")

	// 2. Package "7-Zip" should NOT match single character "7"
	// Before S-17: single char or reverse substring might match
	sevenZipPkg := PackageItem{
		ID:   "org.7zip.7zip",
		Name: "7-Zip",
	}
	sevenZipVer := DetectInstalledVersion(sevenZipPkg, nil)
	assert.Equal(t, "23.01", sevenZipVer, "should match '7-zip 23.01 (x64 edition)' but not '7'")

	// 3. Short package name (< 4 chars) without direct match should not do fuzzy matching
	shortPkg := PackageItem{
		ID:   "pkg-git",
		Name: "Git",
	}
	// Add an entry in registry that contains "git" like "digital signature tool"
	mockRegistry["digital signature tool"] = "1.0.0"
	gitVer := DetectInstalledVersion(shortPkg, nil)
	assert.Empty(t, gitVer, "short package name (< 4 chars) must not match via substring")

	// 4. Proper substring match where registry app contains full package name (>= 4 chars)
	firefoxPkg := PackageItem{
		ID:   "mozilla.firefox",
		Name: "Mozilla Firefox",
	}
	firefoxVer := DetectInstalledVersion(firefoxPkg, nil)
	assert.Equal(t, "121.0", firefoxVer)
}

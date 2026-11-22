package selfservice

import (
	"os"
	"path/filepath"
	"testing"
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

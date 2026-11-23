package selfservice

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractZip unpacks a zip archive into destDir with strict protection against Zip Slip attacks.
func ExtractZip(srcZip string, destDir string) error {
	r, err := zip.OpenReader(srcZip)
	if err != nil {
		return fmt.Errorf("failed to open zip file %s: %w", srcZip, err)
	}
	defer r.Close()

	cleanDestDir := filepath.Clean(destDir)
	if err := os.MkdirAll(cleanDestDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	for _, f := range r.File {
		targetPath := filepath.Clean(filepath.Join(cleanDestDir, f.Name))

		// Strict Zip Slip vulnerability check
		if !strings.HasPrefix(targetPath, cleanDestDir+string(filepath.Separator)) && targetPath != cleanDestDir {
			return fmt.Errorf("illegal file path in zip archive (Zip Slip vulnerability): %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
			continue
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory for %s: %w", targetPath, err)
		}

		// Extract regular file
		if err := extractZipFile(f, targetPath); err != nil {
			return err
		}
	}

	return nil
}

func extractZipFile(f *zip.File, targetPath string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("failed to read zip entry %s: %w", f.Name, err)
	}
	defer rc.Close()

	outFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", targetPath, err)
	}
	defer outFile.Close()

	// Limit copy size to prevent zip bombs (e.g. max 5GB per file)
	limitedReader := io.LimitReader(rc, 5*1024*1024*1024)
	if _, err := io.Copy(outFile, limitedReader); err != nil {
		return fmt.Errorf("failed to extract file %s: %w", targetPath, err)
	}

	return nil
}

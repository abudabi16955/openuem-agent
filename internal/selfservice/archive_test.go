package selfservice

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractZip_Success(t *testing.T) {
	tempDir := t.TempDir()
	zipPath := filepath.Join(tempDir, "test.zip")
	destDir := filepath.Join(tempDir, "extracted")

	// Create valid zip archive
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	files := map[string]string{
		"file1.txt":            "Hello World",
		"sub/file2.txt":        "Subdir content",
		"sub/nested/file3.txt": "Nested content",
	}

	for name, content := range files {
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	require.NoError(t, os.WriteFile(zipPath, buf.Bytes(), 0644))

	// Extract
	err := ExtractZip(zipPath, destDir)
	require.NoError(t, err)

	for name, content := range files {
		fullPath := filepath.Join(destDir, filepath.FromSlash(name))
		data, err := os.ReadFile(fullPath)
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
	}
}

func TestExtractZip_ZipSlipProtection(t *testing.T) {
	tempDir := t.TempDir()
	destDir := filepath.Join(tempDir, "extracted")
	require.NoError(t, os.MkdirAll(destDir, 0755))

	evilPaths := []string{
		"../evil.txt",
		"../../evil.txt",
		"sub/../../evil.txt",
		"nested/../../../../tmp/evil.txt",
	}

	for _, evilPath := range evilPaths {
		t.Run("slip_"+evilPath, func(t *testing.T) {
			var buf bytes.Buffer
			w := zip.NewWriter(&buf)
			f, err := w.Create(evilPath)
			require.NoError(t, err)
			_, err = f.Write([]byte("malicious content"))
			require.NoError(t, err)
			require.NoError(t, w.Close())

			zipFile := filepath.Join(tempDir, "malicious.zip")
			require.NoError(t, os.WriteFile(zipFile, buf.Bytes(), 0644))

			err = ExtractZip(zipFile, destDir)
			require.Error(t, err, "Zip Slip attempt must return an error")
			assert.True(t, strings.Contains(err.Error(), "Zip Slip"), "error message must mention Zip Slip")

			// Ensure file was not extracted outside destDir
			outsideFile := filepath.Join(tempDir, "evil.txt")
			_, statErr := os.Stat(outsideFile)
			assert.True(t, os.IsNotExist(statErr), "evil file must not exist outside destDir")
		})
	}
}

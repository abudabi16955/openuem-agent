package selfservice

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIPCDispatcher_SecurityValidation(t *testing.T) {
	tempDir := t.TempDir()
	cm, err := NewCatalogManager(tempDir)
	require.NoError(t, err)

	exec := &mockExecutor{}
	rep := &mockReporter{}
	q := NewInstallationQueue(nil, exec, rep, "agent-test", "")
	defer q.Stop()

	// Seed catalog with one enabled package and one disabled package
	validPkg := PackageItem{
		ID:          "valid-uuid-1",
		Name:        "Authorized Package",
		PackageType: "winget",
		Enabled:     true,
	}
	disabledPkg := PackageItem{
		ID:          "disabled-uuid-2",
		Name:        "Disabled Package",
		PackageType: "winget",
		Enabled:     false,
	}
	require.NoError(t, cm.UpdatePackages([]PackageItem{validPkg, disabledPkg}))

	dispatcher := NewIPCDispatcher(cm, q)

	// 1. Test get_catalog
	resp := dispatcher.Dispatch(IPCRequest{Action: "get_catalog"})
	assert.True(t, resp.Success)
	require.NotNil(t, resp.Catalog)
	assert.Len(t, resp.Catalog.Packages, 2)

	// 2. Test install of authorized enabled package -> SUCCESS (with token)
	testToken := uintptr(98765)
	resp = dispatcher.Dispatch(IPCRequest{
		Action:    "install",
		PackageID: "valid-uuid-1",
	}, testToken)
	assert.True(t, resp.Success, "authorized package should succeed")
	assert.NotEmpty(t, resp.Task.ID)
	assert.Equal(t, testToken, resp.Task.UserToken)

	// 3. Test install of arbitrary/unauthorized UUID or path -> REJECTED
	resp = dispatcher.Dispatch(IPCRequest{
		Action:    "install",
		PackageID: "arbitrary-uuid-or-path; calc.exe",
	}, uintptr(11111))
	assert.False(t, resp.Success, "arbitrary package ID must be rejected")
	assert.Contains(t, resp.Error, "not found in authorized catalog")

	// 4. Test install of disabled package -> REJECTED
	resp = dispatcher.Dispatch(IPCRequest{
		Action:    "install",
		PackageID: "disabled-uuid-2",
	}, uintptr(22222))
	assert.False(t, resp.Success, "disabled package must be rejected")
	assert.Contains(t, resp.Error, "disabled")

	// 5. Test get_status
	resp = dispatcher.Dispatch(IPCRequest{
		Action:    "get_status",
		PackageID: "valid-uuid-1",
	}, uintptr(33333))
	assert.True(t, resp.Success)
	assert.NotEmpty(t, resp.Status)
}

func TestIPCServer_EndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	cm, err := NewCatalogManager(tempDir)
	require.NoError(t, err)

	exec := &mockExecutor{}
	rep := &mockReporter{}
	q := NewInstallationQueue(nil, exec, rep, "agent-test", "")
	defer q.Stop()

	require.NoError(t, cm.UpdatePackages([]PackageItem{
		{
			ID:          "pkg-e2e",
			Name:        "End-to-End Test",
			PackageType: "winget",
			Enabled:     true,
		},
	}))

	dispatcher := NewIPCDispatcher(cm, q)
	socketOrPipe := filepath.Join(tempDir, "test-ipc.sock")
	server := NewIPCServer(socketOrPipe, dispatcher)
	require.NoError(t, server.Start())
	defer server.Stop()

	// Wait briefly for server listener
	time.Sleep(50 * time.Millisecond)

	// Call get_catalog via client
	resp, err := CallIPC(socketOrPipe, IPCRequest{Action: "get_catalog"})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	require.NotNil(t, resp.Catalog)
	assert.Len(t, resp.Catalog.Packages, 1)

	// Call install via client
	resp, err = CallIPC(socketOrPipe, IPCRequest{
		Action:    "install",
		PackageID: "pkg-e2e",
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, "pkg-e2e", resp.Task.PackageID)
}
func TestNamedPipeConstants(t *testing.T) {
	assert.Equal(t, `\\.\pipe\OpenUEM_SelfService_IPC`, DefaultNamedPipeName)
	assert.Equal(t, `\\.\pipe\openuem-selfservice`, LegacyNamedPipeName)
}

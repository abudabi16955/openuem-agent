package selfservice

import (
	"fmt"
	"sync"
	"testing"
	"time"

	openuem_nats "github.com/open-uem/nats"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockExecutor struct {
	mu           sync.Mutex
	executedList []string
	delay        time.Duration
	failPkgID    string
}

func (m *mockExecutor) Execute(pkg PackageItem, localPath string) (string, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	m.mu.Lock()
	m.executedList = append(m.executedList, pkg.ID)
	m.mu.Unlock()

	if m.failPkgID == pkg.ID {
		return "", fmt.Errorf("mock execution error for %s", pkg.ID)
	}
	return "mock installation successful", nil
}

type mockReporter struct {
	mu      sync.Mutex
	actions []openuem_nats.DeployAction
}

func (r *mockReporter) ReportDeployResult(action openuem_nats.DeployAction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions = append(r.actions, action)
	return nil
}

func TestInstallationQueue_FIFOOrder(t *testing.T) {
	exec := &mockExecutor{delay: 20 * time.Millisecond}
	rep := &mockReporter{}

	// winget packages skip downloader
	q := NewInstallationQueue(nil, exec, rep, "agent-123", "")
	defer q.Stop()

	pkg1 := PackageItem{ID: "pkg-1", Name: "App 1", PackageType: "winget"}
	pkg2 := PackageItem{ID: "pkg-2", Name: "App 2", PackageType: "winget"}
	pkg3 := PackageItem{ID: "pkg-3", Name: "App 3", PackageType: "winget"}

	t1, err := q.Enqueue(pkg1)
	require.NoError(t, err)
	t2, err := q.Enqueue(pkg2)
	require.NoError(t, err)
	t3, err := q.Enqueue(pkg3)
	require.NoError(t, err)

	assert.Equal(t, StatusQueued, t1.Status)
	assert.Equal(t, StatusQueued, t2.Status)
	assert.Equal(t, StatusQueued, t3.Status)

	// Wait for queue to process all 3 items
	require.Eventually(t, func() bool {
		st1 := q.GetStatusByPackageID(pkg1.ID)
		st2 := q.GetStatusByPackageID(pkg2.ID)
		st3 := q.GetStatusByPackageID(pkg3.ID)
		return st1 == StatusInstalled && st2 == StatusInstalled && st3 == StatusInstalled
	}, 2*time.Second, 20*time.Millisecond)

	exec.mu.Lock()
	assert.Equal(t, []string{"pkg-1", "pkg-2", "pkg-3"}, exec.executedList, "tasks must be processed in strict FIFO order")
	exec.mu.Unlock()

	rep.mu.Lock()
	assert.Len(t, rep.actions, 3)
	assert.False(t, rep.actions[0].Failed)
	assert.Equal(t, "pkg-1", rep.actions[0].PackageId)
	rep.mu.Unlock()
}

func TestInstallationQueue_PreventDuplicateActiveInstall(t *testing.T) {
	exec := &mockExecutor{delay: 100 * time.Millisecond}
	rep := &mockReporter{}

	q := NewInstallationQueue(nil, exec, rep, "agent-123", "")
	defer q.Stop()

	pkg := PackageItem{ID: "pkg-dup", Name: "App Duplicate", PackageType: "winget"}

	_, err := q.Enqueue(pkg)
	require.NoError(t, err)

	// Second enqueue of same package while in progress should fail
	_, err = q.Enqueue(pkg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already")
}

func TestInstallationQueue_HandleFailure(t *testing.T) {
	exec := &mockExecutor{failPkgID: "pkg-fail"}
	rep := &mockReporter{}

	q := NewInstallationQueue(nil, exec, rep, "agent-123", "")
	defer q.Stop()

	pkg := PackageItem{ID: "pkg-fail", Name: "Failing App", PackageType: "winget"}
	task, err := q.Enqueue(pkg)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		tInfo, found := q.GetTask(task.ID)
		return found && tInfo.Status == StatusFailed
	}, 1*time.Second, 20*time.Millisecond)

	tInfo, _ := q.GetTask(task.ID)
	assert.Contains(t, tInfo.Error, "mock execution error")

	rep.mu.Lock()
	require.Len(t, rep.actions, 1)
	assert.True(t, rep.actions[0].Failed)
	rep.mu.Unlock()
}

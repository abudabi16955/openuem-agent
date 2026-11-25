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
	lastToken    uintptr
}

func (m *mockExecutor) Execute(pkg PackageItem, localPath string, userTokens ...uintptr) (string, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	m.mu.Lock()
	m.executedList = append(m.executedList, pkg.ID)
	if len(userTokens) > 0 {
		m.lastToken = userTokens[0]
	}
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

func TestInstallationQueue_SequentialFIFO(t *testing.T) {
	exec := &mockExecutor{delay: 20 * time.Millisecond}
	rep := &mockReporter{}

	q := NewInstallationQueue(nil, exec, rep, "agent-test", "")
	defer q.Stop()

	pkg1 := PackageItem{ID: "pkg-1", Name: "Package 1", PackageType: "winget"}
	pkg2 := PackageItem{ID: "pkg-2", Name: "Package 2", PackageType: "winget"}
	pkg3 := PackageItem{ID: "pkg-3", Name: "Package 3", PackageType: "winget"}

	t1, err1 := q.Enqueue(pkg1)
	require.NoError(t, err1)
	assert.NotEmpty(t, t1.ID)

	t2, err2 := q.Enqueue(pkg2)
	require.NoError(t, err2)
	assert.NotEmpty(t, t2.ID)

	t3, err3 := q.Enqueue(pkg3)
	require.NoError(t, err3)
	assert.NotEmpty(t, t3.ID)

	// Wait for queue processing to complete
	require.Eventually(t, func() bool {
		st1 := q.GetStatusByPackageID("pkg-1")
		st2 := q.GetStatusByPackageID("pkg-2")
		st3 := q.GetStatusByPackageID("pkg-3")
		return st1 == StatusInstalled && st2 == StatusInstalled && st3 == StatusInstalled
	}, 3*time.Second, 20*time.Millisecond)

	exec.mu.Lock()
	defer exec.mu.Unlock()
	assert.Equal(t, []string{"pkg-1", "pkg-2", "pkg-3"}, exec.executedList)

	rep.mu.Lock()
	defer rep.mu.Unlock()
	assert.Len(t, rep.actions, 3)
	assert.Equal(t, "pkg-1", rep.actions[0].PackageId)
	assert.Equal(t, "pkg-2", rep.actions[1].PackageId)
	assert.Equal(t, "pkg-3", rep.actions[2].PackageId)
}

func TestInstallationQueue_PreventDuplicates(t *testing.T) {
	exec := &mockExecutor{delay: 100 * time.Millisecond}
	rep := &mockReporter{}

	q := NewInstallationQueue(nil, exec, rep, "agent-test", "")
	defer q.Stop()

	pkg := PackageItem{ID: "pkg-dup", Name: "Duplicate Test", PackageType: "winget"}

	t1, err := q.Enqueue(pkg)
	require.NoError(t, err)
	assert.NotNil(t, t1)

	// Enqueue again immediately while first is queued/in-progress
	t2, err := q.Enqueue(pkg)
	assert.Error(t, err)
	assert.Equal(t, t1.ID, t2.ID)
	assert.Contains(t, err.Error(), "already")
}

func TestInstallationQueue_FailureHandling(t *testing.T) {
	exec := &mockExecutor{failPkgID: "pkg-fail"}
	rep := &mockReporter{}

	q := NewInstallationQueue(nil, exec, rep, "agent-test", "")
	defer q.Stop()

	pkg := PackageItem{ID: "pkg-fail", Name: "Failing Package", PackageType: "winget"}

	t1, err := q.Enqueue(pkg)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		task, found := q.GetTask(t1.ID)
		return found && task.Status == StatusFailed
	}, 2*time.Second, 20*time.Millisecond)

	task, _ := q.GetTask(t1.ID)
	assert.Equal(t, StatusFailed, task.Status)
	assert.Contains(t, task.Error, "mock execution error")

	rep.mu.Lock()
	defer rep.mu.Unlock()
	require.Len(t, rep.actions, 1)
	assert.True(t, rep.actions[0].Failed)
}

func TestInstallationQueue_UserTokenPassed(t *testing.T) {
	exec := &mockExecutor{}
	rep := &mockReporter{}

	q := NewInstallationQueue(nil, exec, rep, "agent-test", "")
	defer q.Stop()

	pkg := PackageItem{ID: "pkg-token", Name: "Token Package", PackageType: "winget"}
	dummyToken := uintptr(12345)

	t1, err := q.Enqueue(pkg, dummyToken)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		task, found := q.GetTask(t1.ID)
		return found && task.Status == StatusInstalled
	}, 2*time.Second, 20*time.Millisecond)

	exec.mu.Lock()
	defer exec.mu.Unlock()
	assert.Equal(t, dummyToken, exec.lastToken)
}

func TestInstallationQueue_RecordInstalledBeforeStatusInstalled(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewInstalledStore(tempDir + "/installed_packages.json")
	require.NoError(t, err)
	exec := &mockExecutor{}
	rep := &mockReporter{}

	q := NewInstallationQueue(nil, exec, rep, "agent-test", "", store)
	defer q.Stop()

	pkg := PackageItem{ID: "pkg-race-check", Name: "Race Check Package", Version: "1.2.3", PackageType: "winget"}

	t1, err := q.Enqueue(pkg)
	require.NoError(t, err)

	var recordedBeforeStatus bool
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, found := q.GetTask(t1.ID)
		if found && task.Status == StatusInstalled {
			_, recordedBeforeStatus = store.GetInstalled(pkg.ID)
			break
		}
		time.Sleep(500 * time.Microsecond)
	}

	assert.True(t, recordedBeforeStatus, "Package must be recorded in installedStore BEFORE status becomes StatusInstalled")
}

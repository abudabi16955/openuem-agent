package selfservice

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	openuem_nats "github.com/open-uem/nats"
)

// JobStatus represents the state of a package installation task.
type JobStatus string

const (
	StatusAvailable   JobStatus = "available"
	StatusQueued      JobStatus = "queued"
	StatusDownloading JobStatus = "downloading"
	StatusInstalling  JobStatus = "installing"
	StatusInstalled   JobStatus = "installed"
	StatusFailed      JobStatus = "failed"
)

// InstallationTask describes an installation job in the queue.
type InstallationTask struct {
	ID         string      `json:"id"`
	PackageID  string      `json:"package_id"`
	Package    PackageItem `json:"package"`
	Status     JobStatus   `json:"status"`
	Error      string      `json:"error,omitempty"`
	Output     string      `json:"output,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	StartedAt  *time.Time  `json:"started_at,omitempty"`
	FinishedAt *time.Time  `json:"finished_at,omitempty"`
}

// InstallationQueue coordinates sequential FIFO processing of package installations.
type InstallationQueue struct {
	mu             sync.RWMutex
	tasks          map[string]*InstallationTask
	packageTasks   map[string]*InstallationTask
	queueChan      chan *InstallationTask
	downloader     *Downloader
	executor       Executor
	reporter       DeployReporter
	agentID        string
	consoleBaseURL string
	quitChan       chan struct{}
	wg             sync.WaitGroup
	msiTimeout     time.Duration
}

// NewInstallationQueue creates a new FIFO installation queue.
func NewInstallationQueue(
	downloader *Downloader,
	executor Executor,
	reporter DeployReporter,
	agentID string,
	consoleBaseURL string,
) *InstallationQueue {
	if executor == nil {
		executor = NewWindowsExecutor(30 * time.Minute)
	}

	q := &InstallationQueue{
		tasks:          make(map[string]*InstallationTask),
		packageTasks:   make(map[string]*InstallationTask),
		queueChan:      make(chan *InstallationTask, 100),
		downloader:     downloader,
		executor:       executor,
		reporter:       reporter,
		agentID:        agentID,
		consoleBaseURL: consoleBaseURL,
		quitChan:       make(chan struct{}),
		msiTimeout:     10 * time.Minute,
	}

	q.wg.Add(1)
	go q.workerLoop()

	return q
}

// Enqueue adds a package installation request to the FIFO queue.
func (q *InstallationQueue) Enqueue(pkg PackageItem) (*InstallationTask, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Prevent duplicate active installs of the same package
	if existing, exists := q.packageTasks[pkg.ID]; exists {
		if existing.Status == StatusQueued ||
			existing.Status == StatusDownloading ||
			existing.Status == StatusInstalling {
			return existing, fmt.Errorf("package %s is already %s", pkg.Name, existing.Status)
		}
	}

	task := &InstallationTask{
		ID:        uuid.New().String(),
		PackageID: pkg.ID,
		Package:   pkg,
		Status:    StatusQueued,
		CreatedAt: time.Now(),
	}

	q.tasks[task.ID] = task
	q.packageTasks[pkg.ID] = task

	select {
	case q.queueChan <- task:
		log.Printf("[INFO]: enqueued package %s (%s) for installation [TaskID: %s]", pkg.Name, pkg.ID, task.ID)
		return task, nil
	default:
		task.Status = StatusFailed
		task.Error = "installation queue is full"
		return nil, fmt.Errorf("installation queue is full")
	}
}

// GetTask returns a task by its ID.
func (q *InstallationQueue) GetTask(taskID string) (*InstallationTask, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	task, exists := q.tasks[taskID]
	if !exists {
		return nil, false
	}
	cp := *task
	return &cp, true
}

// GetStatusByPackageID returns the latest status for a package.
func (q *InstallationQueue) GetStatusByPackageID(packageID string) JobStatus {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if task, exists := q.packageTasks[packageID]; exists {
		return task.Status
	}
	return StatusAvailable
}

// GetAllTasks returns a list of all known installation tasks.
func (q *InstallationQueue) GetAllTasks() []InstallationTask {
	q.mu.RLock()
	defer q.mu.RUnlock()

	list := make([]InstallationTask, 0, len(q.tasks))
	for _, t := range q.tasks {
		list = append(list, *t)
	}
	return list
}

// Stop shuts down the installation queue and waits for the worker to finish.
func (q *InstallationQueue) Stop() {
	close(q.quitChan)
	q.wg.Wait()
}

func (q *InstallationQueue) workerLoop() {
	defer q.wg.Done()

	for {
		select {
		case <-q.quitChan:
			return
		case task, ok := <-q.queueChan:
			if !ok {
				return
			}
			q.processTask(task)
		}
	}
}

func (q *InstallationQueue) processTask(task *InstallationTask) {
	now := time.Now()
	q.mu.Lock()
	task.StartedAt = &now
	task.Status = StatusDownloading
	q.mu.Unlock()

	var localFilePath string
	var err error

	// Download installer if not a winget package
	if !strings.EqualFold(task.Package.PackageType, "winget") {
		if q.downloader != nil {
			localFilePath, err = q.downloader.Download(task.Package, q.consoleBaseURL)
			if err != nil {
				q.finishTask(task, StatusFailed, "", fmt.Sprintf("download failed: %v", err))
				return
			}
		} else {
			q.finishTask(task, StatusFailed, "", "downloader not configured")
			return
		}
	}

	// For MSI packages, acquire Global\_MSIExecute mutex to avoid error 1618
	if strings.EqualFold(task.Package.PackageType, "msi") {
		releaseMutex, err := WaitForMSIExecuteMutex(q.msiTimeout)
		if err != nil {
			q.finishTask(task, StatusFailed, "", fmt.Sprintf("waiting for Windows Installer mutex failed: %v", err))
			return
		}
		defer releaseMutex()
	}

	q.mu.Lock()
	task.Status = StatusInstalling
	q.mu.Unlock()

	log.Printf("[INFO]: executing installation for package %s (%s)", task.Package.Name, task.Package.PackageType)
	output, err := q.executor.Execute(task.Package, localFilePath)
	if err != nil {
		log.Printf("[ERROR]: installation failed for package %s: %v", task.Package.Name, err)
		q.finishTask(task, StatusFailed, output, err.Error())
		return
	}

	log.Printf("[INFO]: installation succeeded for package %s", task.Package.Name)
	q.finishTask(task, StatusInstalled, output, "")
}

func (q *InstallationQueue) finishTask(task *InstallationTask, status JobStatus, output, errMsg string) {
	finished := time.Now()
	q.mu.Lock()
	task.Status = status
	task.Output = output
	task.Error = errMsg
	task.FinishedAt = &finished
	taskCopy := *task
	q.mu.Unlock()

	// Report deployment result to OpenUEM via NATS
	if q.reporter != nil {
		action := openuem_nats.DeployAction{
			AgentId:        q.agentID,
			Action:         "install",
			PackageId:      taskCopy.Package.ID,
			PackageName:    taskCopy.Package.Name,
			PackageVersion: taskCopy.Package.Version,
			When:           finished,
			Failed:         status == StatusFailed,
			Info:           output,
		}
		if errMsg != "" {
			if action.Info != "" {
				action.Info += "\n"
			}
			action.Info += errMsg
		}

		if err := q.reporter.ReportDeployResult(action); err != nil {
			log.Printf("[ERROR]: could not report deploy result for task %s: %v", taskCopy.ID, err)
		}
	}
}

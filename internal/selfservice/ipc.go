package selfservice

import (
	"encoding/json"
	"fmt"
	"log"
)

// DefaultNamedPipeName is the well-known Windows Named Pipe for OpenUEM Self-Service Portal.
const DefaultNamedPipeName = `\\.\pipe\openuem-selfservice`

// IPCRequest represents a request sent across the named pipe.
type IPCRequest struct {
	Action    string `json:"action"` // "get_catalog", "install", "get_status"
	PackageID string `json:"package_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
}

// IPCResponse represents the response returned across the named pipe.
type IPCResponse struct {
	Success bool               `json:"success"`
	Error   string             `json:"error,omitempty"`
	Catalog *Catalog           `json:"catalog,omitempty"`
	Task    *InstallationTask  `json:"task,omitempty"`
	Tasks   []InstallationTask `json:"tasks,omitempty"`
	Status  JobStatus          `json:"status,omitempty"`
}

// IPCDispatcher handles and validates incoming IPC requests.
type IPCDispatcher struct {
	catalogMgr     *CatalogManager
	queue          *InstallationQueue
	installedStore *InstalledStore
}

// NewIPCDispatcher creates a new IPC dispatcher.
func NewIPCDispatcher(catalogMgr *CatalogManager, queue *InstallationQueue, store ...*InstalledStore) *IPCDispatcher {
	var s *InstalledStore
	if len(store) > 0 {
		s = store[0]
	}

	return &IPCDispatcher{
		catalogMgr:     catalogMgr,
		queue:          queue,
		installedStore: s,
	}
}

// HandleMessage parses and executes an IPC request, producing an IPCResponse.
func (d *IPCDispatcher) HandleMessage(msg []byte) []byte {
	var req IPCRequest
	if err := json.Unmarshal(msg, &req); err != nil {
		return d.errorResponse(fmt.Sprintf("invalid IPC request json: %v", err))
	}

	resp := d.Dispatch(req)
	data, err := json.Marshal(resp)
	if err != nil {
		return d.errorResponse(fmt.Sprintf("failed to marshal IPC response: %v", err))
	}
	return data
}

// Dispatch executes the request with strict validation.
func (d *IPCDispatcher) Dispatch(req IPCRequest) IPCResponse {
	switch req.Action {
	case "get_catalog":
		cat := d.catalogMgr.GetCatalog()
		for i := range cat.Packages {
			cat.Packages[i].InstalledVersion = DetectInstalledVersion(cat.Packages[i], d.installedStore)
		}
		return IPCResponse{
			Success: true,
			Catalog: &cat,
		}

	case "install":
		// CRITICAL SECURITY ENFORCEMENT:
		// Installation accepts EXCLUSIVELY ActionID/UUID present in authorized catalog.json.
		// Arbitrary paths and command injection from unprivileged users are strictly disallowed.
		if req.PackageID == "" {
			return IPCResponse{Success: false, Error: "package_id is required"}
		}

		pkg, found := d.catalogMgr.FindPackage(req.PackageID)
		if !found {
			return IPCResponse{
				Success: false,
				Error:   fmt.Sprintf("package %s is not found in authorized catalog", req.PackageID),
			}
		}

		if !pkg.Enabled {
			return IPCResponse{
				Success: false,
				Error:   fmt.Sprintf("package %s is disabled in catalog", pkg.Name),
			}
		}

		task, err := d.queue.Enqueue(*pkg)
		if err != nil {
			return IPCResponse{
				Success: false,
				Error:   err.Error(),
			}
		}

		return IPCResponse{
			Success: true,
			Task:    task,
		}

	case "get_status":
		if req.TaskID != "" {
			task, found := d.queue.GetTask(req.TaskID)
			if !found {
				return IPCResponse{Success: false, Error: "task not found"}
			}
			return IPCResponse{Success: true, Task: task}
		}

		if req.PackageID != "" {
			st := d.queue.GetStatusByPackageID(req.PackageID)
			return IPCResponse{Success: true, Status: st}
		}

		tasks := d.queue.GetAllTasks()
		return IPCResponse{Success: true, Tasks: tasks}

	default:
		log.Printf("[WARN]: unknown IPC action requested: %s", req.Action)
		return IPCResponse{
			Success: false,
			Error:   fmt.Sprintf("unknown action: %s", req.Action),
		}
	}
}

func (d *IPCDispatcher) errorResponse(err string) []byte {
	resp := IPCResponse{Success: false, Error: err}
	data, _ := json.Marshal(resp)
	return data
}

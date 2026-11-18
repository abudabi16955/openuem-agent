package selfservice

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	openuem_nats "github.com/open-uem/nats"
)

// DeployReporter reports deployment results back to OpenUEM server.
type DeployReporter interface {
	ReportDeployResult(action openuem_nats.DeployAction) error
}

// NATSReporter sends deployment results over NATS connection.
type NATSReporter struct {
	natsConn      *nats.Conn
	refreshReport func()
}

// NewNATSReporter initializes a new NATSReporter.
func NewNATSReporter(natsConn *nats.Conn, refreshReport func()) *NATSReporter {
	return &NATSReporter{
		natsConn:      natsConn,
		refreshReport: refreshReport,
	}
}

// ReportDeployResult marshals and sends DeployAction to 'deployresult' subject.
func (r *NATSReporter) ReportDeployResult(action openuem_nats.DeployAction) error {
	if r.natsConn == nil || !r.natsConn.IsConnected() {
		log.Println("[WARN]: NATS connection is nil or not connected, cannot report deploy result immediately")
		return fmt.Errorf("NATS connection is not ready")
	}

	data, err := json.Marshal(action)
	if err != nil {
		return fmt.Errorf("failed to marshal deploy action: %w", err)
	}

	response, err := r.natsConn.Request("deployresult", data, 2*time.Minute)
	if err != nil {
		return fmt.Errorf("NATS deployresult request failed: %w", err)
	}

	if len(response.Data) > 0 {
		respStr := string(response.Data)
		if respStr != "" && respStr != "ok" && respStr != "OK" {
			log.Printf("[WARN]: server response to deployresult: %s", respStr)
		}
	}

	// Trigger agent report run to refresh installed software list on success
	if !action.Failed && r.refreshReport != nil {
		go r.refreshReport()
	}

	return nil
}

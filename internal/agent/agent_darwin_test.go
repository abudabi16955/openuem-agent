//go:build darwin

package agent

import (
	"path/filepath"
	"testing"

	"github.com/open-uem/openuem-agent/internal/agent/dsc"
)

func TestApplyConfigurationDarwinAnsibleFailureGuaranteedTaskReport(t *testing.T) {
	t.Setenv("OPENUEM_PLAYBOOKS_DIR", t.TempDir())

	a := &Agent{}
	tmpDir := t.TempDir()
	taskControlPath := filepath.Join(tmpDir, "task_control.json")
	taskControl := &dsc.TaskControl{}

	playbookYAML := []byte(`
- hosts: 127.0.0.1
  tasks:
    - name: "failing_task"
      command: /bin/false
`)

	reports, err := a.ApplyConfiguration(888, playbookYAML, taskControl, taskControlPath, 5)
	if err != nil {
		t.Fatalf("expected ApplyConfiguration to not return error on process execution failure, got: %v", err)
	}

	if len(reports) == 0 {
		t.Fatal("expected at least one TaskReport to be returned on execution failure, got none")
	}

	report := reports[0]
	if !report.Failed {
		t.Errorf("expected task report to have Failed=true, got: %+v", report)
	}
	if report.Timeout {
		t.Errorf("expected task report to have Timeout=false for process failure, got: %+v", report)
	}
	if report.StdErr == "" {
		t.Errorf("expected task report to have non-empty StdErr, got: %+v", report)
	}
	if report.EndTime == "" {
		t.Errorf("expected task report to have non-empty EndTime, got: %+v", report)
	}
	if report.Name != "profile_888" {
		t.Errorf("expected task report Name profile_888, got: %s", report.Name)
	}
}

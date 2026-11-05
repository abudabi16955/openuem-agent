//go:build darwin

package agent

import (
	"os"
	"path/filepath"
	"strings"
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

func TestApplyConfigurationDarwinProcessStderrFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENUEM_PLAYBOOKS_DIR", dir)

	executable := filepath.Join(dir, "ansible-playbook")
	script := "#!/bin/sh\necho darwin-process-stderr >&2\nexit 1\n"
	if err := os.WriteFile(executable, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENUEM_ANSIBLE_PLAYBOOK_BIN", executable)

	a := &Agent{}
	taskControl := &dsc.TaskControl{}
	taskControlPath := filepath.Join(dir, "task_control.json")

	playbookYAML := []byte("- hosts: 127.0.0.1\n  tasks: []\n")
	reports, err := a.ApplyConfiguration(777, playbookYAML, taskControl, taskControlPath, 5)
	if err != nil {
		t.Fatalf("expected nil error from ApplyConfiguration, got: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("expected 1 task report, got %d", len(reports))
	}
	report := reports[0]
	if !report.Failed || report.EndTime == "" {
		t.Fatalf("terminal fields missing: %+v", report)
	}
	if !strings.Contains(report.StdErr, "darwin-process-stderr") {
		t.Fatalf("expected StdErr to contain 'darwin-process-stderr', got: %q", report.StdErr)
	}
}

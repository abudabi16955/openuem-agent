//go:build linux

package agent

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/open-uem/openuem-agent/internal/agent/dsc"
)

func TestApplyConfigurationAnsibleExecuteErrorGuaranteedReport(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("OPENUEM_PLAYBOOKS_DIR", tmpDir)

	a := &Agent{}
	a.Config.UUID = "test-agent-uuid"

	taskControl := &dsc.TaskControl{
		ProfilesRunning: make(map[string]time.Time),
	}
	taskControlPath := filepath.Join(tmpDir, "tasks.json")

	playbookContent := []byte("- hosts: 127.0.0.1\n  tasks:\n  - name: dummy\n    debug:\n      msg: hi\n")

	profileID := 42
	tasks, err := a.ApplyConfiguration(profileID, playbookContent, taskControl, taskControlPath)
	if err != nil {
		t.Fatalf("expected nil error from ApplyConfiguration, got: %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task report, got %d", len(tasks))
	}

	report := tasks[0]
	expectedName := "profile_42"
	if report.Name != expectedName {
		t.Errorf("expected report.Name %q, got %q", expectedName, report.Name)
	}

	if !report.Failed {
		t.Errorf("expected report.Failed == true, got false")
	}

	if report.Timeout {
		t.Errorf("expected report.Timeout == false, got true")
	}

	if report.StdErr == "" {
		t.Errorf("expected non-empty StdErr in TaskReport")
	}

	if report.StartTime == "" {
		t.Errorf("expected non-empty StartTime in TaskReport")
	}

	if report.EndTime == "" {
		t.Errorf("expected non-empty EndTime in TaskReport")
	}
}

func TestApplyConfigurationInvalidYAMLReturnsError(t *testing.T) {
	a := &Agent{}
	taskControl := &dsc.TaskControl{
		ProfilesRunning: make(map[string]time.Time),
	}
	taskControlPath := filepath.Join(t.TempDir(), "tasks.json")

	invalidYAML := []byte("invalid:\n  - yaml: [unclosed")

	_, err := a.ApplyConfiguration(99, invalidYAML, taskControl, taskControlPath)
	if err == nil {
		t.Errorf("expected error for unparseable YAML config, got nil")
	}
}

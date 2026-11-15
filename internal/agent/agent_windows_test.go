//go:build windows

package agent

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-uem/openuem-agent/internal/agent/dsc"
	"github.com/open-uem/wingetcfg/wingetcfg"
)

func TestRunTasksGuaranteedReportOnTaskError(t *testing.T) {
	a := &Agent{}
	a.Config.UUID = "test-agent-uuid"

	taskControl := &dsc.TaskControl{}
	taskControlPath := filepath.Join(t.TempDir(), "tasks.json")

	// Resource with missing required settings (ID missing) so PowershellTask returns an error
	cfg := wingetcfg.WinGetCfg{
		Properties: wingetcfg.WinGetProperties{
			Resources: []*wingetcfg.WinGetResource{
				{
					Resource: wingetcfg.OpenUEMPowershell,
					ID:       "task_10_1",
					Settings: map[string]any{
						// missing ID in settings causes error
						"Script": "Write-Output 'hello'",
					},
				},
			},
		},
	}

	reports, err := a.RunTasks(cfg, 100, taskControlPath, taskControl, false)
	if err != nil {
		t.Fatalf("expected nil error from RunTasks despite task execution error, got: %v", err)
	}

	if len(reports) != 1 {
		t.Fatalf("expected 1 task report, got %d", len(reports))
	}

	r := reports[0]
	if r.Name != "task_10_1" {
		t.Errorf("expected report name 'task_10_1', got %q", r.Name)
	}

	if !r.Failed {
		t.Errorf("expected report.Failed == true, got false")
	}

	if r.StdErr == "" {
		t.Errorf("expected non-empty StdErr in TaskReport")
	}

	if r.StartTime == "" || r.EndTime == "" {
		t.Errorf("expected StartTime and EndTime to be populated, got StartTime=%q EndTime=%q", r.StartTime, r.EndTime)
	}
}

func TestRunTasksMultipleTasksContinueOnFailure(t *testing.T) {
	a := &Agent{}
	a.Config.UUID = "test-agent-uuid"

	taskControl := &dsc.TaskControl{}
	taskControlPath := filepath.Join(t.TempDir(), "tasks.json")

	cfg := wingetcfg.WinGetCfg{
		Properties: wingetcfg.WinGetProperties{
			Resources: []*wingetcfg.WinGetResource{
				{
					Resource: wingetcfg.OpenUEMPowershell,
					ID:       "task_1_1",
					Settings: map[string]any{
						// Missing Script key will cause PowershellTask to return an error
						"ID":   "task_1_1",
						"Name": "Invalid PowerShell Task",
					},
				},
				{
					Resource: wingetcfg.WinGetRegistryResource,
					ID:       "task_2_1",
					Settings: map[string]any{
						// Missing required keys like Ensure
						"ID": "task_2_1",
					},
				},
			},
		},
	}

	reports, err := a.RunTasks(cfg, 42, taskControlPath, taskControl, false)
	if err != nil {
		t.Fatalf("expected RunTasks not to abort with error, got: %v", err)
	}

	if len(reports) != 2 {
		t.Fatalf("expected 2 reports for both failed tasks, got %d", len(reports))
	}

	for i, r := range reports {
		if !r.Failed {
			t.Errorf("report %d (%s) expected Failed=true, got false", i, r.Name)
		}
		if r.StdErr == "" {
			t.Errorf("report %d (%s) expected non-empty StdErr", i, r.Name)
		}
		if r.EndTime == "" {
			t.Errorf("report %d (%s) expected non-empty EndTime", i, r.Name)
		}
	}
}


func TestPreparePowerShellScriptWindows(t *testing.T) {
	script := "Write-Output 'zażółć gęślą jaźń'"
	res := preparePowerShellScript(script)
	if !bytes.HasPrefix(res, UTF8BOM) {
		t.Fatalf("expected UTF-8 BOM prefix")
	}
	expectedHeader := "$OutputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8\r\n"
	if !strings.HasPrefix(string(res[len(UTF8BOM):]), expectedHeader) {
		t.Fatalf("expected encoding header")
	}
	if !strings.Contains(string(res), "zażółć gęślą jaźń") {
		t.Fatalf("expected Polish characters to be preserved")
	}
}

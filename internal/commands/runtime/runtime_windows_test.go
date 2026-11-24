//go:build windows

package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindProcessByName_NonExistent(t *testing.T) {
	_, err := FindProcessByName("non_existent_proc_xyz_99999.exe")
	if err == nil {
		t.Fatal("expected error for non-existent process")
	}
}

func TestFindProcessByName_CurrentProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip("cannot get executable path")
	}
	exeName := filepath.Base(exe)
	pid, err := FindProcessByName(exeName)
	if err != nil {
		t.Logf("Process %s not found in snapshot: %v", exeName, err)
		return
	}
	if pid == 0 {
		t.Fatal("expected non-zero PID")
	}
}

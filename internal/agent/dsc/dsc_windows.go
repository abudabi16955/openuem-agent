//go:build windows

package dsc

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"time"

	"github.com/open-uem/openuem-agent/internal/commands/runtime"
	"golang.org/x/sys/windows"
)

var ErrTimeout = errors.New("command execution timed out")

func KillProcessTreeWindows(pid int) error {
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[ERROR]: taskkill /T /F /PID %d failed: %v, output: %s", pid, err, string(out))
		return err
	}
	return nil
}

func RunTaskWithTimeout(command string, timeout time.Duration) (string, string, bool, error) {
	var stdOut bytes.Buffer
	var stdErr bytes.Buffer

	cmd := exec.Command("PowerShell", "-command", command)
	cmd.Stderr = &stdErr
	cmd.Stdout = &stdOut

	err := cmd.Start()
	if err != nil {
		return "", "", false, err
	}

	err = runtime.SetPriorityWindows(cmd.Process.Pid, windows.IDLE_PRIORITY_CLASS)
	if err != nil {
		log.Println("[ERROR]: could not change process priority")
		return "", "", false, err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var timer <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timer = t.C
	}

	select {
	case <-timer:
		log.Printf("[WARN]: command timed out after %v, killing process tree for PID %d", timeout, cmd.Process.Pid)
		if killErr := KillProcessTreeWindows(cmd.Process.Pid); killErr != nil {
			_ = cmd.Process.Kill()
		}
		<-done
		return stdOut.String(), fmt.Sprintf("execution timed out after %v", timeout), true, ErrTimeout
	case err := <-done:
		if err != nil {
			if _, ok := err.(*exec.ExitError); !ok {
				return "", "", false, err
			}
		}
		return stdOut.String(), stdErr.String(), false, nil
	}
}

func RunTaskWithLowPriority(command string) (string, string, error) {
	stdout, stderr, _, err := RunTaskWithTimeout(command, 0)
	return stdout, stderr, err
}

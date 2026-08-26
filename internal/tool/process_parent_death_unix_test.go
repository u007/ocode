//go:build !windows

package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const parentDeathHelperEnv = "OCODE_PARENT_DEATH_HELPER"
const parentDeathModeEnv = "OCODE_PARENT_DEATH_MODE"

// TestBashToolForegroundPromotionParentDeath verifies the complete lifecycle
// across a process boundary. The helper starts a foreground command, promotes
// it, and then stays alive. The test kills the helper with SIGKILL (so no
// supervisor cleanup can run) and checks that both the tracked monitor and its
// command disappear.
func TestBashToolForegroundPromotionParentDeath(t *testing.T) {
	if os.Getenv(parentDeathHelperEnv) == "1" {
		runParentDeathPromotionHelper(t)
		return
	}

	pidFile := t.TempDir() + "/child.pid"
	reportFile := filepath.Join(filepath.Dir(pidFile), "promotion.txt")
	var stderr bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=TestBashToolForegroundPromotionParentDeath")
	cmd.Env = append(os.Environ(),
		parentDeathHelperEnv+"=1",
		"OCODE_CHILD_PID_FILE="+pidFile,
		"OCODE_PROMOTION_REPORT="+reportFile,
	)
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	var wrapperPID, childPID int
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if wrapperPID > 1 {
			_ = syscall.Kill(-wrapperPID, syscall.SIGKILL)
			_ = syscall.Kill(wrapperPID, syscall.SIGKILL)
		}
		if childPID > 1 {
			_ = syscall.Kill(childPID, syscall.SIGKILL)
		}
	})

	var line string
	deadline := time.Now().Add(5 * time.Second)
	for line == "" && time.Now().Before(deadline) {
		data, readErr := os.ReadFile(reportFile)
		if readErr == nil {
			line = strings.TrimSpace(string(data))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if line == "" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("timed out waiting for promotion (stderr: %s)", stderr.String())
	}
	{
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "PROMOTED" {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("helper did not report promotion: %q (stderr: %s)", line, stderr.String())
		}
		var parseErr error
		wrapperPID, parseErr = strconv.Atoi(fields[1])
		if parseErr != nil {
			t.Fatalf("wrapper pid %q: %v", fields[1], parseErr)
		}
		childPID, parseErr = strconv.Atoi(fields[2])
		if parseErr != nil {
			t.Fatalf("child pid %q: %v", fields[2], parseErr)
		}
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("helper unexpectedly exited cleanly after SIGKILL")
	}

	if !waitForProcessGone(wrapperPID, 5*time.Second) {
		t.Fatalf("promoted monitor process %d survived helper SIGKILL", wrapperPID)
	}
	if !waitForProcessGone(childPID, 5*time.Second) {
		t.Fatalf("promoted command process %d survived helper SIGKILL", childPID)
	}
}

func TestBashToolBackgroundParentDeath(t *testing.T) {
	if os.Getenv(parentDeathHelperEnv) == "1" {
		runParentDeathBackgroundHelper(t)
		return
	}

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	reportFile := filepath.Join(dir, "background.txt")
	var output bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=TestBashToolBackgroundParentDeath")
	cmd.Env = append(os.Environ(),
		parentDeathHelperEnv+"=1",
		parentDeathModeEnv+"=background",
		"OCODE_CHILD_PID_FILE="+pidFile,
		"OCODE_PROMOTION_REPORT="+reportFile,
	)
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start background helper: %v", err)
	}
	var wrapperPID, childPID int
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if wrapperPID > 1 {
			_ = syscall.Kill(-wrapperPID, syscall.SIGKILL)
			_ = syscall.Kill(wrapperPID, syscall.SIGKILL)
		}
		if childPID > 1 {
			_ = syscall.Kill(childPID, syscall.SIGKILL)
		}
	})

	line := waitForParentDeathReport(t, reportFile, &output)
	fields := strings.Fields(line)
	if len(fields) != 3 || fields[0] != "BACKGROUND" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("helper did not report background process: %q (output: %s)", line, output.String())
	}
	var err error
	wrapperPID, err = strconv.Atoi(fields[1])
	if err != nil {
		t.Fatalf("wrapper pid %q: %v", fields[1], err)
	}
	childPID, err = strconv.Atoi(fields[2])
	if err != nil {
		t.Fatalf("child pid %q: %v", fields[2], err)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill background helper: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("background helper unexpectedly exited cleanly after SIGKILL")
	}
	if !waitForProcessGone(wrapperPID, 5*time.Second) {
		t.Fatalf("background monitor process %d survived helper SIGKILL", wrapperPID)
	}
	if !waitForProcessGone(childPID, 5*time.Second) {
		t.Fatalf("background command process %d survived helper SIGKILL", childPID)
	}
}

func runParentDeathPromotionHelper(t *testing.T) {
	pidFile := os.Getenv("OCODE_CHILD_PID_FILE")
	if pidFile == "" {
		t.Fatal("missing child pid file")
	}

	sup := NewProcessSupervisor(ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	reg := NewProcessRegistry()
	reg.SetSupervisor(sup)
	command := fmt.Sprintf("exec sh -c %s", shellQuote(fmt.Sprintf("echo $$ > %s; exec sleep 30", shellQuote(pidFile))))
	args, err := json.Marshal(map[string]any{"command": command})
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		_, _ = (BashTool{Procs: reg}).Execute(args)
	}()

	for range 500 {
		if id, _, ok := reg.RequestBackgroundLatest(); ok {
			for range 100 {
				for _, record := range sup.Snapshot() {
					if record.ID == id && record.PID > 1 {
						childPID, err := readPIDFile(pidFile)
						if err == nil {
							reportFile := os.Getenv("OCODE_PROMOTION_REPORT")
							if reportFile == "" {
								t.Fatal("missing promotion report file")
							}
							if err := os.WriteFile(reportFile, []byte(fmt.Sprintf("PROMOTED %d %d\n", record.PID, childPID)), 0o600); err != nil {
								t.Fatalf("write promotion report: %v", err)
							}
							select {}
						}
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("timed out waiting for promoted command pid")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out promoting foreground command")
}

func runParentDeathBackgroundHelper(t *testing.T) {
	pidFile := os.Getenv("OCODE_CHILD_PID_FILE")
	reportFile := os.Getenv("OCODE_PROMOTION_REPORT")
	if pidFile == "" || reportFile == "" {
		t.Fatal("missing background parent-death test paths")
	}

	reg := NewProcessRegistry()
	command := fmt.Sprintf("exec sh -c %s", shellQuote(fmt.Sprintf("echo $$ > %s; exec sleep 30", shellQuote(pidFile))))
	args, err := json.Marshal(map[string]any{"command": command, "run_in_background": true})
	if err != nil {
		t.Fatal(err)
	}
	p, err := func() (*Process, error) {
		out, executeErr := (BashTool{Procs: reg}).Execute(args)
		if executeErr != nil {
			return nil, executeErr
		}
		fields := strings.Fields(out)
		if len(fields) < 4 {
			return nil, fmt.Errorf("unexpected background result: %q", out)
		}
		id := strings.TrimSuffix(fields[3], ".")
		reg.mu.Lock()
		process := reg.procs[id]
		reg.mu.Unlock()
		if process == nil {
			return nil, fmt.Errorf("background process %q not found", id)
		}
		return process, nil
	}()
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if p.PID > 1 {
			if _, readErr := readPIDFile(pidFile); readErr == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for background child pid (process pid=%d)", p.PID)
		}
		time.Sleep(10 * time.Millisecond)
	}
	childPID, err := readPIDFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportFile, []byte(fmt.Sprintf("BACKGROUND %d %d\n", p.PID, childPID)), 0o600); err != nil {
		t.Fatalf("write background report: %v", err)
	}
	select {}
}

func readPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func waitForProcessGone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return syscall.Kill(pid, 0) != nil
}

func waitForParentDeathReport(t *testing.T, path string, output *bytes.Buffer) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(data)) != "" {
			return strings.TrimSpace(string(data))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for parent-death report (output: %s)", output.String())
	return ""
}

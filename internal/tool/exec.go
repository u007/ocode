package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

const bashDefaultTimeout = 300 * time.Second
const bashMaxOutputLength = 30000

type BashTool struct {
	Procs *ProcessRegistry
}

func (t BashTool) Name() string        { return "bash" }
func (t BashTool) Description() string { return "Execute shell commands and return stdout/stderr" }
func (t BashTool) Parallel() bool      { return false }
func (t BashTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "bash",
		"description": fmt.Sprintf("Execute shell commands and return combined stdout and stderr. Timeout: %v (default).", bashDefaultTimeout),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "The command to execute",
				},
				"timeout": map[string]interface{}{
					"type":        "integer",
					"description": fmt.Sprintf("Timeout in seconds (default: %d, max: 600).", int(bashDefaultTimeout.Seconds())),
				},
				"run_in_background": map[string]interface{}{
					"type":        "boolean",
					"description": "Run the command in the background. Returns a process id immediately; poll with bash_output and stop with kill_shell.",
				},
			},
			"required": []string{"command"},
		},
	}
}

func (t BashTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Command         string `json:"command"`
		Timeout         int    `json:"timeout"`
		RunInBackground bool   `json:"run_in_background"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.RunInBackground {
		if t.Procs == nil {
			return "", fmt.Errorf("background execution unavailable: no process registry")
		}
		p := t.Procs.StartBackground(params.Command)
		return fmt.Sprintf("Started background process %s. Poll with bash_output(id=%q), stop with kill_shell(id=%q).", p.ID, p.ID, p.ID), nil
	}

	timeout := bashDefaultTimeout
	if params.Timeout > 0 {
		timeout = time.Duration(params.Timeout) * time.Second
		if timeout > 600*time.Second {
			timeout = 600 * time.Second
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	shouldCancel := true
	defer func() {
		if shouldCancel {
			cancel()
		}
	}()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", params.Command)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-c", params.Command)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	var stdout, stderr bytes.Buffer
	var proc *Process
	if t.Procs == nil {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	}

	var sup *ProcessSupervisor
	var onDone func(*Process)
	if t.Procs != nil {
		sup = t.Procs.Supervisor()
		onDone = t.Procs.onDoneCallback()
	}
	startedAt := time.Now()
	if t.Procs != nil {
		waitState := newCommandWait()
		stdoutPipe, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Sprintf("Command failed: %v", err), nil
		}
		stderrPipe, err := cmd.StderrPipe()
		if err != nil {
			return fmt.Sprintf("Command failed: %v", err), nil
		}

		// Start the process BEFORE registering it with the supervisor so that
		// cmd.Process is fully populated. Registering first races with Start:
		// the supervisor stores a reference to *exec.Cmd and may read
		// cmd.Process from Shutdown/force paths while Start is still writing it.
		if err := cmd.Start(); err != nil {
			return fmt.Sprintf("Command failed: %v", err), nil
		}

		var regErr error
		proc, regErr = t.Procs.RegisterForeground(params.Command, cmd, startedAt, waitState.Wait)
		if regErr != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Sprintf("Command failed: %v", regErr), nil
		}

		// Pump stdout/stderr concurrently into the shared bytes.Buffers AND
		// the Process output ring. The WaitGroup guarantees both pumps have
		// returned before the foreground branch reads the buffers, otherwise
		// io.Copy could still be writing after cmd.Wait() unblocks — a data
		// race that the -race detector flags.
		var pumpWg sync.WaitGroup
		pump := func(dst *bytes.Buffer, rc io.Reader) {
			defer pumpWg.Done()
			_, _ = io.Copy(io.MultiWriter(dst, processWriter{p: proc}), rc)
		}
		pumpWg.Add(2)
		go pump(&stdout, stdoutPipe)
		go pump(&stderr, stderrPipe)

		go func() {
			waitState.Store(cmd.Wait())
		}()

		select {
		case <-waitState.Done():
			// Wait for pump goroutines to drain the pipes before reading the
			// buffers; cmd.Wait() returns as soon as the child exits, but the
			// kernel-buffered tail of stdout/stderr may still be in flight.
			pumpWg.Wait()
			err := waitState.Result()
			res := joinStdoutStderr(stdout.String(), stderr.String())
			// finalizeManagedProcess is the sole place that marks the
			// supervisor exited/killed — the inline MarkExited/MarkKilled
			// block that used to live here duplicated that work.
			finalizeManagedProcess(proc, sup, onDone, err)
			return finalizeExecResult(res, err, ctx.Err() == context.DeadlineExceeded, timeout), nil
		case <-proc.bgRequestCh:
			shouldCancel = false
			go func() {
				err := waitState.Wait()
				pumpWg.Wait()
				finalizeManagedProcess(proc, sup, onDone, err)
				cancel()
			}()
			return fmt.Sprintf("Moved running bash command to background as %s. Continue the turn now; poll with bash_output(id=%q), stop with kill_shell(id=%q), or trust the completion push when it finishes.", proc.ID, proc.ID, proc.ID), nil
		}
	}

	err := cmd.Run()
	res := joinStdoutStderr(stdout.String(), stderr.String())
	return finalizeExecResult(res, err, ctx.Err() == context.DeadlineExceeded, timeout), nil
}

// joinStdoutStderr concatenates the captured stdout and stderr into a single
// human-readable string, inserting a newline separator only when both halves
// are non-empty.
func joinStdoutStderr(stdoutStr, stderrStr string) string {
	if stderrStr == "" {
		return stdoutStr
	}
	if stdoutStr == "" {
		return stderrStr
	}
	return stdoutStr + "\n" + stderrStr
}

// finalizeExecResult formats the user-facing output string for a finished
// shell command, identical for the registry-managed and registry-less paths.
func finalizeExecResult(res string, err error, timedOut bool, timeout time.Duration) string {
	if timedOut {
		return fmt.Sprintf("Command timed out after %v. Output so far:\n%s", timeout, truncateOutput(res))
	}
	if err != nil {
		code := commandExitCode(err)
		if res == "" {
			return fmt.Sprintf("Command failed (exit code %d): %v", code, err)
		}
		return fmt.Sprintf("Command failed (exit code %d). Output:\n%s", code, truncateOutput(res))
	}
	if strings.TrimSpace(res) == "" {
		return "Command executed successfully (no output)."
	}
	return truncateOutput(res)
}

func truncateOutput(s string) string {
	if len(s) <= bashMaxOutputLength {
		return s
	}
	return s[:bashMaxOutputLength] + "\n\n... [output truncated, exceeds 30000 chars]"
}

type processWriter struct{ p *Process }

func (w processWriter) Write(b []byte) (int, error) {
	w.p.appendOutput(b)
	return len(b), nil
}

type commandWait struct {
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func newCommandWait() *commandWait {
	return &commandWait{done: make(chan struct{})}
}

func (w *commandWait) Store(err error) {
	w.mu.Lock()
	w.err = err
	w.mu.Unlock()
	close(w.done)
}

func (w *commandWait) Done() <-chan struct{} { return w.done }

func (w *commandWait) Wait() error {
	<-w.done
	return w.Result()
}

func (w *commandWait) Result() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return 1
}

func finalizeManagedProcess(proc *Process, sup *ProcessSupervisor, onDone func(*Process), err error) {
	status := ProcExited
	proc.mu.Lock()
	if proc.Status == ProcKilled {
		status = ProcKilled
	}
	proc.mu.Unlock()
	if sup != nil {
		if status == ProcKilled {
			sup.MarkKilled(proc.ID, commandExitCode(err))
		} else {
			sup.MarkExited(proc.ID, commandExitCode(err))
		}
	}
	finishProcess(proc, commandExitCode(err), status, onDone)
}

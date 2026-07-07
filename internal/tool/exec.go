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
	"sync/atomic"
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
	return t.ExecuteStream(args, nil)
}

// ExecuteStream runs the bash command and, when emit is non-nil, streams
// incremental stdout/stderr chunks to it as they are produced. The returned
// string is the canonical, complete result captured into the buffer ring and
// supervisor. Background and move-to-background paths return immediately and
// stop streaming (the live output shown up to that point is preserved).
func (t BashTool) ExecuteStream(args json.RawMessage, emit func(chunk string)) (string, error) {
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

	// streaming gates live emission. Once the command is moved to the
	// background we stop emitting so the transcript keeps the output produced
	// before the move and is not polluted by trailing background output.
	var streaming atomic.Bool
	if emit != nil {
		streaming.Store(true)
	}
	safeEmit := func(b []byte) {
		if emit == nil || len(b) == 0 || !streaming.Load() {
			return
		}
		emit(string(b))
	}

	var stdout, stderr bytes.Buffer
	var proc *Process
	if t.Procs == nil {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if emit != nil {
			cmd.Stdout = io.MultiWriter(&stdout, emitWriter{emit: safeEmit})
			cmd.Stderr = io.MultiWriter(&stderr, emitWriter{emit: safeEmit})
		}
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
			if emit != nil {
				_, _ = io.Copy(io.MultiWriter(dst, processWriter{p: proc}, emitWriter{emit: safeEmit}), rc)
			} else {
				_, _ = io.Copy(io.MultiWriter(dst, processWriter{p: proc}), rc)
			}
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
			return finalizeExecResult(res, err, ctx.Err() == context.DeadlineExceeded, timeout, emit == nil), nil
		case <-proc.bgRequestCh:
			streaming.Store(false)
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
	return finalizeExecResult(res, err, ctx.Err() == context.DeadlineExceeded, timeout, emit == nil), nil
}

// emitWriter adapts a chunk-emitting callback to io.Writer so it can be
// composed into an io.MultiWriter pipeline alongside the buffer/ring sinks.
type emitWriter struct{ emit func(b []byte) }

func (w emitWriter) Write(b []byte) (int, error) {
	w.emit(b)
	return len(b), nil
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
// When capAtMax is false (i.e. the command streamed its output live), the
// 30000-char hard cap is skipped so the canonical result keeps the full output
// that was already shown to the user; the agent truncates for the LLM prompt
// separately via TruncateToolResult, and the full text is carried to the UI
// through Message.DisplayContent.
func finalizeExecResult(res string, err error, timedOut bool, timeout time.Duration, capAtMax bool) string {
	applyCap := func(s string) string {
		if capAtMax {
			return truncateOutput(s)
		}
		return s
	}
	if timedOut {
		return fmt.Sprintf("Command timed out after %v. Output so far:\n%s", timeout, applyCap(res))
	}
	if err != nil {
		code := commandExitCode(err)
		if res == "" {
			return fmt.Sprintf("Command failed (exit code %d): %v", code, err)
		}
		return fmt.Sprintf("Command failed (exit code %d). Output:\n%s", code, applyCap(res))
	}
	if strings.TrimSpace(res) == "" {
		return "Command executed successfully (no output)."
	}
	return applyCap(res)
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
		// Use the supervisor-scoped key captured at registration. When a
		// subagent's ProcessRegistry has SupervisorIDPrefix set, proc.ID is
		// the bare counter ("proc-N") while the supervisor record is keyed
		// "<prefix>proc-N"; calling MarkExited/MarkKilled with proc.ID would
		// silently miss the record and leave the process stuck in Running.
		key := proc.SupKey()
		if key == "" {
			key = proc.ID
		}
		if status == ProcKilled {
			sup.MarkKilled(key, commandExitCode(err))
		} else {
			sup.MarkExited(key, commandExitCode(err))
		}
	}
	finishProcess(proc, commandExitCode(err), status, onDone)
}

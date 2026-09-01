package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/snapshot"
)

const bashDefaultTimeout = 300 * time.Second
const bashMaxOutputLength = 30000

// bashMaxRetainedBytes bounds how much output is held in memory WHILE a command
// runs. It applies to stdout and stderr independently, so the ceiling is 2x this
// per command — and commands in a parallel tool batch each carry their own pair.
//
// bashMaxOutputLength is a display cap applied by truncateOutput after the
// command exits, so it never bounds the peak: until then the sink grows for as
// long as the command runs, which the timeout allows to be up to 600s of
// arbitrary output.
//
// This is a runaway guard, not a display limit — it sits far above any
// legitimate command's output so normal use never reaches it, and only stops an
// accidental infinite loop or a `find / | xargs cat` from growing the heap
// without limit.
const bashMaxRetainedBytes = 64 << 20 // 64MiB

// BashRecorder is the seam between BashTool and the changes registry.
// The tool calls Pre() before executing a command and Post(command, exitCode)
// after it returns. Implementations capture filesystem state around the
// invocation to detect file changes made by the shell command.
type BashRecorder interface {
	Pre()
	Post(command string, exitCode int)
}

type BashTool struct {
	Procs    *ProcessRegistry
	Recorder BashRecorder
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
	return t.ExecuteStreamCtx(context.Background(), args, nil)
}

// ExecuteStream runs the bash command and, when emit is non-nil, streams
// incremental stdout/stderr chunks to it as they are produced. Backups for
// undo are only captured when a snapshot store and tool call ID are
// available in the context — use ExecuteCtx/ExecuteStreamCtx for that.
func (t BashTool) ExecuteStream(args json.RawMessage, emit func(chunk string)) (string, error) {
	return t.ExecuteStreamCtx(context.Background(), args, emit)
}

// ExecuteCtx runs the bash command with backup-for-undo support but no
// streaming.
func (t BashTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	return t.ExecuteStreamCtx(ctx, args, nil)
}

// ExecuteStreamCtx runs the bash command and, when emit is non-nil, streams
// incremental stdout/stderr chunks to it as they are produced. The returned
// string is the canonical, complete result captured into the buffer ring and
// supervisor. Background and move-to-background paths return immediately and
// stop streaming (the live output shown up to that point is preserved).
//
// Before running a whitelisted destructive command (rm, mv, cp, sed -i,
// truncate) matched by destructiveBashBackupPaths, the target file(s) are
// snapshotted via the context's snapshot store so undo_file_change can
// revert them — the same mechanism the Edit/Write tools use.
func (t BashTool) ExecuteStreamCtx(ctx context.Context, args json.RawMessage, emit func(chunk string)) (string, error) {
	var params struct {
		Command         string `json:"command"`
		Timeout         int    `json:"timeout"`
		RunInBackground bool   `json:"run_in_background"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	// Reject commands that are empty or contain only whitespace. A model that
	// emits a tool call with no arguments (e.g. a codex/responses-lite model
	// served over the chat-completions endpoint) would otherwise run `bash -c ""`,
	// a silent no-op that can corrupt a multi-step plan. Fail loudly instead.
	if strings.TrimSpace(params.Command) == "" {
		return "", fmt.Errorf("bash: refusing to run an empty command")
	}

	tcID := snapshot.ToolCallIDFromContext(ctx)
	var backedUpPaths []string
	if tcID != "" {
		store := snapshot.FromContext(ctx)
		for _, p := range destructiveBashBackupPaths(params.Command) {
			safe, err := confinedPath(ctx, p)
			if err != nil {
				continue // outside the allowed scope — not ours to back up
			}
			if err := store.Backup(safe, tcID); err != nil {
				continue // intentionally not logged: best-effort undo support, never blocks the command
			}
			backedUpPaths = append(backedUpPaths, safe)
		}
	}

	if params.RunInBackground {
		if t.Procs == nil {
			return "", fmt.Errorf("background execution unavailable: no process registry")
		}
		// Wrapped so the child self-terminates (polling kill -0 on ocode's
		// PID) if ocode is force-killed and never gets to run its own
		// graceful-shutdown cleanup — see ParentMonitorWrap's doc comment.
		// StartBackgroundDisplay keeps bash_output/kill_shell listings
		// showing the command the caller actually asked for, not the
		// monitor-wrapper shell around it. The session workdir rides along so
		// the background cmd.Dir and its process-hole cwd are the session
		// root, not the process cwd.
		p := t.Procs.StartBackgroundDisplayDir(WrapWithParentMonitor(params.Command), params.Command, workDirFromContext(ctx))
		return fmt.Sprintf("Started background process %s. Poll with bash_output(id=%q), stop with kill_shell(id=%q).", p.ID, p.ID, p.ID), nil
	}

	if t.Recorder != nil {
		t.Recorder.Pre()
	}

	timeout := bashDefaultTimeout
	if params.Timeout > 0 {
		timeout = time.Duration(params.Timeout) * time.Second
		if timeout > 600*time.Second {
			timeout = 600 * time.Second
		}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	shouldCancel := true
	defer func() {
		if shouldCancel {
			cancel()
		}
	}()

	command := params.Command
	// Wrap before Start, rather than when the command is promoted. A
	// foreground command may become a background command after it has
	// already started; starting the monitor here means both paths retain
	// the same parent-death protection without interrupting streaming or
	// replacing the process that the registry and supervisor track.
	if t.Procs != nil {
		command = WrapWithParentMonitor(params.Command)
	}
	// Unified construction: GOOS branch + session-workdir cmd.Dir. The
	// timeout ctx keeps CommandContext kill behavior (whole process group via
	// setProcGroup); sandbox wrap lands here in Part 02.
	cmd := buildBashCmd(ctx, command, workDirFromContext(ctx))

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

	// Bounded so a runaway command cannot grow the heap for the whole timeout
	// window; see bashMaxRetainedBytes.
	stdout := newBoundedBuffer(bashMaxRetainedBytes)
	stderr := newBoundedBuffer(bashMaxRetainedBytes)
	var proc *Process
	if t.Procs == nil {
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if emit != nil {
			cmd.Stdout = io.MultiWriter(stdout, emitWriter{emit: safeEmit})
			cmd.Stderr = io.MultiWriter(stderr, emitWriter{emit: safeEmit})
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

		// Pump stdout/stderr concurrently into the shared bounded buffers AND
		// the Process output ring. The WaitGroup guarantees both pumps have
		// returned before the foreground branch reads the buffers, otherwise
		// io.Copy could still be writing after cmd.Wait() unblocks — a data
		// race that the -race detector flags.
		var pumpWg sync.WaitGroup
		pump := func(dst *boundedBuffer, rc io.Reader) {
			defer pumpWg.Done()
			if emit != nil {
				_, _ = io.Copy(io.MultiWriter(dst, processWriter{p: proc}, emitWriter{emit: safeEmit}), rc)
			} else {
				_, _ = io.Copy(io.MultiWriter(dst, processWriter{p: proc}), rc)
			}
		}
		pumpWg.Add(2)
		go pump(stdout, stdoutPipe)
		go pump(stderr, stderrPipe)

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
			res := appendRunawayNotice(joinStdoutStderr(stdout.String(), stderr.String()), stdout.Dropped()+stderr.Dropped())
			// finalizeManagedProcess is the sole place that marks the
			// supervisor exited/killed — the inline MarkExited/MarkKilled
			// block that used to live here duplicated that work.
			finalizeManagedProcess(proc, sup, onDone, err)
			registerBashWrites(ctx, tcID, backedUpPaths)
			if t.Recorder != nil {
				t.Recorder.Post(params.Command, commandExitCode(err))
			}
			return finalizeExecResult(res, err, ctx.Err() == context.DeadlineExceeded, timeout, !FullOutputRetained(ctx)), nil
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
		case <-ctx.Done():
			// The exec timeout (or caller cancellation) fired before the
			// command exited. exec.CommandContext's default Cancel only
			// kills the direct child (cmd.Process); setProcGroup put the
			// child in its own process group, so a grandchild that
			// inherited the stdout/stderr pipes (e.g. a pipeline stage)
			// can keep them open indefinitely and block pumpWg.Wait()
			// forever even after the child is dead. Kill the whole
			// process group and return immediately with whatever output
			// was captured so far; draining the pipes and finalizing the
			// process record continues on its own goroutine instead of
			// blocking this call.
			shouldCancel = false
			if cmd.Process != nil {
				_ = killProcessGroup(cmd.Process)
			}
			streaming.Store(false)
			res, _, _, _ := t.Procs.Dump(proc.ID)
			go func() {
				err := waitState.Wait()
				pumpWg.Wait()
				finalizeManagedProcess(proc, sup, onDone, err)
				cancel()
			}()
			registerBashWrites(ctx, tcID, backedUpPaths)
			if t.Recorder != nil {
				t.Recorder.Post(params.Command, 124)
			}
			return finalizeExecResult(res, ctx.Err(), true, timeout, !FullOutputRetained(ctx)), nil
		}
	}

	err := cmd.Run()
	res := appendRunawayNotice(joinStdoutStderr(stdout.String(), stderr.String()), stdout.Dropped()+stderr.Dropped())
	registerBashWrites(ctx, tcID, backedUpPaths)
	if t.Recorder != nil {
		t.Recorder.Post(params.Command, commandExitCode(err))
	}
	return finalizeExecResult(res, err, ctx.Err() == context.DeadlineExceeded, timeout, !FullOutputRetained(ctx)), nil
}

// registerBashWrites records backed-up paths as written in the snapshot
// store's cross-agent registry, mirroring what Edit/Write tools do after a
// successful write, so undo_file_change's conflict detection sees them.
func registerBashWrites(ctx context.Context, tcID string, paths []string) {
	if tcID == "" || len(paths) == 0 {
		return
	}
	store := snapshot.FromContext(ctx)
	for _, p := range paths {
		store.RegisterWrite(p, tcID)
	}
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
// When capAtMax is false, the 30000-char hard cap is skipped so the canonical
// result keeps the full output that was already shown to the user; the agent
// truncates for the LLM prompt separately via TruncateToolResult, and the full
// text is carried to the UI through Message.DisplayContent.
//
// capAtMax is driven by tool.FullOutputRetained(ctx), NOT by whether a
// streaming sink was wired. Streaming and full-output retention are separate
// questions: the SSE server streams chunks for live progress but receives the
// authoritative result through a separately-truncated frame, and cannot read
// DisplayContent at all (json:"-"). See fulloutput_ctx.go.
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

// appendRunawayNotice records that the runaway output guard discarded bytes, so
// an incomplete result is never presented as a complete one. When the result is
// additionally capped by truncateOutput the notice is trimmed away with the
// rest of the tail, but that path carries its own truncation notice already.
func appendRunawayNotice(res string, dropped int) string {
	if dropped <= 0 {
		return res
	}
	return res + fmt.Sprintf(
		"\n\n... [output truncated: %d bytes dropped past the %d-byte runaway output guard]",
		dropped, bashMaxRetainedBytes)
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

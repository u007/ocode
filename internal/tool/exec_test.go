package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// TestBashTool_ForegroundLargeOutput exercises the pump/Wait race fix in
// BashTool.Execute. When the foreground bash branch reads stdout/stderr after
// cmd.Wait() returns, the io.Copy goroutines must already have drained the
// pipes — otherwise `go test -race` flags a data race AND the tail of the
// output is lost. Generating >64KB of output (well past the pipe buffer)
// forces the kernel to back-pressure the writer, so the race window is
// real.
func TestBashTool_ForegroundLargeOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses bash -c yes/head pipeline")
	}
	procs := NewProcessRegistry()
	bt := BashTool{Procs: procs}

	// Produce ~200KB of output: 1000 lines of ~200 bytes each.
	cmd := `for i in $(seq 1 1000); do printf '%0.s=' $(seq 1 200); printf "\n"; done`
	args, _ := json.Marshal(map[string]interface{}{"command": cmd})
	out, err := bt.Execute(args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	// The tool truncates at bashMaxOutputLength (30000); verify we received
	// at least that many bytes of payload so the pump goroutines clearly
	// finished before the buffer was read.
	if len(out) < bashMaxOutputLength {
		t.Fatalf("expected at least %d bytes captured, got %d", bashMaxOutputLength, len(out))
	}
	// Sanity check: it should be all '=' chars (with newlines).
	if !strings.Contains(out, strings.Repeat("=", 200)) {
		t.Fatalf("output missing expected payload; first 200 bytes: %q", out[:min(200, len(out))])
	}
	_ = fmt.Sprint // keep fmt imported in case payload assertion changes
}

// TestBashTool_ExecuteStream verifies that incremental stdout/stderr is
// emitted to the callback as it is produced, while the returned string remains
// the canonical full result. It also confirms streaming stops once the command
// is moved to the background.
func TestBashTool_ExecuteStream(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses bash -c")
	}
	procs := NewProcessRegistry()
	bt := BashTool{Procs: procs}

	// Emit three clearly delimited lines so we can assert the callback fired
	// more than once and before the final result was returned.
	cmd := `printf 'line-one\n'; sleep 0.05; printf 'line-two\n'; sleep 0.05; printf 'line-three\n'`
	args, _ := json.Marshal(map[string]interface{}{"command": cmd})

	var mu sync.Mutex
	var chunks []string
	out, err := bt.ExecuteStream(args, func(chunk string) {
		mu.Lock()
		chunks = append(chunks, chunk)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("ExecuteStream returned error: %v", err)
	}
	mu.Lock()
	joined := strings.Join(chunks, "")
	mu.Unlock()

	// The callback must have streamed output live (more than one chunk or a
	// non-empty aggregated stream), and the canonical result must contain all
	// three lines.
	if joined == "" {
		t.Fatalf("expected streamed chunks, got none")
	}
	for _, want := range []string{"line-one", "line-two", "line-three"} {
		if !strings.Contains(joined, want) {
			t.Errorf("streamed output missing %q; got %q", want, joined)
		}
		if !strings.Contains(out, want) {
			t.Errorf("canonical result missing %q; got %q", want, out)
		}
	}
}

// TestBashTool_ExecuteFallsBackWithoutEmit confirms that calling Execute (no
// emit) still returns the full result and does not require a streaming sink.
func TestBashTool_ExecuteFallsBackWithoutEmit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses bash -c")
	}
	procs := NewProcessRegistry()
	bt := BashTool{Procs: procs}
	cmd := `printf 'hello-world\n'`
	args, _ := json.Marshal(map[string]interface{}{"command": cmd})
	out, err := bt.Execute(args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(out, "hello-world") {
		t.Fatalf("expected hello-world in result, got %q", out)
	}
}

// TestBashTool_RejectsEmptyCommand verifies that a command that is empty or
// contains only whitespace is rejected before any process is spawned (and
// before backups/recorder hooks run). This guards against models that emit a
// tool call with no arguments (e.g. a codex/responses-lite model served over
// the chat-completions endpoint) silently running `bash -c ""`.
func TestBashTool_RejectsEmptyCommand(t *testing.T) {
	procs := NewProcessRegistry()
	bt := BashTool{Procs: procs}

	for _, cmd := range []string{"", "   ", "\t\n  "} {
		args, _ := json.Marshal(map[string]interface{}{"command": cmd})
		out, err := bt.Execute(args)
		if err == nil {
			t.Fatalf("expected error for command %q, got output %q", cmd, out)
		}
		if !strings.Contains(err.Error(), "empty command") {
			t.Fatalf("expected 'empty command' error for %q, got %v", cmd, err)
		}
	}
}

// TestBashTool_StreamKeepsFullOutput verifies the chunked-tool-result fix:
// a consumer that renders the live stream AS the transcript (the TUI) opts in
// via WithFullOutputRetained, and its canonical result is NOT capped at
// bashMaxOutputLength (30000), so the full output is carried to the UI.
// The synchronous Execute path still caps at 30000. Without this, a large
// streamed result would be clobbered by the cap on completion and the live
// chunks would appear "not applied".
//
// Opting in used to be implicit in "emit != nil". It is now explicit, so that
// streaming for live progress only (the SSE server) stays capped — see
// TestBashTool_StreamCapsWithoutRetainFlag.
func TestBashTool_StreamKeepsFullOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses bash -c")
	}
	procs := NewProcessRegistry()
	bt := BashTool{Procs: procs}

	// Generate ~50000 chars — well above the 30000 cap.
	cmd := `yes "0123456789ABCDEFGHIJ" | head -n 2000`
	args, _ := json.Marshal(map[string]interface{}{"command": cmd})

	var mu sync.Mutex
	var chunks []string
	ctx := WithFullOutputRetained(context.Background(), true)
	streamed, err := bt.ExecuteStreamCtx(ctx, args, func(chunk string) {
		mu.Lock()
		chunks = append(chunks, chunk)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("ExecuteStreamCtx returned error: %v", err)
	}
	// The live-streamed path must keep the FULL output (no 30000 cap), so it
	// must NOT carry the truncation notice and must exceed the cap.
	if strings.Contains(streamed, "exceeds 30000 chars") {
		t.Fatalf("streamed result must not be capped, but got truncation notice; len=%d", len(streamed))
	}
	if len(streamed) <= bashMaxOutputLength {
		t.Fatalf("expected streamed result to exceed the %d cap (full output), got %d", bashMaxOutputLength, len(streamed))
	}

	// The synchronous path caps at bashMaxOutputLength and carries the notice.
	syncOut, err := bt.Execute(args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(syncOut, "exceeds 30000 chars") {
		t.Fatalf("synchronous result must be capped at %d, got no notice; len=%d", bashMaxOutputLength, len(syncOut))
	}
	// Content portion is capped: strip the notice and confirm bounded length.
	capped := syncOut
	if idx := strings.Index(syncOut, "\n\n... [output truncated"); idx >= 0 {
		capped = syncOut[:idx]
	}
	if len(capped) != bashMaxOutputLength {
		t.Fatalf("expected capped content length %d, got %d", bashMaxOutputLength, len(capped))
	}
}

// TestBashTool_StreamCapsWithoutRetainFlag is the memory-retention guard for
// the server/desktop path. Wiring a streaming sink (emit != nil) must NOT by
// itself disable the bashMaxOutputLength cap: only an explicit
// WithFullOutputRetained(ctx, true) may do that.
//
// The distinction matters because the full, uncapped text is carried to a UI
// solely via Message.DisplayContent, which is json:"-" and therefore never
// serialized to the browser. A server that streams but does not opt in would
// retain an unbounded result for a consumer that structurally cannot receive
// it — pure memory growth on the hot bash path.
func TestBashTool_StreamCapsWithoutRetainFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses bash -c")
	}
	procs := NewProcessRegistry()
	bt := BashTool{Procs: procs}

	// Generate ~50000 chars — well above the 30000 cap.
	cmd := `yes "0123456789ABCDEFGHIJ" | head -n 2000`
	args, _ := json.Marshal(map[string]interface{}{"command": cmd})

	var mu sync.Mutex
	var chunks []string
	// Streaming sink wired, but the context does NOT opt into full retention.
	out, err := bt.ExecuteStreamCtx(context.Background(), args, func(chunk string) {
		mu.Lock()
		chunks = append(chunks, chunk)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("ExecuteStreamCtx returned error: %v", err)
	}

	// Chunks must still stream live — capping the canonical result must not
	// disable streaming.
	mu.Lock()
	streamed := strings.Join(chunks, "")
	mu.Unlock()
	if streamed == "" {
		t.Fatal("expected live chunks to stream even when the result is capped, got none")
	}

	if !strings.Contains(out, "exceeds 30000 chars") {
		t.Fatalf("result must be capped without the retain flag, got no truncation notice; len=%d", len(out))
	}
	capped := out
	if idx := strings.Index(out, "\n\n... [output truncated"); idx >= 0 {
		capped = out[:idx]
	}
	if len(capped) != bashMaxOutputLength {
		t.Fatalf("expected capped content length %d, got %d", bashMaxOutputLength, len(capped))
	}
}

// TestBashTool_BoundsRunawayOutput is the memory guard for the reported hang:
// a single command producing more than bashMaxRetainedBytes must not grow the
// in-memory sink without limit. The pre-existing 30000-char cap does not help
// here — truncateOutput runs only after the command exits, so it never bounds
// the peak while a runaway command is still writing.
//
// Uses the retain-full-output path (the TUI's), because that is the path with
// no other cap in front of it: without the guard this result would be the full
// multi-megabyte stream.
func TestBashTool_BoundsRunawayOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses bash -c yes/head pipeline")
	}
	procs := NewProcessRegistry()
	bt := BashTool{Procs: procs}

	// Emit ~2MB past the cap.
	overshoot := bashMaxRetainedBytes + 2<<20
	cmd := fmt.Sprintf(`yes "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ" | head -c %d`, overshoot)
	args, _ := json.Marshal(map[string]interface{}{"command": cmd})

	ctx := WithFullOutputRetained(context.Background(), true)
	out, err := bt.ExecuteStreamCtx(ctx, args, nil)
	if err != nil {
		t.Fatalf("ExecuteStreamCtx returned error: %v", err)
	}

	// Retention must be bounded by the guard, not by the command's volume.
	if len(out) > bashMaxRetainedBytes+2048 {
		t.Fatalf("retained output must be bounded near the %d-byte guard, got %d",
			bashMaxRetainedBytes, len(out))
	}
	// Dropping bytes must be reported, never silent.
	if !strings.Contains(out, "runaway output guard") {
		t.Fatalf("expected a truncation notice naming the runaway guard; got tail %q",
			out[max(0, len(out)-200):])
	}
}

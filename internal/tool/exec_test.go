package tool

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
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

package tool

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestParentMonitorWrap_ContainsParentAndChild(t *testing.T) {
	cmd := "HF_HUB_OFFLINE=1 python3 -m mlx_lm.server --model foo --port 11459"
	wrapped := ParentMonitorWrap(cmd, 12345)
	if !strings.Contains(wrapped, "ppid=12345") {
		t.Fatalf("wrapped missing ppid: %q", wrapped)
	}
	if !strings.Contains(wrapped, cmd) {
		t.Fatalf("wrapped missing original cmdline: %q", wrapped)
	}
	if !strings.Contains(wrapped, "kill -0 $ppid") {
		t.Fatalf("wrapped missing parent poll: %q", wrapped)
	}
	if !strings.Contains(wrapped, "child=$!") {
		t.Fatalf("wrapped missing child capture: %q", wrapped)
	}
	if !strings.Contains(wrapped, "trap") {
		t.Fatalf("wrapped missing trap: %q", wrapped)
	}
}

func TestParentMonitorWrap_FallbackPID(t *testing.T) {
	cmd := "echo hi"
	wrapped := ParentMonitorWrap(cmd, 0)
	// fallback should embed current pid, not 0 or 1
	pid := strconv.Itoa(os.Getpid())
	if !strings.Contains(wrapped, "ppid="+pid) {
		t.Fatalf("fallback pid not current pid: got %q want ppid=%s", wrapped, pid)
	}
}

func TestWrapWithParentMonitor(t *testing.T) {
	cmd := "python3 -m mlx_lm.server --port 11458"
	wrapped := WrapWithParentMonitor(cmd)
	pid := strconv.Itoa(os.Getpid())
	if !strings.Contains(wrapped, "ppid="+pid) {
		t.Fatalf("WrapWithParentMonitor missing current ppid: %q", wrapped)
	}
}

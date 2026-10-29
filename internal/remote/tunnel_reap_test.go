package remote

import (
	"errors"
	"os"
	"runtime"
	"testing"
)

func TestParseListeningPID(t *testing.T) {
	out := `COMMAND   PID   USER   FD   TYPE             DEVICE SIZE/OFF NODE NAME
ssh     20546 james    5u  IPv6 0x488e4df995a2d8c7      0t0  TCP [::1]:36655 (LISTEN)
ssh     20546 james    6u  IPv4 0x70fb700a3123280a      0t0  TCP 127.0.0.1:36655 (LISTEN)`
	pids, err := parseLsofOutput(out, 36655)
	if err != nil {
		t.Fatalf("parseListeningPID() error: %v", err)
	}
	// Two rows for one pid (IPv4 and IPv6) list that pid once.
	if len(pids) != 1 || pids[0] != 20546 {
		t.Fatalf("parseListeningPID() = %v, want [20546]", pids)
	}

	// Wrong port → not found.
	if _, err := parseLsofOutput(out, 99999); err == nil {
		t.Fatal("parseListeningPID(wrong port) expected error, got nil")
	}

	// Empty output → not found.
	if _, err := parseLsofOutput("", 36655); err == nil {
		t.Fatal("parseListeningPID(empty) expected error, got nil")
	}
}

// TestContainsPortAtBoundary guards against prefix collisions like
// port 4096 matching inside ":40960".
func TestContainsPortAtBoundary(t *testing.T) {
	if !containsPortAtBoundary(":36655 (LISTEN)", 36655) {
		t.Fatal("expected match for exact port")
	}
	if containsPortAtBoundary(":366550 (LISTEN)", 36655) {
		t.Fatal("should not match port prefix inside larger number")
	}
	if containsPortAtBoundary(":40960 (LISTEN)", 4096) {
		t.Fatal("should not match 4096 inside 40960")
	}
}

func TestParseListeningPID_SS(t *testing.T) {
	out := `LISTEN 0      128          127.0.0.1:36655    0.0.0.0:*    users:(("ssh",pid=20546,fd=6))
LISTEN 0      128          127.0.0.1:59190     0.0.0.0:*    users:(("ssh",pid=31001,fd=5))`
	pids, err := parseSsOutput(out, 59190)
	if err != nil {
		t.Fatalf("parseListeningPID() error: %v", err)
	}
	if len(pids) != 1 || pids[0] != 31001 {
		t.Fatalf("parseListeningPID() = %v, want [31001]", pids)
	}
}

// A socket shared by a master and its workers lists every holder; all of them
// must be returned, or the port stays bound after the first one is signalled.
func TestParseListeningPIDs_SharedSocket(t *testing.T) {
	ss := `LISTEN 0 511 127.0.0.1:3845 0.0.0.0:* users:(("node",pid=701,fd=9),("node",pid=702,fd=9))`
	pids, err := parseSsOutput(ss, 3845)
	if err != nil || len(pids) != 2 || pids[0] != 701 || pids[1] != 702 {
		t.Fatalf("parseSsOutput shared = %v, %v; want [701 702]", pids, err)
	}
	lsof := `COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME
node 701 james 9u IPv4 0x1 0t0 TCP 127.0.0.1:3845 (LISTEN)
node 702 james 9u IPv4 0x1 0t0 TCP 127.0.0.1:3845 (LISTEN)`
	pids, err = parseLsofOutput(lsof, 3845)
	if err != nil || len(pids) != 2 || pids[0] != 701 || pids[1] != 702 {
		t.Fatalf("parseLsofOutput shared = %v, %v; want [701 702]", pids, err)
	}
}

// TestParseListeningPID_NoListenerSentinel asserts "nothing listening" is the
// errNoListener sentinel, which reclaimStaleTunnelPort treats as benign. Any
// other inspection error must not match it, or it would be silently dropped.
func TestParseListeningPID_NoListenerSentinel(t *testing.T) {
	if _, err := parseLsofOutput("", 36655); !errors.Is(err, errNoListener) {
		t.Fatalf("parseLsofOutput(empty) error = %v, want errNoListener", err)
	}
	if _, err := parseSsOutput("", 59190); !errors.Is(err, errNoListener) {
		t.Fatalf("parseSsOutput(empty) error = %v, want errNoListener", err)
	}
}

// TestParseProcStatPPID covers the Linux /proc/<pid>/stat parser, including a
// comm field with spaces and parentheses. Malformed input must be an error,
// never a parent of 0.
func TestParseProcStatPPID(t *testing.T) {
	ppid, err := parseProcStatPPID("1234 (my (ssh) x) S 42 1234 1234 0 -1 4194560")
	if err != nil || ppid != 42 {
		t.Fatalf("parseProcStatPPID(valid) = %d, %v; want 42, nil", ppid, err)
	}
	for _, bad := range []string{"", "1234 ssh S 42", "1234 (ssh) S", "1234 (ssh) S x"} {
		if _, err := parseProcStatPPID(bad); err == nil {
			t.Fatalf("parseProcStatPPID(%q) expected error, got nil", bad)
		}
	}
}

// TestParentPID_CurrentProcess asserts the real parent is read for a live PID.
func TestParentPID_CurrentProcess(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("parent lookup not implemented on %s", runtime.GOOS)
	}
	ppid, err := parentPID(os.Getpid())
	if err != nil {
		// `ps` can be denied by a sandboxed exec runner, in which case the
		// parent lookup is UNAVAILABLE rather than wrong — skip, don't fail.
		if errors.Is(err, os.ErrPermission) {
			t.Skipf("parent lookup unavailable (ps denied): %v", err)
		}
		t.Fatalf("parentPID(self) error: %v", err)
	}
	if ppid != os.Getppid() {
		t.Fatalf("parentPID(self) = %d, want %d", ppid, os.Getppid())
	}
}

// TestParentPID_MissingPIDIsError asserts a PID that no longer exists yields an
// error rather than a silent parent of 0 (which reads as "not orphaned").
func TestParentPID_MissingPIDIsError(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("parent lookup not implemented on %s", runtime.GOOS)
	}
	if _, err := parentPID(999999); err == nil {
		t.Fatal("parentPID(nonexistent) expected error, got nil")
	}
}

// TestReclaimStaleTunnelPort_FreePort asserts reclaiming a free port is a
// no-op success. This is the common case and must never error.
func TestReclaimStaleTunnelPort_FreePort(t *testing.T) {
	// Use an ephemeral port that is extremely unlikely to be in use.
	port := 59998
	if err := reclaimStaleTunnelPort(port); err != nil {
		t.Fatalf("reclaimStaleTunnelPort(free port) error: %v", err)
	}
}

// TestIsOcodeSSHTunnel_NonExistentPID asserts a non-existent PID is not our tunnel.
func TestIsOcodeSSHTunnel_NonExistentPID(t *testing.T) {
	if got := isOcodeSSHTunnel(999999); got {
		t.Fatal("isOcodeSSHTunnel(nonexistent) expected false, got true")
	}
}

// TestReclaimStaleTunnelPort_ZeroAndNegative ports are no-ops.
func TestReclaimStaleTunnelPort_ZeroAndNegative(t *testing.T) {
	for _, port := range []int{0, -1, -36655} {
		if err := reclaimStaleTunnelPort(port); err != nil {
			t.Fatalf("reclaimStaleTunnelPort(%d) error: %v", port, err)
		}
	}
}

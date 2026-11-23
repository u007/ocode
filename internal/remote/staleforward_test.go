package remote

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Fixtures copied verbatim from a live host where a vanished client
// (161.142.139.239) left `ssh -R 3845` holding the port in a forward-only
// session with 460 bytes unacked and a running retransmit timer.
const (
	ssStaleListen = `State  Recv-Q Send-Q      Local Address:Port        Peer Address:Port Process
LISTEN 0      128                   127.0.0.1:3845       0.0.0.0:*    uid:1001 ino:1396246 sk:1f cgroup:/user.slice/user-1001.slice/session-63.scope <->
LISTEN 0      128                       [::1]:3845          [::]:*    uid:1001 ino:1396245 sk:20 cgroup:/user.slice/user-1001.slice/session-63.scope v6only:1 <->
`
	ssStaleProcs = `227138|0|sshd: james [priv]   
227283|1001|sshd: james          
`
	ssStaleRemoteHost = `RemoteHost=161.142.139.239
`
	ssStaleConns = `Recv-Q Send-Q            Local Address:Port          Peer Address:Port Process
0      0                      217.216.72.49:22      175.139.199.245:58475  timer:(keepalive,58min,0) uid:1001 ino:4682869
0      460                    217.216.72.49:22      161.142.139.239:3840  timer:(on,1min37sec,10) ino:1395109
`
	ssStaleFree = `State  Recv-Q Send-Q      Local Address:Port        Peer Address:Port Process
`
)

// staleForwardTransport scripts the four probes for the stale fixture, plus the
// port-free poll and the kill.
func staleForwardTransport() *fakeTransport {
	ft := newFakeTransport()
	ft.execResults["ss -ltnpe 2>/dev/null || true"] = ExecResult{Stdout: ssStaleListen}
	ft.execResults[sessionProcsCommand("63")] = ExecResult{Stdout: ssStaleProcs}
	ft.execResults["loginctl show-session 63 -p RemoteHost 2>/dev/null || true"] = ExecResult{Stdout: ssStaleRemoteHost}
	ft.execResults["ss -tnoe state established 2>/dev/null || true"] = ExecResult{Stdout: ssStaleConns}
	ft.execResults["ss -ltn 2>/dev/null || true"] = ExecResult{Stdout: ssStaleFree}
	return ft
}

func TestFindStaleReverseForwardReapsDeadSession(t *testing.T) {
	st, err := FindStaleReverseForward(context.Background(), staleForwardTransport(), 3845)
	if err != nil {
		t.Fatalf("want a stale forward, got %v", err)
	}
	if st.Session != "63" || st.Peer != "161.142.139.239" {
		t.Fatalf("wrong session/peer: %+v", st)
	}
	if len(st.PIDs) != 1 || st.PIDs[0] != 227283 {
		t.Fatalf("want only the non-root sshd pid 227283, got %v", st.PIDs)
	}
}

func TestFindStaleReverseForwardRefusesLivePeer(t *testing.T) {
	ft := staleForwardTransport()
	// The same peer reconnected: a healthy connection proves the client is alive.
	ft.execResults["ss -tnoe state established 2>/dev/null || true"] = ExecResult{Stdout: `Recv-Q Send-Q Local Address:Port Peer Address:Port Process
0      0      217.216.72.49:22   161.142.139.239:41111  timer:(keepalive,20sec,0) uid:1001 ino:1
0      460    217.216.72.49:22   161.142.139.239:3840   timer:(on,1min37sec,10) ino:2
`}
	_, err := FindStaleReverseForward(context.Background(), ft, 3845)
	if !errors.Is(err, ErrNoStaleForward) {
		t.Fatalf("a healthy peer must refuse the reap, got %v", err)
	}
}

func TestFindStaleReverseForwardRefusesSessionRunningAShell(t *testing.T) {
	ft := staleForwardTransport()
	ft.execResults[sessionProcsCommand("63")] = ExecResult{Stdout: `227138|0|sshd: james [priv]   
227283|1001|sshd: james          
227400|1001|-bash           
`}
	_, err := FindStaleReverseForward(context.Background(), ft, 3845)
	if !errors.Is(err, ErrNoStaleForward) {
		t.Fatalf("a session with a shell must refuse the reap, got %v", err)
	}
}

func TestFindStaleReverseForwardRefusesUnattributableListener(t *testing.T) {
	ft := staleForwardTransport()
	ft.execResults["ss -ltnpe 2>/dev/null || true"] = ExecResult{Stdout: `LISTEN 0      128                   127.0.0.1:3845       0.0.0.0:*
`}
	_, err := FindStaleReverseForward(context.Background(), ft, 3845)
	if !errors.Is(err, ErrNoStaleForward) {
		t.Fatalf("a listener with no cgroup must refuse the reap, got %v", err)
	}
}

func TestFindStaleReverseForwardRefusesNothingListening(t *testing.T) {
	ft := staleForwardTransport()
	ft.execResults["ss -ltnpe 2>/dev/null || true"] = ExecResult{Stdout: ssStaleFree}
	_, err := FindStaleReverseForward(context.Background(), ft, 3845)
	if !errors.Is(err, ErrNoStaleForward) {
		t.Fatalf("a free port must refuse the reap, got %v", err)
	}
}

func TestFindStaleReverseForwardRefusesAnotherPortOnTheSameListenLine(t *testing.T) {
	ft := staleForwardTransport()
	ft.execResults["ss -ltnpe 2>/dev/null || true"] = ExecResult{Stdout: `LISTEN 0 128 127.0.0.1:13845 0.0.0.0:* uid:1001 ino:1 cgroup:/user.slice/user-1001.slice/session-63.scope <->
`}
	_, err := FindStaleReverseForward(context.Background(), ft, 3845)
	if !errors.Is(err, ErrNoStaleForward) {
		t.Fatalf("port 13845 must not match 3845, got %v", err)
	}
}

func TestFindStaleReverseForwardRefusesDisagreeingFamilies(t *testing.T) {
	ft := staleForwardTransport()
	ft.execResults["ss -ltnpe 2>/dev/null || true"] = ExecResult{Stdout: `LISTEN 0 128 127.0.0.1:3845 0.0.0.0:* uid:1001 ino:1 cgroup:/user.slice/user-1001.slice/session-63.scope <->
LISTEN 0 128 [::1]:3845 [::]:* uid:1001 ino:2 cgroup:/user.slice/user-1001.slice/session-70.scope v6only:1 <->
`}
	_, err := FindStaleReverseForward(context.Background(), ft, 3845)
	if !errors.Is(err, ErrNoStaleForward) {
		t.Fatalf("families in different sessions must refuse, got %v", err)
	}
}

func TestFindStaleReverseForwardRefusesSessionWithoutAUserProcess(t *testing.T) {
	ft := staleForwardTransport()
	ft.execResults[sessionProcsCommand("63")] = ExecResult{Stdout: "227138|0|sshd: james [priv]   \n"}
	_, err := FindStaleReverseForward(context.Background(), ft, 3845)
	if !errors.Is(err, ErrNoStaleForward) {
		t.Fatalf("a session with only the root monitor must refuse, got %v", err)
	}
}

func TestFindStaleReverseForwardRefusesUnknownPeer(t *testing.T) {
	ft := staleForwardTransport()
	ft.execResults["loginctl show-session 63 -p RemoteHost 2>/dev/null || true"] = ExecResult{}
	_, err := FindStaleReverseForward(context.Background(), ft, 3845)
	if !errors.Is(err, ErrNoStaleForward) {
		t.Fatalf("a session with no remote host must refuse, got %v", err)
	}
}

func TestReapStaleReverseForwardKillsReportedPidOnly(t *testing.T) {
	ft := staleForwardTransport()
	st, err := ReapStaleReverseForward(context.Background(), ft, 3845)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if st.Session != "63" {
		t.Fatalf("wrong session: %+v", st)
	}
	var kills []string
	for _, c := range ft.execCalls {
		if strings.Contains(c, "kill -TERM") {
			kills = append(kills, c)
		}
	}
	if len(kills) != 1 || !strings.Contains(kills[0], "227283") || !strings.Contains(kills[0], "session-63.scope") {
		t.Fatalf("want exactly one guarded SIGTERM to 227283, got %v", kills)
	}
}

// The guard and the signal must be ONE remote command, and the guard must name
// the session: a pid recycled into another session is then skipped, not killed.
func TestStaleForwardKillCommandGuardsTheSession(t *testing.T) {
	cmd := staleForwardKillCommand(&StaleForward{Session: "63", PIDs: []int{227283}})
	for _, want := range []string{
		"/proc/$p/cgroup",
		"*session-63.scope*)",
		`kill -TERM "$p"`,
		"for p in 227283;",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("kill command missing %q: %s", want, cmd)
		}
	}
	if strings.Contains(cmd, "227138") {
		t.Fatalf("the root monitor must never appear in the kill command: %s", cmd)
	}
	if strings.Contains(cmd, "-9") || strings.Contains(cmd, "SIGKILL") {
		t.Fatalf("no SIGKILL path may exist: %s", cmd)
	}
}

func TestReapStaleReverseForwardSignalsNothingWhenNotStale(t *testing.T) {
	ft := staleForwardTransport()
	ft.execResults["ss -tnoe state established 2>/dev/null || true"] = ExecResult{Stdout: `Recv-Q Send-Q Local Address:Port Peer Address:Port Process
0      0      217.216.72.49:22   161.142.139.239:41111  timer:(keepalive,20sec,0) uid:1001 ino:1
`}
	if _, err := ReapStaleReverseForward(context.Background(), ft, 3845); !errors.Is(err, ErrNoStaleForward) {
		t.Fatalf("want ErrNoStaleForward, got %v", err)
	}
	for _, c := range ft.execCalls {
		if strings.Contains(c, "kill -TERM") {
			t.Fatalf("must not signal anything when not provably stale: %q", c)
		}
	}
}

func TestReapStaleReverseForwardReportsPortThatStaysBound(t *testing.T) {
	ft := staleForwardTransport()
	// The port never frees: the poll must give up, not loop forever.
	ft.execResults["ss -ltn 2>/dev/null || true"] = ExecResult{Stdout: ssStaleListen}
	_, err := ReapStaleReverseForward(context.Background(), ft, 3845)
	if err == nil || errors.Is(err, ErrNoStaleForward) {
		t.Fatalf("want a 'still bound' error, got %v", err)
	}
}

func TestReapStaleReverseForwardRefusesUnverifiableListenerList(t *testing.T) {
	ft := staleForwardTransport()
	// No `ss` on the host: the free check cannot claim the port was released.
	ft.execResults["ss -ltn 2>/dev/null || true"] = ExecResult{}
	_, err := ReapStaleReverseForward(context.Background(), ft, 3845)
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("want an 'unavailable' error, got %v", err)
	}
}

func TestReapStaleReverseForwardHonoursDeadline(t *testing.T) {
	ft := staleForwardTransport()
	ft.execResults["ss -ltn 2>/dev/null || true"] = ExecResult{Stdout: ssStaleListen}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := ReapStaleReverseForward(ctx, ft, 3845); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want the deadline to end the poll, got %v", err)
	}
}

func TestParseListenerSessionAndAddrHost(t *testing.T) {
	got, err := parseListenerSession(ssStaleListen, 3845)
	if err != nil || got != "63" {
		t.Fatalf("session = %q, %v", got, err)
	}
	cases := map[string]string{
		"161.142.139.239:3840": "161.142.139.239",
		"[::1]:22":             "::1",
		"127.0.0.1:22":         "127.0.0.1",
	}
	for in, want := range cases {
		if got := addrHost(in); got != want {
			t.Errorf("addrHost(%q) = %q, want %q", in, got, want)
		}
	}
}

// The session id is interpolated into a shell command, so a non-numeric cgroup
// id must be refused rather than run.
func TestSessionFromCgroupRejectsShellSyntax(t *testing.T) {
	fields := strings.Fields("uid:1001 ino:1 cgroup:/user.slice/user-1001.slice/session-6$(id).scope")
	if got := sessionFromCgroup(fields); got != "" {
		t.Fatalf("a non-numeric session id must be refused, got %q", got)
	}
	if got := sessionFromCgroup(strings.Fields("cgroup:/user.slice/user-1001.slice/session-17.scope")); got != "17" {
		t.Fatalf("a numeric session id must be accepted, got %q", got)
	}
}

// A retransmitting connection from a DIFFERENT peer must not condemn this
// session's peer.
func TestCheckPeerDeadIgnoresOtherPeers(t *testing.T) {
	out := `Recv-Q Send-Q Local Address:Port Peer Address:Port Process
0      900    217.216.72.49:22   203.0.113.9:2000  timer:(on,5sec,3) ino:1
`
	if err := checkPeerDead(out, "161.142.139.239"); !errors.Is(err, ErrNoStaleForward) {
		t.Fatalf("an unrelated dead peer must not match, got %v", err)
	}
}

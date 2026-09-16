package remote

import (
	"encoding/json"
	"strings"
	"testing"
)

// The desktop connect path (RemoteWorkspace.startFreshServer) must mirror the
// CLI path's stale-state-file fix (launchServerCmd, commit b418fb15): the
// launch command deletes the old serve.json SYNCHRONOUSLY before starting the
// new server. startFreshServer only runs because the discovered state was
// unusable (dead pid, version mismatch, unhealthy) — that stale file is often
// still on disk and still parses, so without the delete the poll loop can
// return the OLD server as the "fresh" one and the desktop silently
// reconnects to a version-mismatched remote server.
func TestWorkspaceStartFreshServerLaunchCmdDeletesStaleStateFile(t *testing.T) {
	rw := &RemoteWorkspace{
		Transport:  newFakeTransport(),
		RemotePath: "/home/user/proj",
	}
	rw.setStartFreshServerPollForTest(2, 1) // shrink: 2 × 1ms instead of 10s
	if _, err := rw.startFreshServer(); err == nil {
		t.Fatal("startFreshServer: got nil error with no state file, want poll timeout error")
	}

	var launchCmd string
	for _, c := range rw.Transport.(*fakeTransport).execCalls {
		if strings.Contains(c, "nohup") && strings.Contains(c, "serve --remote") {
			launchCmd = c
			break
		}
	}
	if launchCmd == "" {
		t.Fatalf("no launch command in exec calls: %v", rw.Transport.(*fakeTransport).execCalls)
	}
	statePath := shellQuotePath(rw.workspaceStatePath())
	if !strings.Contains(launchCmd, "rm -f "+statePath+"; ") {
		t.Errorf("launch command must delete the old state file synchronously (`;`-terminated, not part of the backgrounded && chain): %q", launchCmd)
	}
}

// rmAwareTransport models the real remote shell around the state file: cat
// sees the stale JSON until the launch command (containing the synchronous
// `rm -f <statePath>;`) runs; afterwards cat sees nothing — the file was
// deleted and this fake's fresh server never writes a new one. This makes the
// poll-loop contract directly observable: without the synchronous rm -f, the
// first poll iteration returns the stale server; with it, the flow must time
// out instead of adopting stale state.
type rmAwareTransport struct {
	*fakeTransport
	launched    bool
	staleStdout string
	catCmd      string
}

func (p *rmAwareTransport) Exec(command string) (ExecResult, error) {
	if strings.Contains(command, "nohup") && strings.Contains(command, "serve --remote") &&
		strings.Contains(command, "rm -f") {
		p.launched = true
		return p.fakeTransport.Exec(command)
	}
	if command == p.catCmd {
		if p.launched {
			return ExecResult{Stdout: ""}, nil // rm'd, never rewritten by this fake's server
		}
		return ExecResult{Stdout: p.staleStdout}, nil
	}
	return p.fakeTransport.Exec(command)
}

// startFreshServer must never return a stale state file as if it were the new
// server's: with the launch command deleting the file first, the poll loop can
// only ever observe nothing (keep polling) or the genuinely fresh state.
func TestWorkspaceStartFreshServerNeverReturnsStaleState(t *testing.T) {
	stale := ServeState{PID: 42, Port: 4096, Token: "oldtok", Version: "0.8.85"}
	staleData, _ := json.Marshal(stale)

	ft := newFakeTransport()
	catCmd := "cat " + shellQuotePath("~/.ocode/remote/serve.json") + " 2>/dev/null || true"
	tpt := &rmAwareTransport{
		fakeTransport: ft,
		staleStdout:   string(staleData),
		catCmd:        catCmd,
	}

	rw := &RemoteWorkspace{
		Transport:  tpt,
		RemotePath: "/home/user/proj",
	}
	rw.setStartFreshServerPollForTest(3, 1)
	state, err := rw.startFreshServer()
	if err == nil {
		t.Fatalf("startFreshServer returned %+v, want poll-timeout error (fresh server never writes its file in this fake)", state)
	}
	if state.Token == stale.Token || state.PID == stale.PID {
		t.Fatalf("returned the STALE server's state: %+v", state)
	}
}

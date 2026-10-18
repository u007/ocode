//go:build !windows

package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// terminal_new is Unix-only (the pty bridge is creack/pty), so its guard paths
// are covered here; the Windows stub returns a fixed refusal and is exercised
// by the tool-shape test instead.
func TestPulseTerminalNewRefusesBadArgs(t *testing.T) {
	h := pulseTestHandler(t)
	cases := []struct {
		name string
		args string
		want string
	}{
		{"missing project", `{}`, "project_path is required"},
		{"remote host", `{"project_path":"/tmp","host":"u@example.com"}`, "remote hosts is not supported"},
		{"out of root", `{"project_path":"/definitely/not/registered"}`, "not a registered project root"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runPulseTool(t, h, "terminal_new", tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("terminal_new(%s) err = %v, want contains %q", tc.args, err, tc.want)
			}
		})
	}
}

// TestPulseTerminalNewSpawnsAndRegisters is the end-to-end happy path: the tool
// must reserve + start a real pty, publish it into the session table and return
// a live id. This is the path the review found dead-on-arrival (completeCreate
// without a prior reserve). It needs a real pty, which some sandboxes deny, so
// it shares the terminal signal-test gate.
func TestPulseTerminalNewSpawnsAndRegisters(t *testing.T) {
	wantSignalTests(t)
	h := pulseTestHandler(t)
	projectDir := t.TempDir()
	h.workDir = projectDir
	h.SetTerminalAccessPolicy(false, true)
	t.Cleanup(func() { h.shutdownTerminals(t.Context()) })

	args, _ := json.Marshal(map[string]string{"project_path": projectDir})
	out, err := runPulseTool(t, h, "terminal_new", string(args))
	if err != nil {
		t.Fatalf("terminal_new: %v", err)
	}
	var res struct {
		TerminalID string `json:"terminal_id"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("decode result %q: %v", out, err)
	}
	if res.TerminalID == "" {
		t.Fatalf("no terminal_id in %q", out)
	}
	sess := h.terminalSessions.lookup(res.TerminalID)
	if sess == nil {
		t.Fatalf("terminal %s was not registered in the session table (dead-on-arrival regression)", res.TerminalID)
	}
	if sess.project != projectDir {
		t.Fatalf("terminal project = %q, want %q", sess.project, projectDir)
	}
	if h.termTabsStore != nil {
		found := false
		for _, term := range h.termTabsStore.Get(projectDir).Terminals {
			if term.ID == res.TerminalID {
				found = true
			}
		}
		if !found {
			t.Fatalf("terminal %s missing from the shared tab list", res.TerminalID)
		}
	}
}

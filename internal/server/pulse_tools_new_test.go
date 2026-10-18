package server

import (
	"strings"
	"testing"
)

// Behavioural coverage for the four Pulse write tools added alongside the
// existing session_send/session_command set. The spawn/attach happy paths need
// a real pty (/dev/ptmx), which CI sandboxes can deny, so these pin the guard
// paths that must never reach a spawn: argument validation, project
// confinement and the "act on another session" safety checks.

func TestPulseSessionNewRefusesBadArgs(t *testing.T) {
	h := pulseTestHandler(t)
	cases := []struct {
		name string
		args string
		want string
	}{
		{"missing project", `{"content":"hi"}`, "project_path is required"},
		{"missing content", `{"project_path":"/tmp"}`, "content is required"},
		{"remote host", `{"project_path":"/tmp","content":"hi","host":"u@example.com"}`, "remote hosts is not supported"},
		{"out of root", `{"project_path":"/definitely/not/registered","content":"hi"}`, "not a registered project root"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runPulseTool(t, h, "session_new", tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("session_new(%s) err = %v, want contains %q", tc.args, err, tc.want)
			}
		})
	}
}

func TestPulseTerminalKillRefusesNonLiveAndEmpty(t *testing.T) {
	h := pulseTestHandler(t)
	if _, err := runPulseTool(t, h, "terminal_kill", `{"terminal_id":"  "}`); err == nil ||
		!strings.Contains(err.Error(), "terminal_id is required") {
		t.Fatalf("empty id err = %v, want terminal_id is required", err)
	}
	if _, err := runPulseTool(t, h, "terminal_kill", `{"terminal_id":"term-does-not-exist"}`); err == nil ||
		!strings.Contains(err.Error(), "not live on this server") {
		t.Fatalf("non-live id err = %v, want not live on this server", err)
	}
}

func TestPulseSessionCloseRefusesUnsafeTargets(t *testing.T) {
	h := pulseTestHandler(t)
	cases := []struct {
		name string
		args string
		want string
	}{
		{"assistant's own session", `{"session_id":"pulse_test_chat"}`, "assistant's own session"},
		{"child session", `{"session_id":"ses_parent_child_worker"}`, "child session"},
		{"unknown session", `{"session_id":"ses_unknown_xyz"}`, "not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runPulseTool(t, h, "session_close", tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("session_close(%s) err = %v, want contains %q", tc.args, err, tc.want)
			}
		})
	}
}

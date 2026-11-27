package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// The path-confinement lease (tool.AcquireTemporaryAllowedPath) is granted only
// after an allow decision. These tests pin that: an outside absolute write must
// reach a human ask when no rule allows it, and the lease must not exist while
// Decide runs.
//
// The outside target lives under the REAL home, captured before setHomeTree
// redirects HOME, because temp dirs are allowed roots. The deny test never
// writes the file (its asker denies); the allow tests create their directory
// and remove it in t.Cleanup.

// pathLeaseRealHome returns the real user home, read before setHomeTree runs.
func pathLeaseRealHome(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skipf("no real home directory: %v", err)
	}
	return home
}

// pathLeaseDir is a per-test directory under the real home. It is removed at
// cleanup whether or not the test created it.
func pathLeaseDir(t *testing.T, realHome, suffix string) string {
	t.Helper()
	dir := filepath.Join(realHome, fmt.Sprintf("ocode-lease-probe-%d-%s", os.Getpid(), suffix))
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// pathLeaseAgent builds a normal-mode agent with the real write tool, a private
// HOME and a temp work dir that is not the target's directory.
func pathLeaseAgent(t *testing.T) *Agent {
	t.Helper()
	workDir := t.TempDir()
	setHomeTree(t, t.TempDir())
	a := newTestAgent(&scriptedSideQueryClient{}, []tool.Tool{tool.WriteTool{}}, nil, nil)
	a.SetWorkDir(workDir)
	a.permissions.SetMode(PermissionModeNormal)
	return a
}

func pathLeaseWriteArgs(t *testing.T, path, content string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"path": path, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func pathLeaseWriteInput(t *testing.T, path, content string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"file_path": path, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestPathLeaseNotVisibleToDecideWithoutRule: with no rule, an outside write
// must reach the human asker, and the lease must be absent while Decide runs.
func TestPathLeaseNotVisibleToDecideWithoutRule(t *testing.T) {
	realHome := pathLeaseRealHome(t)
	dir := pathLeaseDir(t, realHome, "deny")
	target := filepath.Join(dir, "x.txt")

	a := pathLeaseAgent(t)
	var asked int
	a.OnPermissionAsk = func(req PermissionRequest) PermissionResponse {
		asked++
		// Decide's own view of the target: the lease must not be in scope here.
		if a.permissions.IsPathWithinAllowedRoots(target) {
			t.Errorf("target %s is inside the allowed roots at ask time: the lease leaked into the decision", target)
		}
		return PermissionResponse{Level: PermissionDeny}
	}

	res, err := a.handleToolCallWithContext(context.Background(), "write", pathLeaseWriteArgs(t, target, "x"), nil, "lease-deny-1")
	if err != nil {
		t.Fatalf("handleToolCallWithContext: %v", err)
	}
	if asked != 1 {
		t.Fatalf("OnPermissionAsk called %d times, want 1 (result %q)", asked, res)
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatalf("%s was written although the human denied the ask", target)
	}
}

// TestPathLeaseDecideOnlyAndExecuteAfterPathRuleAllow: a path rule allows the
// directory. The decide-only bridge must allow without executing, the lease must
// be released afterwards, and a real execution must write the file.
func TestPathLeaseDecideOnlyAndExecuteAfterPathRuleAllow(t *testing.T) {
	realHome := pathLeaseRealHome(t)
	dir := pathLeaseDir(t, realHome, "rule")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "x.txt")

	a := pathLeaseAgent(t)
	a.permissions.SetPathRule("write", dir+"/**", PermissionAllow)
	a.OnPermissionAsk = func(req PermissionRequest) PermissionResponse {
		t.Errorf("asker called for a path-rule allow: %+v", req)
		return PermissionResponse{Level: PermissionDeny}
	}

	allowed, msg := a.decideClaudeCodeToolUse(context.Background(), "Write", pathLeaseWriteInput(t, target, "decided"), "toolu_lease_rule")
	if !allowed {
		t.Fatalf("decideClaudeCodeToolUse(Write, path-rule dir) = denied: %s", msg)
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatalf("decide-only bridge executed the write to %s", target)
	}
	// The decide-only call released its lease, so a bare confinement check on
	// the same path must fail again.
	if _, err := tool.ConfinedPath(tool.WithWorkDir(context.Background(), a.WorkDir()), target); err == nil {
		t.Fatalf("lease for %s leaked past the decide-only call", dir)
	}

	res, err := a.handleToolCallWithContext(context.Background(), "write", pathLeaseWriteArgs(t, target, "executed"), nil, "lease-rule-exec")
	if err != nil {
		t.Fatalf("write under path rule: %v (result %q)", err, res)
	}
	if strings.Contains(strings.ToLower(res), "outside") {
		t.Fatalf("write under path rule refused by confinement: %q", res)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read back %s: %v (result %q)", target, err, res)
	}
	if string(data) != "executed" {
		t.Fatalf("file content = %q, want %q", data, "executed")
	}
}

// TestPathLeaseHumanAllowExecutesOutsideWrite: a human Allow for the outside
// path must execute the write through the same lease.
func TestPathLeaseHumanAllowExecutesOutsideWrite(t *testing.T) {
	realHome := pathLeaseRealHome(t)
	dir := pathLeaseDir(t, realHome, "human")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "x.txt")

	a := pathLeaseAgent(t)
	var asked int
	a.OnPermissionAsk = func(req PermissionRequest) PermissionResponse {
		asked++
		return PermissionResponse{Level: PermissionAllow}
	}

	res, err := a.handleToolCallWithContext(context.Background(), "write", pathLeaseWriteArgs(t, target, "human"), nil, "lease-human-1")
	if err != nil {
		t.Fatalf("handleToolCallWithContext: %v (result %q)", err, res)
	}
	if asked != 1 {
		t.Fatalf("OnPermissionAsk called %d times, want 1", asked)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("human-approved write did not land: %v (result %q)", err, res)
	}
	if string(data) != "human" {
		t.Fatalf("file content = %q, want %q", data, "human")
	}
}

// TestPathLeaseDecideOnlyBridgeWithoutSurfaceDenies: with no rule and no bridge
// asker, the decide-only bridge must deny an outside write and not execute it.
func TestPathLeaseDecideOnlyBridgeWithoutSurfaceDenies(t *testing.T) {
	realHome := pathLeaseRealHome(t)
	dir := pathLeaseDir(t, realHome, "bridge")
	target := filepath.Join(dir, "x.txt")

	a := pathLeaseAgent(t)
	allowed, msg := a.decideClaudeCodeToolUse(context.Background(), "Write", pathLeaseWriteInput(t, target, "x"), "toolu_lease_bridge")
	if allowed {
		t.Fatal("decideClaudeCodeToolUse allowed an outside write with no rule and no approval surface")
	}
	if msg != claudeCodeNoSurfaceMessage {
		t.Fatalf("msg = %q, want %q", msg, claudeCodeNoSurfaceMessage)
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatalf("%s was written by the decide-only bridge", target)
	}
}

// TestPathLeaseHeldByAnotherCallIsInvisibleToDecide simulates a lease held by
// another in-flight call (a parallel call, a sub-agent, another session, or a
// TUI/server approve path). While it is held, Decide must still ask for an
// outside write and AllowedRoots must not list the leased folder. A persistent
// extra path must still be listed, because bash auto-allow depends on it.
func TestPathLeaseHeldByAnotherCallIsInvisibleToDecide(t *testing.T) {
	realHome := pathLeaseRealHome(t)
	dir := pathLeaseDir(t, realHome, "other-lease")
	target := filepath.Join(dir, "x.txt")

	setHomeTree(t, t.TempDir())
	// Snapshot the process-wide persistent roots and restore exactly them, so
	// this test does not wipe roots another test in the package installed.
	prior := tool.PersistentExtraAllowedRoots()
	t.Cleanup(func() { tool.SetPersistentExtraAllowedPaths(prior) })
	persistent := t.TempDir()
	tool.SetPersistentExtraAllowedPaths(append(append([]string(nil), prior...), persistent))

	if !tool.AcquireTemporaryAllowedPath(dir) {
		t.Fatalf("AcquireTemporaryAllowedPath(%s) refused", dir)
	}
	t.Cleanup(func() { tool.ReleaseTemporaryAllowedPath(dir) })

	pm := NewPermissionManager()
	pm.SetWorkDir(t.TempDir())
	pm.SetMode(PermissionModeNormal)

	if dec := pm.Decide("write", pathLeaseWriteArgs(t, target, "x")); dec.Level != PermissionAsk {
		t.Fatalf("Decide(write, outside path under another call's lease) = %s, want ask", dec.Level)
	}
	if pm.IsPathWithinAllowedRoots(target) {
		t.Fatalf("%s is inside the permission roots while only a lease covers it", target)
	}
	if !pm.IsPathWithinAllowedRoots(filepath.Join(persistent, "y.txt")) {
		t.Fatalf("persistent extra path %s dropped from the permission view", persistent)
	}
	for _, r := range pm.AllowedRoots() {
		if r == filepath.Clean(dir) {
			t.Fatalf("leased folder %s listed in AllowedRoots", dir)
		}
	}
	// The sandbox's execution-time writable roots DO include a live lease: a
	// human "allow once" for an out-of-scope bash write needs it writable while
	// the approved command runs. That set never feeds a decision.
	leasedWritable := false
	for _, r := range pm.AllowedRootsClassified() {
		if r.Path == filepath.Clean(dir) && r.Writable {
			leasedWritable = true
		}
	}
	if !leasedWritable {
		t.Fatalf("leased folder %s missing from the sandbox's writable roots", dir)
	}
}

package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// judgeStateForBash runs the bash command through the real state builder and
// returns the structured state the judge saw.
func judgeStateForBash(t *testing.T, a *Agent, h *typesafeJudgeHarness, command string) map[string]any {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"command": command})
	allowed, reason, _, consulted := a.consultPermissionModel("bash", args, nil)
	t.Logf("allowed=%v consulted=%v reason=%s", allowed, consulted, reason)
	if h.body == nil {
		t.Fatal("judge was never called: no request body captured")
	}
	state, _ := h.body["state"].(map[string]any)
	if state == nil {
		t.Fatalf("no state in judge body: %#v", h.body)
	}
	return state
}

// executedScriptsFromState returns the executed_scripts entries, or nil when the
// state carries none.
func executedScriptsFromState(state map[string]any) []map[string]any {
	raw, ok := state["executed_scripts"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func writeTestScript(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// The bug. The chat judge inlined executed scripts (askPermissionModel) but the
// structured TypeSafe path did not, and this judge has no read_file tool — so a
// bare script path was unreadable. The real event: Jev named
// truncated_or_unknown at 0.84, returned allow at 0.30, and the 0.85 floor
// forwarded an ordinary command to a human.
func TestTypesafeJudgeSeesDirectlyExecutedScriptSource(t *testing.T) {
	script := writeTestScript(t, filepath.Join(t.TempDir(), "gsearch.sh"),
		"#!/usr/bin/env bash\nset -euo pipefail\nsleep 4\necho done\n")

	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))
	scripts := executedScriptsFromState(judgeStateForBash(t, a, h, script+" novita"))
	if len(scripts) != 1 {
		t.Fatalf("executed_scripts = %d entries, want 1", len(scripts))
	}
	es := scripts[0]
	if got, _ := es["path"].(string); got != script {
		t.Errorf("path = %q, want %q", got, script)
	}
	if text, _ := es["text"].(string); !strings.Contains(text, "echo done") {
		t.Errorf("text does not carry the script body; got %q", text)
	}
	if trunc, _ := es["truncated"].(bool); trunc {
		t.Error("truncated = true for a small script")
	}
	if sha, _ := es["sha256"].(string); sha == "" {
		t.Error("sha256 is empty")
	}
	// readFileSnippet counts the trailing empty element after the final newline.
	if n, _ := es["total_lines"].(float64); n != 5 {
		t.Errorf("total_lines = %v, want 5", es["total_lines"])
	}
}

// The real-world shape: `chmod +x SCRIPT && SCRIPT args 2>&1 | head`. The script
// is NOT the first constituent, so a builder inspecting only the head command —
// as classifyInterpreterExecution does — ships nothing.
func TestTypesafeJudgeSeesScriptInChmodThenRunCompound(t *testing.T) {
	script := writeTestScript(t, filepath.Join(t.TempDir(), "gsearch.sh"),
		"#!/usr/bin/env bash\nsleep 4\necho done\n")

	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))
	state := judgeStateForBash(t, a, h, "chmod +x "+script+" && "+script+" \"novita\" 2>&1 | head -5")

	scripts := executedScriptsFromState(state)
	if len(scripts) != 1 {
		t.Fatalf("executed_scripts = %d entries, want 1 for the chmod-then-run compound", len(scripts))
	}
	if text, _ := scripts[0]["text"].(string); !strings.Contains(text, "echo done") {
		t.Errorf("text = %q, want the script body", text)
	}
}

// A shell wrapper is the other common spelling of the same command.
func TestTypesafeJudgeSeesScriptRunThroughShellWrapper(t *testing.T) {
	script := writeTestScript(t, filepath.Join(t.TempDir(), "s.sh"), "#!/bin/sh\necho wrapped\n")
	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))

	scripts := executedScriptsFromState(judgeStateForBash(t, a, h, "bash "+script))
	if len(scripts) != 1 {
		t.Fatalf("executed_scripts = %d entries, want 1 for `bash script`", len(scripts))
	}
	if text, _ := scripts[0]["text"].(string); !strings.Contains(text, "echo wrapped") {
		t.Errorf("text = %q, want the script body", text)
	}
}

// A relative spelling resolves against the agent working directory.
func TestTypesafeJudgeResolvesRelativeScriptAgainstWorkingDir(t *testing.T) {
	dir := t.TempDir()
	writeTestScript(t, filepath.Join(dir, "s.sh"), "#!/bin/sh\necho hi\n")
	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))
	a.SetWorkDir(dir)

	scripts := executedScriptsFromState(judgeStateForBash(t, a, h, "./s.sh"))
	if len(scripts) != 1 {
		t.Fatalf("executed_scripts = %d entries, want 1", len(scripts))
	}
	if got, _ := scripts[0]["path"].(string); got != filepath.Join(dir, "s.sh") {
		t.Errorf("path = %q, want %q", got, filepath.Join(dir, "s.sh"))
	}
}

// Every distinct script ships, bounded by max_context_sources, so one unreadable
// script cannot hide behind another readable one.
func TestTypesafeJudgeShipsEachScriptInCompound(t *testing.T) {
	dir := t.TempDir()
	one := writeTestScript(t, filepath.Join(dir, "one.sh"), "#!/bin/sh\necho one\n")
	two := writeTestScript(t, filepath.Join(dir, "two.sh"), "#!/bin/sh\necho two\n")

	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))
	if got := len(executedScriptsFromState(judgeStateForBash(t, a, h, one+" && "+two))); got != 2 {
		t.Fatalf("executed_scripts = %d entries, want 2", got)
	}

	setTestAutoPermissionConfig(a, func(c *config.AutoPermissionConfig) { c.MaxContextSources = 1 })
	if got := len(executedScriptsFromState(judgeStateForBash(t, a, h, one+" && "+two))); got != 1 {
		t.Errorf("executed_scripts = %d entries, want 1 when max_context_sources = 1", got)
	}
}

// Two realistic large scripts (~46 KB + ~9 KB) must reach the decision backend
// whole: under the per-script ceiling, untruncated, and shipped ONCE (not again
// inside project_context), so the state stays inside the shared 96 KB budget.
func TestTypesafeJudgeShipsLargeScriptsOnceWithinBudget(t *testing.T) {
	dir := t.TempDir()
	big := writeTestScript(t, filepath.Join(dir, "big.sh"),
		"#!/bin/sh\n"+strings.Repeat("echo 0123456789012345678901234567890123456789\n", 1000))
	mid := writeTestScript(t, filepath.Join(dir, "mid.sh"),
		"#!/bin/sh\n"+strings.Repeat("echo 0123456789012345678901234567890123456789\n", 200))

	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))
	state := judgeStateForBash(t, a, h, big+" && "+mid)

	scripts := executedScriptsFromState(state)
	if len(scripts) != 2 {
		t.Fatalf("executed_scripts = %d entries, want 2", len(scripts))
	}
	for _, sc := range scripts {
		if trunc, _ := sc["truncated"].(bool); trunc {
			t.Errorf("%v truncated; a script under the byte ceiling must ship whole", sc["path"])
		}
	}
	if ctx, _ := state["project_context"].(string); strings.Contains(ctx, "echo 0123456789") {
		t.Error("script text duplicated into project_context; it must travel once, in executed_scripts")
	}
	if _, projected := state[stateProjectionKey]; projected {
		t.Error("state was projected to previews; two large scripts must fit the budget whole")
	}
}

// An oversized script is marked truncated rather than silently presented whole.
// This mirrors verifyAutoGrant's refusal to auto-grant a partial view.
func TestTypesafeJudgeMarksOversizedScriptTruncated(t *testing.T) {
	script := writeTestScript(t, filepath.Join(t.TempDir(), "big.sh"),
		"#!/bin/sh\n"+strings.Repeat("echo padding line\n", 3000))

	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))

	scripts := executedScriptsFromState(judgeStateForBash(t, a, h, script))
	if len(scripts) != 1 {
		t.Fatalf("executed_scripts = %d entries, want 1", len(scripts))
	}
	if trunc, _ := scripts[0]["truncated"].(bool); !trunc {
		t.Error("truncated = false for a script over the per-script byte ceiling")
	}
	if n, _ := scripts[0]["total_lines"].(float64); n != 3002 {
		t.Errorf("total_lines = %v, want 3002 so the judge can see the view is partial", scripts[0]["total_lines"])
	}
}

// --- Negative cases: what must NOT gain source ---

// A credential-bearing file must never be shipped into an LLM prompt, even when
// the command executes it — that is the disclosure boundary that matters more than
// the convenience this feature adds. The scenario is contrived (nobody chmod +x's
// a private key), but the guard must hold regardless: it is what stops a future
// caller from widening the detector into secret-bearing paths.
func TestTypesafeJudgeOmitsSensitiveScript(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "id_rsa")
	writeTestScript(t, secret, "#!/bin/sh\necho not-really-a-key\n")

	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))

	if got := executedScriptsFromState(judgeStateForBash(t, a, h, secret)); got != nil {
		t.Errorf("executed_scripts shipped a secret-material file: %v", got)
	}
}

// A system binary carries no policy meaning: shipping its bytes is noise and a
// needless disclosure.
func TestTypesafeJudgeOmitsSourceForNonScriptBinaries(t *testing.T) {
	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))
	if got := executedScriptsFromState(judgeStateForBash(t, a, h, "/bin/ls -la /tmp")); got != nil {
		t.Errorf("executed_scripts present for /bin/ls: %v", got)
	}
}

// A nonexistent path must not fabricate source. An absent key is the honest
// signal, and truncated_or_unknown stays the correct answer.
func TestTypesafeJudgeOmitsSourceForMissingScript(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone.sh")
	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))
	if got := executedScriptsFromState(judgeStateForBash(t, a, h, missing)); got != nil {
		t.Errorf("executed_scripts present for a nonexistent path: %v", got)
	}
}

// An interpreter invocation keeps travelling in the interpreter block. The new
// key must not spend the context budget on the same bytes twice.
func TestTypesafeJudgeDoesNotDoubleShipInterpreterSource(t *testing.T) {
	script := writeTestScript(t, filepath.Join(t.TempDir(), "s.py"), "print('hi')\n")
	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))
	state := judgeStateForBash(t, a, h, "python3 "+script)
	if _, ok := state["interpreter"]; !ok {
		t.Error("interpreter block missing for a python3 invocation")
	}
	if got := executedScriptsFromState(state); got != nil {
		t.Errorf("executed_scripts double-shipped an interpreter's source: %v", got)
	}
}

// Attaching source is ADDITIVE CONTEXT ONLY and must not become a bypass: the
// deterministic truncation guard still refuses a partial view, whatever the judge
// said. This is the guarantee that makes shipping bounded source safe.
func TestTypesafeJudgeSourceDoesNotBypassTruncationGuard(t *testing.T) {
	script := writeTestScript(t, filepath.Join(t.TempDir(), "big.sh"),
		"#!/bin/sh\n"+strings.Repeat("echo padding line\n", 3000))

	a, _ := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.99))
	req := &PermissionRequest{ToolName: "bash", Scope: PermissionScopeBashPrefix, Rule: "bash.prefix." + script}

	if ok, why := a.verifyAutoGrant("bash", mustJSON(t, map[string]string{"command": script}), req); ok {
		t.Error("verifyAutoGrant allowed a truncated script; shipping bounded source became a bypass")
	} else if !strings.Contains(why, "truncated") {
		t.Errorf("refusal = %q, want the truncation refusal", why)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// scratch_dir_vars is an ocode-verified fact: only variables bound once, before
// use, from a bare `mktemp -d` are listed.
func TestTypesafeJudgeStateListsScratchDirVars(t *testing.T) {
	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))
	cases := []struct {
		cmd  string
		want []string
	}{
		{`tmp=$(mktemp -d) && echo hi > "$tmp/a"; rm -rf "$tmp"`, []string{"tmp"}},
		{`a=$(mktemp -d) && b=$(mktemp -d) && ls "$a" "$b"`, []string{"a", "b"}},
		{`tmp=$(mktemp -d) && tmp=/ && rm -rf "$tmp"`, nil},
		{`tmp=$HOME && rm -rf "$tmp"`, nil},
		{`tmp=$(mktemp -d -p /) && ls "$tmp"`, nil},
		{`ls`, nil},
	}
	for _, tc := range cases {
		state := judgeStateForBash(t, a, h, tc.cmd)
		got, _ := state["scratch_dir_vars"].([]any)
		var names []string
		for _, v := range got {
			names = append(names, v.(string))
		}
		if strings.Join(names, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%q: scratch_dir_vars = %v, want %v", tc.cmd, names, tc.want)
		}
		// The instruction rides along only when a variable was detected.
		if _, has := state["scratch_dir_note"]; has != (len(tc.want) > 0) {
			t.Errorf("%q: scratch_dir_note present = %v, want %v", tc.cmd, has, len(tc.want) > 0)
		}
	}
}

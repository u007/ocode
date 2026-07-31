# Terminal-Bench Harness + Token Accounting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give ocode a measurable Terminal-Bench score and per-run token accounting on `opencode-go/deepseek-v4-flash`, so prompt and context changes can be judged against numbers instead of intuition.

**Architecture:** Three layers. First, `ocode run` learns to report token totals by assigning the agent's existing `OnUsage` callback to an accumulator and emitting a trailing `usage` event. Second, a Python Terminal-Bench adapter (`AbstractInstalledAgent`) copies a Linux ocode binary into each task container, runs ocode headless, and reports the run's real token counts through TB's own `AgentResult`. Third, a sweep script runs a frozen task subset with pinned versions so runs stay comparable.

**Tech Stack:** Go 1.26 (`internal/runcli`), Python 3 stdlib + `terminal-bench==0.2.18`, Docker (Podman Desktop), `uvx`.

**Spec:** `docs/superpowers/specs/2026-07-30-terminal-bench-harness-design.md`

## Global Constraints

- Terminal-Bench is pinned to `terminal-bench==0.2.18`. Never invoke bare `uvx --from terminal-bench`.
- Dataset is pinned to `terminal-bench-core==0.1.1`. Version is part of the `--dataset` value; there is **no** `--dataset-version` flag.
- The ocode binary under test is built from a known commit. Never install a floating/`latest` binary.
- Model under test is `opencode-go/deepseek-v4-flash`.
- No new CLI flags on `ocode run`. Token reporting rides the existing `-format json` and `-format summary` paths.
- No fallback values or silent recovery. A missing token count is reported as unknown, never defaulted to zero.
- Every caught error is logged with what was attempted and why it failed. No empty `except`/`catch` blocks.
- Python code uses only the standard library plus `terminal_bench` itself. No new dependencies.
- Go formatting via `gofmt`. Type-check/build with `go build ./...`; test with `go test ./...`.
- Anything left stubbed or deferred gets an entry in `TODO.md`.

---

### Task 1: Token accounting in `ocode run` (done)

Accumulate token usage across every model call in a headless run and surface it in both machine-readable and human-readable output.

**Files:**
- Create: `internal/runcli/usage.go`
- Create: `internal/runcli/usage_test.go`
- Modify: `internal/runcli/run.go` (agent construction ~line 218; output dispatch ~lines 277-285)
- Modify: `internal/runcli/summary.go:28` (signature) and its metadata render block (~line 108)
- Modify: `internal/runcli/summary_test.go:15,30` (test helper only)

**Interfaces:**
- Consumes: `agent.Agent.OnUsage func(inputTokens, outputTokens int64)` (declared `internal/agent/agent.go:191`, installed onto the client per-call at `agent.go:503`).
- Produces:
  - `type usageTotals struct` with methods `record(inputTokens, outputTokens int64)` and `snapshot() (input int64, output int64, calls int)`
  - `func emitUsageEvent(sessionID, modelName string, totals *usageTotals) error`
  - `outputSummary(messages []agent.Message, sessionID, modelName string, startTime time.Time, totals *usageTotals) error` (one new trailing parameter)

Background for the implementer: the callback fires from the SSE-reading goroutine, so `usageTotals` must be mutex-guarded. For this model the provider reports each response's *absolute* prompt/completion counts (OpenAI-shaped payload), so summing across calls yields total tokens billed for the run. Do not write delta-handling logic; the Anthropic-style delta path is not used by `opencode-go/deepseek-v4-flash`.

- [x] **Step 1: Write the failing test**

Create `internal/runcli/usage_test.go`:

```go
package runcli

import (
	"bytes"
	"encoding/json"
	"os"
	"sync"
	"testing"
)

func TestUsageTotals_recordSumsAndCountsCalls(t *testing.T) {
	var totals usageTotals
	totals.record(100, 20)
	totals.record(250, 35)

	in, out, calls := totals.snapshot()
	if in != 350 {
		t.Errorf("input tokens = %d, want 350", in)
	}
	if out != 55 {
		t.Errorf("output tokens = %d, want 55", out)
	}
	if calls != 2 {
		t.Errorf("model calls = %d, want 2", calls)
	}
}

func TestUsageTotals_recordIsConcurrencySafe(t *testing.T) {
	var totals usageTotals
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			totals.record(10, 1)
		}()
	}
	wg.Wait()

	in, out, calls := totals.snapshot()
	if in != 1000 || out != 100 || calls != 100 {
		t.Errorf("got in=%d out=%d calls=%d, want 1000/100/100", in, out, calls)
	}
}

func TestEmitUsageEvent_emitsSingleJSONLine(t *testing.T) {
	var totals usageTotals
	totals.record(1200, 300)
	totals.record(800, 150)

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		outC <- buf.String()
	}()

	emitErr := emitUsageEvent("sess-9", "opencode-go/deepseek-v4-flash", &totals)

	w.Close()
	os.Stdout = old
	out := <-outC

	if emitErr != nil {
		t.Fatalf("emitUsageEvent: %v", emitErr)
	}

	var ev struct {
		Type         string `json:"type"`
		SessionID    string `json:"sessionID"`
		InputTokens  int64  `json:"input_tokens"`
		OutputTokens int64  `json:"output_tokens"`
		TotalTokens  int64  `json:"total_tokens"`
		ModelCalls   int    `json:"model_calls"`
		Model        string `json:"model"`
	}
	if err := json.Unmarshal([]byte(out), &ev); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if ev.Type != "usage" {
		t.Errorf("type = %q, want usage", ev.Type)
	}
	if ev.SessionID != "sess-9" {
		t.Errorf("sessionID = %q, want sess-9", ev.SessionID)
	}
	if ev.InputTokens != 2000 {
		t.Errorf("input_tokens = %d, want 2000", ev.InputTokens)
	}
	if ev.OutputTokens != 450 {
		t.Errorf("output_tokens = %d, want 450", ev.OutputTokens)
	}
	if ev.TotalTokens != 2450 {
		t.Errorf("total_tokens = %d, want 2450", ev.TotalTokens)
	}
	if ev.ModelCalls != 2 {
		t.Errorf("model_calls = %d, want 2", ev.ModelCalls)
	}
	if ev.Model != "opencode-go/deepseek-v4-flash" {
		t.Errorf("model = %q, want opencode-go/deepseek-v4-flash", ev.Model)
	}
}
```

- [x] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/runcli/ -run 'TestUsageTotals|TestEmitUsageEvent' -v`

Expected: FAIL — compile error, `undefined: usageTotals` and `undefined: emitUsageEvent`.

- [x] **Step 3: Write the minimal implementation**

Create `internal/runcli/usage.go`:

```go
package runcli

import (
	"encoding/json"
	"os"
	"sync"
)

// usageTotals accumulates token usage across every model call in a headless
// run. The provider's usage callback fires from the SSE-reading goroutine, so
// every field is guarded.
type usageTotals struct {
	mu           sync.Mutex
	inputTokens  int64
	outputTokens int64
	modelCalls   int
}

// record adds one model call's absolute token counts to the running totals.
func (u *usageTotals) record(inputTokens, outputTokens int64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.inputTokens += inputTokens
	u.outputTokens += outputTokens
	u.modelCalls++
}

func (u *usageTotals) snapshot() (int64, int64, int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.inputTokens, u.outputTokens, u.modelCalls
}

// emitUsageEvent writes the run's single trailing usage event as one JSON line.
func emitUsageEvent(sessionID, modelName string, totals *usageTotals) error {
	in, out, calls := totals.snapshot()
	return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
		"type":          "usage",
		"sessionID":     sessionID,
		"input_tokens":  in,
		"output_tokens": out,
		"total_tokens":  in + out,
		"model_calls":   calls,
		"model":         modelName,
	})
}
```

- [x] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/runcli/ -run 'TestUsageTotals|TestEmitUsageEvent' -v`

Expected: PASS (3 tests).

- [x] **Step 5: Wire the accumulator into the run**

In `internal/runcli/run.go`, immediately after `ag.LoadExternalToolsWithMCP(cfg)` (line 219), add:

```go
	var totals usageTotals
	ag.OnUsage = func(inputTokens, outputTokens int64) {
		totals.record(inputTokens, outputTokens)
	}
```

Then change the output dispatch block (lines 277-285) to:

```go
	if opts.format == "json" {
		if err := outputJSONEvents(resp, opts.sessionID); err != nil {
			return err
		}
		return emitUsageEvent(opts.sessionID, modelStr, &totals)
	}

	if opts.format == "summary" {
		// Pass the full history (original messages + new messages) so that
		// every tool call across the entire run is captured.
		return outputSummary(allMessages, opts.sessionID, modelStr, startTime, &totals)
	}
```

- [x] **Step 6: Add the token line to the human summary**

In `internal/runcli/summary.go`, change the signature at line 28 to:

```go
func outputSummary(messages []agent.Message, sessionID, modelName string, startTime time.Time, totals *usageTotals) error {
```

In the metadata render block, directly after the `@ Model:` line, add:

```go
	if in, out, calls := totals.snapshot(); calls > 0 {
		fmt.Printf("  # Tokens:   %d in / %d out (%d calls)\n", in, out, calls)
	}
```

The `calls > 0` guard exists so a run that produced no usage prints nothing rather than a misleading `0 in / 0 out`.

- [x] **Step 7: Update the test helper for the new signature**

In `internal/runcli/summary_test.go`, change line 30 inside `captureOutputSummary` to:

```go
	err = outputSummary(messages, sessionID, modelName, startTime, &usageTotals{})
```

and change the direct call at line 420 to:

```go
		_ = outputSummary(msgs, "", "", time.Now(), &usageTotals{})
```

Leave `captureOutputSummary`'s own signature and all 12 of its callers untouched — this is deliberately a two-line change, not a sweep through the test file.

- [x] **Step 8: Add a regression test for the summary token line**

Append to `internal/runcli/usage_test.go`:

```go
func TestOutputSummary_printsTokenLineWhenCallsRecorded(t *testing.T) {
	var totals usageTotals
	totals.record(500, 75)

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		outC <- buf.String()
	}()

	sumErr := outputSummary(nil, "", "", time.Now(), &totals)

	w.Close()
	os.Stdout = old
	out := <-outC

	if sumErr != nil {
		t.Fatalf("outputSummary: %v", sumErr)
	}
	if !strings.Contains(out, "500 in / 75 out (1 calls)") {
		t.Errorf("summary missing token line, got:\n%s", out)
	}
}

func TestOutputSummary_omitsTokenLineWhenNoCalls(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		outC <- buf.String()
	}()

	sumErr := outputSummary(nil, "", "", time.Now(), &usageTotals{})

	w.Close()
	os.Stdout = old
	out := <-outC

	if sumErr != nil {
		t.Fatalf("outputSummary: %v", sumErr)
	}
	if strings.Contains(out, "Tokens:") {
		t.Errorf("summary should omit token line with no calls, got:\n%s", out)
	}
}
```

Add `"strings"` and `"time"` to the import block of `usage_test.go`.

- [x] **Step 9: Run the full package test suite**

Run: `go test ./internal/runcli/ -v`

Expected: PASS — the 5 new tests plus every pre-existing `summary_test.go` and `run_test.go` test.

- [x] **Step 10: Verify the whole module still builds**

Run: `gofmt -l internal/runcli/ && go build ./...`

Expected: no output from either command.

- [x] **Step 11: Commit**

```bash
git add internal/runcli/usage.go internal/runcli/usage_test.go internal/runcli/run.go internal/runcli/summary.go internal/runcli/summary_test.go
git commit -m "feat(runcli): report per-run token totals in json and summary output"
```

---

### Task 2: Token log parsing for the adapter (done)

A pure-Python module that reads an ocode JSONL run log and extracts the trailing usage event. Kept free of any `terminal_bench` import so it is testable without Docker or the harness.

**Files:**
- Create: `bench/__init__.py` (empty)
- Create: `bench/terminal_bench/__init__.py` (empty)
- Create: `bench/terminal_bench/usage.py`
- Create: `bench/terminal_bench/test_usage.py`

**Interfaces:**
- Consumes: the `usage` JSON line emitted by `emitUsageEvent` from Task 1.
- Produces:
  - `class RunUsage` with attributes `input_tokens: int`, `output_tokens: int`, `model_calls: int`
  - `def parse_run_usage(log_path) -> RunUsage | None` — returns `None` when the log is absent or contains no usage event, never a zero-filled `RunUsage`.

Background: returning `None` rather than zeros is a hard requirement. A crashed or timed-out task must be reported as *unknown* cost; a zero would silently pull the mean token count down and make a broken run look cheap.

- [x] **Step 1: Write the failing test**

Create `bench/terminal_bench/test_usage.py`:

```python
import json
import tempfile
import unittest
from pathlib import Path

from bench.terminal_bench.usage import RunUsage, parse_run_usage


class ParseRunUsageTest(unittest.TestCase):
    def _write(self, lines):
        tmp = tempfile.NamedTemporaryFile(
            mode="w", suffix=".jsonl", delete=False
        )
        for line in lines:
            tmp.write(json.dumps(line) + "\n")
        tmp.close()
        return Path(tmp.name)

    def test_reads_trailing_usage_event(self):
        path = self._write([
            {"type": "text", "part": {"type": "text", "text": "working"}},
            {"type": "tool_use", "part": {"tool": "bash"}},
            {
                "type": "usage",
                "input_tokens": 2000,
                "output_tokens": 450,
                "total_tokens": 2450,
                "model_calls": 7,
            },
        ])
        usage = parse_run_usage(path)
        self.assertEqual(usage, RunUsage(2000, 450, 7))

    def test_returns_none_when_no_usage_event(self):
        path = self._write([
            {"type": "text", "part": {"type": "text", "text": "crashed"}},
        ])
        self.assertIsNone(parse_run_usage(path))

    def test_returns_none_when_file_missing(self):
        self.assertIsNone(parse_run_usage(Path("/nonexistent/ocode-run.jsonl")))

    def test_ignores_malformed_lines(self):
        path = Path(
            tempfile.NamedTemporaryFile(suffix=".jsonl", delete=False).name
        )
        path.write_text(
            "not json at all\n"
            + json.dumps({
                "type": "usage",
                "input_tokens": 10,
                "output_tokens": 2,
                "model_calls": 1,
            })
            + "\n"
        )
        self.assertEqual(parse_run_usage(path), RunUsage(10, 2, 1))

    def test_last_usage_event_wins(self):
        path = self._write([
            {"type": "usage", "input_tokens": 1, "output_tokens": 1,
             "model_calls": 1},
            {"type": "usage", "input_tokens": 99, "output_tokens": 9,
             "model_calls": 3},
        ])
        self.assertEqual(parse_run_usage(path), RunUsage(99, 9, 3))


if __name__ == "__main__":
    unittest.main()
```

- [x] **Step 2: Run the test to verify it fails**

Run: `cd /Users/james/www/ocode && python3 -m unittest bench.terminal_bench.test_usage -v`

Expected: FAIL — `ModuleNotFoundError: No module named 'bench'` (the `__init__.py` files and `usage.py` do not exist yet).

- [x] **Step 3: Create the package files and implementation**

Create empty `bench/__init__.py` and `bench/terminal_bench/__init__.py`.

Create `bench/terminal_bench/usage.py`:

```python
"""Parse token usage from an ocode headless run log.

Deliberately free of any terminal_bench import so it can be tested without
Docker or the harness.
"""

import json
import logging
from dataclasses import dataclass
from pathlib import Path

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class RunUsage:
    input_tokens: int
    output_tokens: int
    model_calls: int


def parse_run_usage(log_path) -> "RunUsage | None":
    """Return the last usage event in an ocode JSONL run log.

    Returns None when the log is missing or has no usage event. A run whose
    cost is unknown must stay unknown -- returning zeros here would make a
    crashed run look free and quietly drag down any token average.
    """
    path = Path(log_path)
    try:
        raw = path.read_text()
    except OSError as err:
        logger.warning("could not read ocode run log %s: %s", path, err)
        return None

    found = None
    for line in raw.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError as err:
            logger.debug("skipping malformed line in %s: %s", path, err)
            continue
        if isinstance(event, dict) and event.get("type") == "usage":
            found = event

    if found is None:
        logger.warning("no usage event found in ocode run log %s", path)
        return None

    return RunUsage(
        input_tokens=int(found["input_tokens"]),
        output_tokens=int(found["output_tokens"]),
        model_calls=int(found["model_calls"]),
    )
```

- [x] **Step 4: Run the test to verify it passes**

Run: `cd /Users/james/www/ocode && python3 -m unittest bench.terminal_bench.test_usage -v`

Expected: PASS (5 tests).

- [x] **Step 5: Commit**

```bash
git add bench/__init__.py bench/terminal_bench/__init__.py bench/terminal_bench/usage.py bench/terminal_bench/test_usage.py
git commit -m "feat(bench): parse token usage from ocode run logs"
```

---

### Task 3: Host credential and binary resolution (done)

Resolve the `opencode-go` API key and the Linux ocode binary on the host, failing loudly at adapter construction rather than mid-run after Docker spin-up.

**Files:**
- Create: `bench/terminal_bench/hostenv.py`
- Create: `bench/terminal_bench/test_hostenv.py`

**Interfaces:**
- Produces:
  - `def resolve_api_key(auth_path=None, environ=None) -> str` — raises `RuntimeError` when no credential is found.
  - `def resolve_binary(dist_dir, arch=None) -> Path` — raises `RuntimeError` naming `make build-linux` when the binary is absent.

Background: `OPENCODE_API_KEY` is **not** exported in the user's shell. The credential lives in `~/.local/share/opencode/auth.json` under the `opencode-go` key (see `internal/auth/store.go:146`). An exported env var still wins if present, matching `auth.HydrateEnv`'s precedence (`internal/auth/providers.go:277`). Forwarding the key as an env var into the container is sufficient — no auth file needs seeding, because an already-set env var takes highest precedence inside the container too.

- [x] **Step 1: Write the failing test**

Create `bench/terminal_bench/test_hostenv.py`:

```python
import json
import tempfile
import unittest
from pathlib import Path

from bench.terminal_bench.hostenv import resolve_api_key, resolve_binary


class ResolveApiKeyTest(unittest.TestCase):
    def _auth_file(self, payload):
        tmp = tempfile.NamedTemporaryFile(
            mode="w", suffix=".json", delete=False
        )
        json.dump(payload, tmp)
        tmp.close()
        return Path(tmp.name)

    def test_reads_key_from_auth_store(self):
        path = self._auth_file({"opencode-go": {"type": "api", "key": "sk-abc"}})
        self.assertEqual(resolve_api_key(auth_path=path, environ={}), "sk-abc")

    def test_environment_variable_wins(self):
        path = self._auth_file({"opencode-go": {"type": "api", "key": "sk-abc"}})
        self.assertEqual(
            resolve_api_key(
                auth_path=path, environ={"OPENCODE_API_KEY": "sk-env"}
            ),
            "sk-env",
        )

    def test_raises_when_no_credential_anywhere(self):
        path = self._auth_file({"openai": {"type": "api", "key": "sk-other"}})
        with self.assertRaises(RuntimeError) as ctx:
            resolve_api_key(auth_path=path, environ={})
        self.assertIn("opencode-go", str(ctx.exception))

    def test_raises_when_auth_file_missing(self):
        with self.assertRaises(RuntimeError):
            resolve_api_key(auth_path=Path("/nonexistent/auth.json"), environ={})


class ResolveBinaryTest(unittest.TestCase):
    def test_returns_matching_arch_binary(self):
        dist = Path(tempfile.mkdtemp())
        binary = dist / "ocode-linux-arm64"
        binary.write_text("#!/bin/sh\n")
        self.assertEqual(resolve_binary(dist, arch="arm64"), binary)

    def test_raises_with_build_instruction_when_absent(self):
        dist = Path(tempfile.mkdtemp())
        with self.assertRaises(RuntimeError) as ctx:
            resolve_binary(dist, arch="amd64")
        self.assertIn("make build-linux", str(ctx.exception))


if __name__ == "__main__":
    unittest.main()
```

- [x] **Step 2: Run the test to verify it fails**

Run: `cd /Users/james/www/ocode && python3 -m unittest bench.terminal_bench.test_hostenv -v`

Expected: FAIL — `ModuleNotFoundError: No module named 'bench.terminal_bench.hostenv'`.

- [x] **Step 3: Write the implementation**

Create `bench/terminal_bench/hostenv.py`:

```python
"""Resolve host-side prerequisites for the ocode Terminal-Bench adapter.

Both resolvers raise at adapter construction time so a misconfigured host
fails before Docker spins up, not after tokens have been spent.
"""

import json
import os
import platform
from pathlib import Path

PROVIDER_ID = "opencode-go"
ENV_VAR = "OPENCODE_API_KEY"
DEFAULT_AUTH_PATH = Path.home() / ".local" / "share" / "opencode" / "auth.json"


def resolve_api_key(auth_path=None, environ=None) -> str:
    """Return the opencode-go API key.

    Precedence matches auth.HydrateEnv (internal/auth/providers.go): an
    already-set environment variable wins over the stored credential.
    """
    environ = os.environ if environ is None else environ
    from_env = environ.get(ENV_VAR)
    if from_env:
        return from_env

    path = Path(DEFAULT_AUTH_PATH if auth_path is None else auth_path)
    try:
        store = json.loads(path.read_text())
    except OSError as err:
        raise RuntimeError(
            f"cannot read the opencode auth store at {path}: {err}. "
            f"Export {ENV_VAR} or log in with `ocode` first."
        ) from err
    except json.JSONDecodeError as err:
        raise RuntimeError(
            f"the opencode auth store at {path} is not valid JSON: {err}"
        ) from err

    entry = store.get(PROVIDER_ID) or {}
    key = entry.get("key") or entry.get("api_key") or entry.get("apiKey")
    if not key:
        raise RuntimeError(
            f"no {PROVIDER_ID} credential in {path} and {ENV_VAR} is unset. "
            f"Run `ocode` and authenticate the {PROVIDER_ID} provider."
        )
    return key


def resolve_binary(dist_dir, arch=None) -> Path:
    """Return the Linux ocode binary to copy into the task container."""
    if arch is None:
        machine = platform.machine().lower()
        arch = "arm64" if machine in ("arm64", "aarch64") else "amd64"

    binary = Path(dist_dir) / f"ocode-linux-{arch}"
    if not binary.is_file():
        raise RuntimeError(
            f"missing {binary}. Build it first with `make build-linux` and "
            f"copy ocode-linux-{arch} into {dist_dir}."
        )
    return binary
```

- [x] **Step 4: Run the test to verify it passes**

Run: `cd /Users/james/www/ocode && python3 -m unittest bench.terminal_bench.test_hostenv -v`

Expected: PASS (6 tests).

- [x] **Step 5: Commit**

```bash
git add bench/terminal_bench/hostenv.py bench/terminal_bench/test_hostenv.py
git commit -m "feat(bench): resolve opencode-go credential and linux binary on host"
```

---

### Task 4: The Terminal-Bench adapter (done)

The `AbstractInstalledAgent` subclass that installs ocode in the task container, runs it headless, and reports real token counts through TB's own result record.

**Files:**
- Create: `bench/terminal_bench/ocode_agent.py`
- Create: `bench/terminal_bench/ocode-setup.sh.j2`
- Create: `bench/terminal_bench/test_ocode_agent.py`
- Modify: `.gitignore`

**Interfaces:**
- Consumes: `parse_run_usage` (Task 2); `resolve_api_key`, `resolve_binary` (Task 3); `emitUsageEvent`'s output format (Task 1).
- Produces: `class OcodeAgent(AbstractInstalledAgent)` importable as `bench.terminal_bench.ocode_agent:OcodeAgent`.

Verified facts the implementer must not re-derive:
- `AbstractInstalledAgent` requires `_env`, `_install_agent_script_path`, and `_run_agent_commands(instruction)`.
- `_get_templated_script_path("ocode-setup.sh.j2")` renders a **Jinja2** template from the agent's own directory. The file must carry the `.j2` suffix.
- `perform_task(instruction, session, logging_dir)` in the base class copies the install script, sources env, runs the install, detects `INSTALL_FAIL_STATUS`, then sends the run commands. It returns `AgentResult(total_input_tokens=0, total_output_tokens=0)` — hardcoded zeros, which is exactly what this override replaces.
- `session.copy_to_container(paths, container_dir=..., container_filename=...)` copies arbitrary host files in. This is how the binary gets there — no download, no release asset, no host file server.
- The container agent-logs directory is `/agent-logs`, mounted to the host `logging_dir`.
- Commands run in a **tmux pane**, so stdout is not a captured pipe. The redirect to `/agent-logs/ocode-run.jsonl` is mandatory.
- `ocode run -p` is a complete agentic run: `Agent.Step` loops until a response has no tool calls, bounded by `maxSteps` (default 100) — see `internal/agent/agent.go:843`.

- [x] **Step 1: Write the failing test**

Create `bench/terminal_bench/test_ocode_agent.py`. These tests exercise the pieces that do not need Docker: env assembly and the token-reporting override.

```python
import json
import shlex
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from bench.terminal_bench.ocode_agent import OcodeAgent


def _make_agent(tmp_dist):
    with mock.patch(
        "bench.terminal_bench.ocode_agent.resolve_api_key", return_value="sk-test"
    ), mock.patch(
        "bench.terminal_bench.ocode_agent.resolve_binary",
        return_value=Path(tmp_dist) / "ocode-linux-arm64",
    ):
        return OcodeAgent(model_name="opencode-go/deepseek-v4-flash")


class OcodeAgentEnvTest(unittest.TestCase):
    def test_env_carries_key_and_model(self):
        agent = _make_agent(tempfile.mkdtemp())
        env = agent._env
        self.assertEqual(env["OPENCODE_API_KEY"], "sk-test")
        self.assertEqual(env["OCODE_MODEL"], "opencode-go/deepseek-v4-flash")

    def test_run_command_is_headless_json_and_redirected(self):
        agent = _make_agent(tempfile.mkdtemp())
        commands = agent._run_agent_commands("fix the failing test")
        self.assertEqual(len(commands), 1)
        command = commands[0].command
        self.assertIn("ocode run", command)
        self.assertIn("-yolo", command)
        self.assertIn("-format json", command)
        self.assertIn("/agent-logs/ocode-run.jsonl", command)
        self.assertIn(shlex.quote("fix the failing test"), command)


class OcodeAgentTokenReportingTest(unittest.TestCase):
    def _run_with_log(self, log_lines):
        logging_dir = Path(tempfile.mkdtemp())
        if log_lines is not None:
            (logging_dir / "ocode-run.jsonl").write_text(
                "\n".join(json.dumps(line) for line in log_lines) + "\n"
            )

        agent = _make_agent(tempfile.mkdtemp())
        session = mock.MagicMock()
        base_result = mock.MagicMock(
            total_input_tokens=0, total_output_tokens=0, failure_mode="none"
        )
        with mock.patch(
            "bench.terminal_bench.ocode_agent."
            "AbstractInstalledAgent.perform_task",
            return_value=base_result,
        ):
            return agent.perform_task("task", session, logging_dir), session

    def test_reports_real_token_counts(self):
        result, _ = self._run_with_log([
            {"type": "text", "part": {"type": "text", "text": "hi"}},
            {"type": "usage", "input_tokens": 3000, "output_tokens": 700,
             "model_calls": 9},
        ])
        self.assertEqual(result.total_input_tokens, 3000)
        self.assertEqual(result.total_output_tokens, 700)

    def test_leaves_zeros_when_usage_missing(self):
        result, _ = self._run_with_log(None)
        self.assertEqual(result.total_input_tokens, 0)
        self.assertEqual(result.total_output_tokens, 0)

    def test_preserves_failure_mode_from_base_class(self):
        result, _ = self._run_with_log([
            {"type": "usage", "input_tokens": 1, "output_tokens": 1,
             "model_calls": 1},
        ])
        self.assertEqual(result.failure_mode, "none")

    def test_copies_binary_into_container(self):
        _, session = self._run_with_log([
            {"type": "usage", "input_tokens": 1, "output_tokens": 1,
             "model_calls": 1},
        ])
        session.copy_to_container.assert_called_once()
        kwargs = session.copy_to_container.call_args.kwargs
        self.assertEqual(kwargs["container_filename"], "ocode")


if __name__ == "__main__":
    unittest.main()
```

- [x] **Step 2: Run the test to verify it fails**

Run: `cd /Users/james/www/ocode && uvx --from terminal-bench==0.2.18 python -m unittest bench.terminal_bench.test_ocode_agent -v`

Expected: FAIL — `ModuleNotFoundError: No module named 'bench.terminal_bench.ocode_agent'`.

Note the runner change: this module imports `terminal_bench`, so it needs the pinned TB environment. Tasks 2 and 3 stay on plain `python3` because they have no such import.

- [x] **Step 3: Write the install script template**

Create `bench/terminal_bench/ocode-setup.sh.j2`:

```bash
#!/bin/bash
# Install ocode inside the Terminal-Bench task container. The binary is copied
# in by the adapter before this runs; this script only places it on PATH and
# writes a minimal config.
set -euo pipefail

install -m 0755 /installed-agent/ocode /usr/local/bin/ocode

mkdir -p "$HOME"
cat > "$HOME/ocodeconfig.json" <<'JSON'
{
  "permissions": {
    "auto": {
      "enabled": false
    }
  }
}
JSON

mkdir -p /agent-logs

ocode version
```

The trailing `ocode version` is load-bearing: it makes a broken or wrong-architecture binary fail the install script, which the base class reports as `AGENT_INSTALLATION_FAILED` rather than letting it masquerade as a task failure.

- [x] **Step 4: Write the adapter**

Create `bench/terminal_bench/ocode_agent.py`:

```python
"""Terminal-Bench adapter for ocode.

Runs ocode headless inside each task container and reports the run's real
token cost through TB's own AgentResult, whose base implementation hardcodes
zeros.
"""

import logging
import shlex
from pathlib import Path

from terminal_bench.agents.installed_agents.abstract_installed_agent import (
    AbstractInstalledAgent,
)
from terminal_bench.terminal.models import TerminalCommand

from bench.terminal_bench.hostenv import resolve_api_key, resolve_binary
from bench.terminal_bench.usage import parse_run_usage

logger = logging.getLogger(__name__)

CONTAINER_LOG = "/agent-logs/ocode-run.jsonl"
CONTAINER_ERR = "/agent-logs/ocode-run.err"
DIST_DIR = Path(__file__).parent / "dist"


class OcodeAgent(AbstractInstalledAgent):
    @staticmethod
    def name() -> str:
        return "ocode"

    def __init__(self, model_name: str, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._model_name = model_name
        # Resolve both host prerequisites now so a misconfigured host fails
        # before Docker spins up.
        self._api_key = resolve_api_key()
        self._binary = resolve_binary(DIST_DIR)

    @property
    def _env(self) -> dict[str, str]:
        return {
            "OPENCODE_API_KEY": self._api_key,
            "OCODE_MODEL": self._model_name,
        }

    @property
    def _install_agent_script_path(self) -> Path:
        return self._get_templated_script_path("ocode-setup.sh.j2")

    def _run_agent_commands(self, instruction: str) -> list[TerminalCommand]:
        escaped = shlex.quote(instruction)
        return [
            TerminalCommand(
                command=(
                    f'ocode run -yolo -format json -m "$OCODE_MODEL" '
                    f"-p {escaped} > {CONTAINER_LOG} 2> {CONTAINER_ERR}"
                ),
                min_timeout_sec=0.0,
                max_timeout_sec=float("inf"),
                block=True,
                append_enter=True,
            ),
        ]

    def perform_task(self, instruction, session, logging_dir=None):
        # The binary must land in the container before the install script runs.
        session.copy_to_container(
            self._binary,
            container_dir="/installed-agent",
            container_filename="ocode",
        )

        result = super().perform_task(instruction, session, logging_dir)

        if logging_dir is None:
            logger.warning(
                "no logging_dir given; cannot report ocode token usage"
            )
            return result

        usage = parse_run_usage(Path(logging_dir) / "ocode-run.jsonl")
        if usage is None:
            # Deliberately leave the base class's zeros in place and say so.
            # Inventing a number here would make a crashed run look cheap.
            logger.warning(
                "no usage event for this task; token cost is unknown"
            )
            return result

        result.total_input_tokens = usage.input_tokens
        result.total_output_tokens = usage.output_tokens
        return result
```

- [x] **Step 5: Run the tests to verify they pass**

Run: `cd /Users/james/www/ocode && uvx --from terminal-bench==0.2.18 python -m unittest bench.terminal_bench.test_ocode_agent -v`

Expected: PASS (6 tests).

- [x] **Step 6: Ignore build and run artifacts**

Append to `.gitignore`:

```
bench/terminal_bench/dist/
bench/terminal_bench/runs/
```

- [x] **Step 7: Run every bench test together**

Run: `cd /Users/james/www/ocode && uvx --from terminal-bench==0.2.18 python -m unittest discover -s bench -t . -p 'test_*.py' -v`

Expected: PASS — all 17 tests from Tasks 2, 3, and 4.

- [x] **Step 8: Commit**

```bash
git add bench/terminal_bench/ocode_agent.py bench/terminal_bench/ocode-setup.sh.j2 bench/terminal_bench/test_ocode_agent.py .gitignore
git commit -m "feat(bench): add terminal-bench adapter for ocode"
```

---

### Task 5: Smoke test the harness end to end (in progress)

Prove the adapter works against real containers before any subset or baseline work. This is the task that resolves the design's one unproven assumption: whether task containers can reach `opencode.ai`.

**Files:**
- Create: `bench/terminal_bench/sweep.sh`
- Create: `bench/terminal_bench/README.md`
- Modify: `TODO.md`

**Interfaces:**
- Consumes: `OcodeAgent` (Task 4).
- Produces: `bench/terminal_bench/sweep.sh <label> [extra tb args...]` — runs the frozen subset with every version pinned.

- [x] **Step 1: Build the Linux binary**

```bash
cd /Users/james/www/ocode
make build-linux
mkdir -p bench/terminal_bench/dist
cp ocode-linux-arm64 ocode-linux-amd64 bench/terminal_bench/dist/
```

Expected: both files exist under `bench/terminal_bench/dist/`.

Note: the task containers are `linux/amd64` unless the image says otherwise, while an Apple-silicon host reports `arm64`. If the smoke run fails at `ocode version` inside the container, that architecture mismatch is the first thing to check — pass `arch="amd64"` explicitly to `resolve_binary` in `OcodeAgent.__init__` and re-run.

- [x] **Step 2: Confirm Docker is available**

Run: `docker version --format '{{.Server.Version}}'`

Expected: a version string (Podman Desktop provides the `docker` CLI).

- [~] **Step 3: Run two tasks with a single attempt** (in progress)

```bash
cd /Users/james/www/ocode
uvx --from terminal-bench==0.2.18 tb run \
  --agent-import-path bench.terminal_bench.ocode_agent:OcodeAgent \
  --model opencode-go/deepseek-v4-flash \
  --dataset terminal-bench-core==0.1.1 \
  -t chess-best-move -t count-dataset-tokens \
  --n-attempts 1 --n-concurrent 2 \
  --global-agent-timeout-sec 900 \
  --output-path bench/terminal_bench/runs/smoke
```

- [~] **Step 4: Verify the five smoke criteria** (containers running, deferred)

Check each one and write the answers into the README in Step 6:

1. **Install succeeded** — no `AGENT_INSTALLATION_FAILED` in the results. Proves the binary copy and architecture are right.
2. **Egress works** — `bench/terminal_bench/runs/smoke/**/ocode-run.err` contains no connection/DNS errors. **This is the design's one unproven assumption.** If it fails, stop and report: the installed-agent approach cannot work and the design needs revisiting.
3. **Log landed** — `ocode-run.jsonl` exists on the host and its last line has `"type":"usage"`.
4. **Tokens propagated** — the TB results record shows non-zero `total_input_tokens`.
5. **No prompt-hang and session save worked** — the run terminated on its own rather than hitting the 900s timeout, and `ocode-run.err` shows no `save session` error (that write happens *after* the work, so an unwritable `HOME` would fail the run only after tokens were spent).

Useful commands:

```bash
grep -l '"type":"usage"' bench/terminal_bench/runs/smoke/**/ocode-run.jsonl
grep -ri 'connection\|dns\|refused\|save session' bench/terminal_bench/runs/smoke/**/ocode-run.err
grep -r 'total_input_tokens' bench/terminal_bench/runs/smoke/ | head
```

- [x] **Step 5: Write the sweep script**

Create `bench/terminal_bench/sweep.sh`:

```bash
#!/bin/bash
# Run the frozen Terminal-Bench subset against ocode.
# Usage: bench/terminal_bench/sweep.sh <label> [extra tb args...]
set -euo pipefail

if [ $# -lt 1 ]; then
  echo "usage: $0 <label> [extra tb args...]" >&2
  exit 1
fi

LABEL="$1"
shift

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

SUBSET="bench/terminal_bench/subset.txt"
if [ ! -s "$SUBSET" ]; then
  echo "error: $SUBSET is empty; freeze the task subset first" >&2
  exit 1
fi

TASK_ARGS=()
while read -r task; do
  [ -z "$task" ] && continue
  case "$task" in \#*) continue ;; esac
  TASK_ARGS+=(-t "$task")
done < "$SUBSET"

# Record exactly what produced these numbers. A floating harness or binary
# makes two runs incomparable.
echo "label:      $LABEL"
echo "tb:         0.2.18"
echo "dataset:    terminal-bench-core==0.1.1"
echo "ocode:      $(./ocode version 2>&1 | head -1)"
echo "commit:     $(git rev-parse --short HEAD)"

uvx --from terminal-bench==0.2.18 tb run \
  --agent-import-path bench.terminal_bench.ocode_agent:OcodeAgent \
  --model opencode-go/deepseek-v4-flash \
  --dataset terminal-bench-core==0.1.1 \
  --n-attempts 3 --n-concurrent 4 \
  --global-agent-timeout-sec 900 \
  --output-path "bench/terminal_bench/runs/$LABEL" \
  "${TASK_ARGS[@]}" \
  "$@"
```

Then: `chmod +x bench/terminal_bench/sweep.sh`

- [x] **Step 6: Write the README**

Create `bench/terminal_bench/README.md` covering: prerequisites (`make build-linux` into `dist/`, Docker running, an authenticated `opencode-go` provider); how to run a sweep (`./sweep.sh <label>`); the pinned versions and why they are pinned; the smoke-test results from Step 4; and the rule that the subset is frozen and re-picking it invalidates every prior comparison.

- [x] **Step 7: Record deferred work**

Add to `TODO.md`:

```markdown
- **Bench: cache-token reporting.** `ocode run`'s `usage` event omits
  cache-read/cache-write counts because `Agent.OnUsage` carries only input and
  output. The gateway does return `prompt_cache_hit_tokens`. Widening the
  callback signature would let the bench measure prompt-cache effectiveness
  directly. See `docs/superpowers/specs/2026-07-30-terminal-bench-harness-design.md`.
- **Bench: `subset.txt` is unfrozen.** Populated in the baseline task; until
  then `sweep.sh` refuses to run.
```

- [x] **Step 8: Commit**

```bash
git add bench/terminal_bench/sweep.sh bench/terminal_bench/README.md TODO.md
git commit -m "feat(bench): add sweep script and smoke-test results"
```

---

### Task 6: Freeze the subset and record the baseline (in progress)

Turn the working harness into a number that later work is measured against.

**Files:**
- Create: `bench/terminal_bench/subset.txt`
- Modify: `bench/terminal_bench/README.md`

**Interfaces:**
- Consumes: `sweep.sh` (Task 5).
- Produces: the frozen subset and the recorded baseline every optimization is compared against.

- [x] **Step 1: List the dataset's tasks**

Run: `ls ~/.cache/terminal-bench/terminal-bench-core/0.1.1`

Expected: 80 task directories.

- [x] **Step 2: Choose 12 tasks by stratified sample**

Group the 80 task IDs by domain prefix/theme (build/compile, debugging, file manipulation, networking, data processing, ML, security, and so on). Take roughly one or two from each group, preferring tasks that finished in reasonable time during the smoke run. Avoid picking only easy tasks — a subset with no headroom cannot show improvement.

Write the chosen IDs, one per line, into `bench/terminal_bench/subset.txt`, with a header comment:

```
# FROZEN SUBSET -- do not re-pick.
# Changing these IDs invalidates every prior comparison.
# Chosen 2026-07-30 by stratified sample across terminal-bench-core==0.1.1.
```

- [~] **Step 3: Run the baseline** (deferred — pending smoke test completion)

```bash
cd /Users/james/www/ocode
bench/terminal_bench/sweep.sh baseline
```

This runs 12 tasks × 3 attempts. Expect it to take a while.

- [~] **Step 4: Record the baseline in the README** (deferred)

Add a "Baseline" section to `bench/terminal_bench/README.md` containing: the date, the ocode commit, the pinned TB and dataset versions, and a per-task table of pass rate across the 3 attempts, mean input tokens, mean output tokens, and mean model calls.

Report the overall pass rate **with its spread across attempts**, never as a bare number. With 12 tasks, one task flipping moves the score by roughly 8 points — a single number invites chasing noise.

Any task with unknown token cost is listed as unknown and **counted**, not dropped. Dropping it would flatter the average.

- [~] **Step 5: Commit** (deferred — blocked on baseline run)

---

## After this plan

The harness now produces a comparable score and a per-task token cost. Optimization work starts from the levers in the spec, in this order, changing one thing at a time and keeping it only if tokens drop without a score regression outside the measured spread:

1. Anything that reduces **turn count** — highest leverage, because reasoning tokens dominate output and each turn re-pays that tax in full.
2. System-prompt weight for the DeepSeek family (`internal/agent/prompts/deepseek.txt`, `deepseek-v4-flash.OCODE.md`) — with the caveat that trimming guidance which prevents flailing can raise turn count and cost more than it saves.
3. Tool schema and description bloat.
4. Tool-output truncation limits.
5. Prompt-cache hit rate (measured at 0 in every probe).
6. Compaction thresholds (`internal/agent/compact.go`).
7. Skill injection.

`reasoning_effort` is **not** on this list: measured across n=5 per level, its effect was smaller than the sample noise.

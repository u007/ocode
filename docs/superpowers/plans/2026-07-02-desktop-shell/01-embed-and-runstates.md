# Part 01 — Shared web embed + Server.RunStates()

Global constraints from `INDEX.md` apply (pure-Go main binary, pinned deps, no handler behavior changes, build/test + commit per task).

---

### Task 1: Move the SPA embed to `web/embed.go`

`//go:embed all:web/dist` currently lives in root `main.go` (lines 27–28) with helper `webFS()` (main.go:36–42). `go:embed` cannot use `..` paths, so `cmd/ocode-desktop` (a second main package) cannot reuse it. Create a shared embed package both binaries import.

**Files:**
- Create: `web/embed.go`
- Create: `web/embed_test.go`
- Modify: `main.go:27-42` (remove `webAssets` embed + `webFS()` body; delegate to new package)

**Interfaces:**
- Consumes: nothing.
- Produces: `package web` at `github.com/u007/ocode/web` with `func FS() fs.FS` returning the built SPA rooted so `index.html` is at the FS root, or `nil` when unavailable (existing `webFS()` semantics — callers treat nil as "web UI not built").

- [ ] **Step 1: Write the failing test**

`web/embed_test.go`:

```go
package web

import "testing"

func TestFSContainsIndexHTML(t *testing.T) {
	f := FS()
	if f == nil {
		t.Fatal("FS() returned nil; web/dist must be built and embedded")
	}
	if _, err := f.Open("index.html"); err != nil {
		t.Fatalf("index.html not found in embedded SPA: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/`
Expected: FAIL — `no Go files in .../web` (package doesn't exist yet).

- [ ] **Step 3: Create `web/embed.go`**

```go
// Package web embeds the built SPA (dist/) so any binary in this module can
// serve it. The embed lives here because go:embed cannot reach ../web/dist
// from a second main package (e.g. cmd/ocode-desktop).
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var assets embed.FS

// FS returns the built SPA rooted at dist/ (index.html at the root), or nil
// when the sub-tree is unavailable. Nil mirrors the historical main.go
// behaviour: callers treat it as "web UI not built" and degrade gracefully.
// intentionally not logged: fs.Sub over a compile-time embed cannot fail at
// runtime unless dist/ was empty at build time, which the test guards.
func FS() fs.FS {
	f, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil
	}
	return f
}
```

Note: `web/dist` must exist at build time (already true — root `main.go` embeds it today).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/`
Expected: PASS

- [ ] **Step 5: Point `main.go` at the shared package**

In root `main.go`:
1. Delete lines 27–28 (`//go:embed all:web/dist` + `var webAssets embed.FS`).
2. Add import `"github.com/u007/ocode/web"`.
3. Replace the `webFS()` body (lines 36–42) with:

```go
func webFS() fs.FS {
	return web.FS()
}
```

4. If `embed` and/or `fs` imports become unused in `main.go`, remove exactly the ones your change orphaned (`bundledSkills`/`bundledModelConfigs` still use `embed`, so only check `fs` usage elsewhere in the file before removing it).

- [ ] **Step 6: Build and test everything**

Run: `go build ./... && go test ./web/ ./internal/server/`
Expected: builds clean; tests PASS.

- [ ] **Step 7: Commit**

```bash
git add web/embed.go web/embed_test.go main.go
git commit -m "refactor: move SPA embed to shared web package

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: `Server.RunStates()` snapshot accessor

The desktop shell needs run state (badge count, finished-run notifications) across ALL active agents. No push bus exists; `HandleRunsStream` polls per-session registries. Add an exported, cross-session snapshot on `Handler`/`Server` reusing the exact registries the HTTP handlers use.

Relevant existing code (do not change it): `internal/server/handler_runs.go` — `activeAgentForRuns` (line ~93) resolves the `/rc` agent first, else `h.agents[sessionID]`; `agent.AgentRun` has `ID, Name string`, `Status agent.RunStatus` (`"running" | "done" | "failed"`), `Err string`; top-level runs come from `ag.Runs().Snapshot()`.

**Files:**
- Create: `internal/server/run_states.go`
- Create: `internal/server/run_states_test.go`

**Interfaces:**
- Consumes: `Handler.rc *RCBridge`, `Handler.agents map[string]*agentSession` (existing unexported state), `agent.RunRunning`.
- Produces (Parts 02/03 rely on these exact names):

```go
type RunState struct {
	SessionID string // "" for the /rc-bridged TUI agent
	ID        string
	Name      string
	Ended     bool
	Failed    bool
}
func (h *Handler) RunStates() []RunState
func (s *Server) RunStates() []RunState
```

- [ ] **Step 1: Write the failing test**

`internal/server/run_states_test.go` — build agents the same way existing handler tests do. First read `internal/server/handler_runs_test.go` and copy its fixture pattern for creating a `Handler` with a fake/real agent session (reuse its helper if one exists; do not invent a new fixture style). The test asserts:

```go
package server

import "testing"

func TestRunStatesEmptyWhenNoAgents(t *testing.T) {
	h := NewHandler()
	states := h.RunStates()
	if len(states) != 0 {
		t.Fatalf("expected no run states, got %d", len(states))
	}
}

// TestRunStatesReportsSessionRuns: using the fixture pattern from
// handler_runs_test.go, register an agent session with one running and one
// done run, then assert: len==2; the running run has Ended==false; the done
// run has Ended==true, Failed==false; SessionID matches the session key.
// Sort/order must follow registry (chronological) order, same as
// runsSnapshot. Write this test concretely against the fixtures you found.
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestRunStates -v`
Expected: FAIL — `h.RunStates undefined`.

- [ ] **Step 3: Implement `internal/server/run_states.go`**

```go
package server

import "github.com/u007/ocode/internal/agent"

// RunState is a minimal cross-package view of one top-level agent run.
// The desktop shell polls this for dock-badge counts and finished-run
// notifications; it deliberately excludes transcripts (see agentRunDTO for
// the full web-facing shape).
type RunState struct {
	SessionID string // "" for the /rc-bridged TUI agent
	ID        string
	Name      string
	Ended     bool
	Failed    bool
}

// RunStates returns one entry per top-level run across the /rc agent (if
// any) and every per-session server agent, in registry (chronological)
// order per agent. Sessions iterate in sorted key order so output is
// stable. (Listing rule: sorted; unpaginated per spec — bounded small set.)
func (h *Handler) RunStates() []RunState {
	h.mu.Lock()
	rc := h.rc
	sessionIDs := make([]string, 0, len(h.agents))
	for id := range h.agents {
		sessionIDs = append(sessionIDs, id)
	}
	agents := make(map[string]*agent.Agent, len(h.agents))
	for id, as := range h.agents {
		agents[id] = as.agent
	}
	h.mu.Unlock()

	sort.Strings(sessionIDs)

	out := []RunState{}
	appendRuns := func(sessionID string, ag *agent.Agent) {
		if ag == nil || ag.Runs() == nil {
			return
		}
		for _, r := range ag.Runs().Snapshot() {
			status := r.Status
			out = append(out, RunState{
				SessionID: sessionID,
				ID:        r.ID,
				Name:      r.Name,
				Ended:     status != agent.RunRunning,
				Failed:    status == agent.RunFailed,
			})
		}
	}

	if rc != nil {
		appendRuns("", rc.Agent())
	}
	for _, id := range sessionIDs {
		appendRuns(id, agents[id])
	}
	return out
}

// RunStates exposes the handler snapshot at the Server level for in-process
// consumers (the desktop shell).
func (s *Server) RunStates() []RunState {
	return s.handler.RunStates()
}
```

Add the missing `"sort"` import. IMPORTANT: `r.Status` is read outside the run's own mutex in `runsSnapshot` too — but check `agent_runs.go` for `statusValue()` (line ~95); if a locked accessor exists, use `r.statusValue()`-equivalent exported accessor instead of the raw field. If only the raw field is exported, match `buildRunDTO`'s existing access pattern exactly.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/server/ -run TestRunStates -v`
Expected: PASS. Then full package: `go test ./internal/server/` — PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/run_states.go internal/server/run_states_test.go
git commit -m "feat(server): exported RunStates snapshot for desktop shell

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

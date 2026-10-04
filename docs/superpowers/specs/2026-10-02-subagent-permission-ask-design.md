# Sub-agent permission asks on web/desktop — design

Date: 2026-10-02
Status: approved in chat (§1–§4 accepted). Awaiting spec review.

## Problem

A sub-agent that hits a permission ask on the web/desktop server silently
aborts mid-work, and its run is recorded as **done**. There is no error, no
dialog, and nothing for the user to answer.

Observed in `ses_2026-10-02-094643-b014b5ba` (agent-run-4, child session
`…_child_context_2026-10-02-141511`). The child ran 1m46s, returned only a
preamble, and wrote nothing:

```
Now let me examine the current code to re-derive anchors.
Now let me mechanically verify every anchor against the working tree:
```

Its transcript **ends** on an unresolved sentinel — a `python3 -c` anchor dump
tripped `bash.interpreter.python`, and the TypeSafe judge leaned allow at 0.76,
under the 0.85 floor:

```
PERMISSION_ASK:{"tool_name":"bash", …, "rule":"bash.interpreter.python",
"deny_reason":"TypeSafe judge leaned allow but confidence 0.76 is below the 0.85 floor"}
```

The parent re-dispatched with *"You must WRITE the doc. Your previous run
returned only a preamble and made no edit."*

## Why it is invisible

Not because nothing scans child transcripts — because **the parent's turn lock
is held for the child's entire lifetime**.

- `runTurn` takes `as.mu.Lock()` at `internal/server/agent_session.go:1061` and
  holds it for the whole turn (`defer as.mu.Unlock()`). A synchronous sub-agent
  dispatch blocks inside `Step` on that same goroutine
  (`internal/agent/subagent.go:871`), so the lock is held until the child ends.
- `livePendingAsks` reads the session's messages under `as.mu.TryLock()` and
  returns **nil** when the lock is busy
  (`internal/server/handler_session_state.go:133-143`).
- The web dialog is driven by exactly that payload: `useChat.ts:140` opens it
  from `state.pending_asks.permissions` on `GET /api/sessions/:id/state`.

So for a sub-agent ask the poll cannot see the ask for exactly as long as the
ask exists. The existing comment — *"while a turn holds the lock it cannot yet
be paused on an ask"* — is true for main-agent asks and precisely inverted for
child asks.

Two more facts close the remaining gaps:

- The sentinel path in `Agent.Step` is only taken when `a.OnPermissionAsk` is
  nil (`internal/agent/agent.go:3669-3685`).
- `SetSubAgentPermAsker` has exactly one real install site in the whole tree,
  `internal/tui/model.go:18842`. The server never installs one; `subagent.go:552`
  and `advisor_tool.go:358` merely propagate the parent's (nil) asker.

## Non-goals

- **The TUI is unaffected.** It installs a working asker
  (`tui/model.go:18842-18859`) and this change does not touch that path. The
  attribution wrapper (§Components #2) is a no-op when the name is already set.
- **ACP and `runcli` are unaffected.** ACP installs its own asker; `runcli`'s
  is opt-in (`internal/runcli/run.go:267`). Either way both keep their current
  behaviour — callback or sentinel — because this change adds an install site
  only in the server.
- **The parent-cancel gap is explicitly NOT fixed here.** See §Known limits.
- **`runcli`'s opt-in asker override and RC-bridged sessions are untouched.**
  Bridged sessions never reach `buildAgentSession`, which is what prevents a
  second asker competing with the TUI's.

## Design

### Invariants

1. **The registry has its own mutex.** The asker and the resolve path never take
   `as.mu`. This is the whole reason the design works: the parent holds `as.mu`
   for the entire turn by design, so anything the turn itself must expose has to
   live outside it.
2. **The child blocks.** That is what delivers the answer to the goroutine that
   needs it, without inventing a re-`Step` protocol that must survive
   `shutdownTransient` (`agent.go:6003`). It mirrors the TUI's asker line for
   line.
3. **`livePendingAsks` merges rather than replaces.** The registry is read
   unconditionally; the existing message-scan keeps its current `TryLock`
   semantics for main-agent asks, so the "running turn ⇒ no ask" invariant is
   preserved for the case it was written for.

### Flow

```
browser              server (turn goroutine)                    child agent
   | POST /chat          |                                          |
   |-------------------->| runTurn: as.mu.Lock()                  |
   |                     |-----------------------------------------> Step
   |                     |                                          | Decide = Ask
   |                     |                                          | OnPermissionAsk(req)
   |                     |                                          |   1. register {req, respCh}
   |                     |                                          |      in as.childPermAsks
   |                     |                                          |   2. emit `permission` SSE frame
   |  <== permission ====|                                          |   3. BLOCK on
   |                     |                                          |      select{respCh, cancel, timeout}
   | GET /state          |                                          |
   |-------------------->| read registry (own mutex)              |
   | pending_asks: 1     |   message-scan skipped (TryLock fails)  |
   | <-------------------| merge both sources                     |
   |  dialog opens       |                                          |
   | POST /permissions/resolve                                        |
   |-------------------->| registry branch: respCh <- decision     |
   |  200                |   (never touches as.mu)                 |
   |                     |                                          |  unblocks -> runs tool call
```

The resolve branch is far simpler than the main-agent path, and that is the
payoff of the blocking design: the existing path locks the session, rewrites the
sentinel, and spawns `dispatchAskContinuation` to re-`Step`. A child ask needs
none of that — the child's goroutine is already parked on `respCh`.

### Components

| # | File | Change |
|---|---|---|
| 1 | `internal/agent/permissions.go:93` | Add `AgentName string \`json:"agent_name,omitempty"\`` to `PermissionRequest`. `omitempty` keeps every existing frame byte-identical — same precedent as the `Untrusted*` fields. |
| 2 | `internal/agent/subagent.go:552`, `advisor_tool.go:358` | The asker is installed once on the parent and shared by every child, so the closure captures nothing per-dispatch and cannot know who is asking. Wrap it per dispatch: `if req.AgentName == "" { req.AgentName = spec.Name }`. **This is what makes attribution possible** — without it the dialog can only say "a sub-agent asked". |
| 3 | **new** `internal/server/child_perm_asks.go` | The registry: `childPermAsks{ mu sync.Mutex; m map[string]*childPermAsk }` with `add` / `take` / `list` / `denyAll`, plus `newServerSubAgentAsker(as *agentSession)`. Registers, emits the `permission` SSE frame, then blocks on `select{respCh, parentStop, timeout}`. `respCh` buffered(1), mirroring the TUI. |
| 4 | `internal/server/agent_session.go:437` `buildAgentSession` | Construct the registry on the `agentSession` and install the asker. Headless only. |
| 5 | `internal/server/handler_session_state.go:133` `livePendingAsks` | Restructure: read the registry **always** (own mutex, works while `as.mu` is held); message-scan **only** when `TryLock` succeeds; merge. |
| 6 | `internal/server/handler_permissions_resolve.go` | Add `AgentName` to `PermissionEvent`; project it in `newPermissionEvent`. Add a **registry-first branch to `HandleResolvePermission`, before `findPendingSession`**: the same always-allow guards (`AlwaysRuleChoiceAvailable`, `AlwaysToolChoiceAvailable`, `IsHarmfulRequest`), the same `persistAlwaysAllow(decision, permReq, as.agent.Permissions())`, the same `permission_resolved` broadcast, then deliver into `respCh`. Falls through to the existing path unchanged when the id is not in the registry. |
| 7 | `web/…/PermissionDialog` | Render which sub-agent is asking. |

Only #2 leaves `internal/server`.

## Lifecycles

1. **Answer** → `respCh` receives the decision → the child's tool call proceeds.
2. **Parent cancel** (Escape / stop) → the asker auto-denies via the parent's
   `stopCh`, mirroring the TUI's `<-done` arm → the child gets a normal `denied:`
   tool result and the ask is resolved. This does **not** stop the child: it
   carries on with its own loop until it finishes on its own (see Known limits).
3. **Park timeout** → auto-deny even when the parent is alive and nobody
   answers. Given Known limits, this — not the cancel path — is what actually
   bounds the exposure. Without it a forgotten dialog holds a turn open forever.
   The value is a package const, `childPermAskTimeout = 10 * time.Minute`: long
   enough that someone who steps away is still met by a live dialog, short
   enough that an abandoned one cannot pin a turn. The park is only time spent
   *waiting on a human*, which is why it can afford to be generous.
4. **Session eviction / shutdown** → `denyAll` denies every outstanding ask, so
   no goroutine outlives the session. The registry dies with the `agentSession`;
   there is nothing to sweep. One exception (2026-10-04): a rebuild that lands
   while a turn is active (`replaceAgentSession` after an MCP/plugin/model
   toggle) leaves the old agent running, so the replacement session **inherits
   the old registry** instead of starting an empty one. Otherwise resolve and
   `pending_asks`, which look the session up by id, could no longer find an ask
   the still-running child was parked on, and the turn hung until the timeout.
5. **Stale or already-answered id** → `404 no pending permission found for
   request_id`, identical to today's answer for a resolved main-agent ask. No new
   error shape.
6. **Several asks at once** (parallel DAG children) → the registry holds all of
   them, each with its own request id; `livePendingAsks` returns them all and the
   web UI already renders a list for the multi-ask main-agent case. No new UI
   concept.
7. **Denial never aborts the run** — the child continues and can route around
   the blocked call.

## Tests

**The regression that started this**, in `internal/server`:

1. A session whose `as.mu` is held still reports the child ask. *This is the
   test* — it fails on today's code, and it is the one that would have caught
   agent-run-4.
2. Merge both sources when the lock is free (a main-agent ask **and** a child
   ask both appear); registry-only; nil-registry-preserves-nil.

**The resolve branch — the security-relevant half:**

3. Allow and deny each deliver the right level into `respCh`, HTTP 200.
4. The always-allow guards are enforced identically for child asks:
   `always_rule` where `AlwaysRuleChoiceAvailable` is false → 409; same for
   `always_tool` and `IsHarmfulRequest` — and **nothing is delivered to
   `respCh`**, so a hand-crafted resolve cannot wave a child through.
5. `persistAlwaysAllow` hits the shared `PermissionManager`.
6. Registry miss falls through to the unchanged main-agent path; a stale id → 404.

**The asker's lifecycles:**

7. Answer → the child proceeds. Parent cancel → auto-deny. Park timeout →
   auto-deny. `denyAll` → auto-deny.
8. No goroutine outlives any of those four paths — each asserts the registry is
   empty and the asker goroutine exited. `go test -race` throughout.

**Attribution and wire stability:**

9. `subagent.go:552` stamps `AgentName`; a request that already carries one is
   not overwritten; main-agent asks are unaffected.
10. `PermissionEvent.AgentName` is `omitempty` — an event without it is
    byte-identical to before.

**End-to-end:**

11. The agent-run-4 shape: a sub-agent whose tool needs permission produces a
    pending ask in `/state` *with the turn lock held*, and resolving it lets the
    sub-agent finish its work.
12. A turn parked on a child ask does **not** trip the 30s stalled watchdog. If
    it does, we have traded a silent failure for a false alarm, which is worse
    than the bug being fixed.

**Mutation-verified, and every mutant must compile or it is INVALID:** drop the
registry read from `livePendingAsks` → #1 fails; skip a resolve guard → #4 fails;
remove the timeout arm → #7 fails; drop the `AgentName` stamp → #9 fails; make
`respCh` unbuffered → #8 catches the deadlock.

**Web:** the dialog renders the agent name; the existing `PermissionDialog`
suites stay green, since that dialog is shared with main-agent asks.

## Success criteria

A sub-agent ask shows one dialog naming the agent and the rule; answering
resumes the child's tool call with the real decision; "always allow" persists
and applies to the session; cancelling or timing out unblocks pending child asks
with no goroutine leak; zero behaviour change for the TUI, ACP, `runcli`, and
RC-bridged sessions.

## Known limits (deliberate)

- **Parent cancellation does not reach an executing synchronous sub-agent.**
  `Step` snapshots its own `a.StopCh()` (`agent.go:1459`), and `runSyncDispatch`
  consults the parent's channel only for queue admission (`subagent.go:899`,
  `:905`). So after an auto-deny the child keeps running its own loop until it
  finishes on its own. Today that window is "however long the child takes";
  with a parked ask it becomes "however long the child takes, *after* you hit
  stop". The park timeout caps the exposure rather than leaving it unbounded.
  **This is a separate pre-existing defect and is not fixed here.**
- **Doc alignment is unverified.** A knowledge-bundle lookup was dispatched to
  check whether an existing concept page constrains this design; it did not
  return (`unknown task agent-run-1`). Treat alignment as an open risk, not a
  confirmed non-conflict.
- **Unrelated, still unexplained:** agent-run-5 in the same session ran 45m19s
  and edited nothing. Its cause was never traced. Out of scope here.
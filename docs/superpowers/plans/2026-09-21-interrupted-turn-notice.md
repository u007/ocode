# Interrupted-turn notice in chat — Implementation Plan

> **For agentic workers:** implement task-by-task, ticking the `- [ ]` steps.
> Spec: `docs/superpowers/specs/2026-09-21-interrupted-turn-notice-design.md`
> (untracked at plan time — commit it with the work). This plan names files and
> functions — it contains no code; the executor writes the code.

**Goal:** When a session has settled on an unfinished turn, its chat shows one
inline row — "The previous reply was interrupted" — with a **Continue** button
that re-sends the turn through the normal message path, so a cut-off reply stops
looking like a chat that simply finished.

**Architecture:** The server derives a single `interrupted` boolean and exposes
it on the existing session-state payload. The derivation is conjunctive and
non-blocking: no active turn, **no turn job in flight** (probe the per-session
turn lock), **not parked on an ask** (probe the agent lock, or no resident
agent), and an unfinished stored/memory tail. The web client already polls that
payload; it mirrors the flag per session and `ChatPanel` renders the row, with
App wiring Continue into the existing send path. No new endpoint, no new fetch.

**Tech Stack:** Go (`internal/session`, `internal/server`), React/TypeScript
(`web/src`), Vitest, `tsgo`.

**Spec:** `docs/superpowers/specs/2026-09-21-interrupted-turn-notice-design.md`.

## Global Constraints

- **No code in this plan.** Names of files/functions only.
- **TDD.** Every task starts with a failing test and ends with it passing.
  Mutation-verify each new test: break the behaviour, confirm the test fails,
  restore (the repo's established habit).
- **`use-modern-go` before editing Go.** Run
  `sh "skills/use-modern-go/scripts/run-tool.sh" list --file-path <file.go>` and
  apply the returned guidelines. Then `gofmt` + `go vet` on every touched
  package.
- **Anchors drift — resolve by symbol.** This tree is edited concurrently by
  other sessions (the spec's own review caught `runTurn`'s body moving a line).
  Every file:line in the spec is an anchor to the symbol named beside it;
  re-verify before editing.
- **Both probes must be non-blocking.** The state endpoint is polled by every
  open tab; a blocking lock would pin HTTP connections behind a turn (the
  reason `livePendingAsks` uses `TryLock` today). `sessionTurnInFlight` must not
  create a turn-lock entry as a side effect of a read.
- **Fail open.** Unreadable store, unknown/legacy session format, decode
  failure, or a probe that cannot be evaluated → `interrupted=false`. Never
  invent an interruption.
- **Additive contract.** `pending_asks`, `revision`, `SessionState`, and every
  existing `livePendingAsks` caller keep their current behaviour; the new field
  is `omitempty`.
- **Client separation is load-bearing.** `slice.interrupted` is not
  `slice.wasInterrupted`: the latter means "sending is blocked" in `ChatInput`,
  so reusing it would disable the very Continue action this feature adds.
- **Errors:** every caught error is logged with what was attempted before it is
  returned; an empty `catch` is banned. A degraded path (no tail readable) must
  be an explicit, documented fail-open, not a silent one.
- **No git hazards:** never `git reset`/`git restore --staged .`/`git stash`.
  The tree carries concurrent work — stage specific paths only, and never touch
  files outside this plan's list.
- **Commit policy:** do not commit unless the user asks; the spec is already
  written but uncommitted.
- **Docs in the same change:** CHANGES.md + a concept page via the context agent
  (docs/ is bundle-owned); do not hand-edit `docs/index.md` or `docs/log.md`.
  Add the TUI-parity follow-up to TODO.md.
- **In-session test caveats:** pty-dependent `internal/server` tests fail in
  this sandbox at pristine HEAD too (not touched here); `internal/server` has
  known in-suite flakes under concurrent WIP — compare FAIL sets, don't trust a
  single run.

---

### Task 1: Tail classification + the cheap stored read (`internal/session`)

- [ ] Write failing tests for the tail-classification predicate over
      `[]agent.Message`: assistant-with-content, assistant-with-notice,
      assistant-with-only-tool_calls, content-less assistant, user, tool,
      answered-ask tool row, unanswered question/permission sentinel, empty
      transcript. Pin the three verdicts (complete / waiting / unfinished).
- [ ] Extend the existing cheap stored read so one open yields both the
      revision and the last stored row (sqlite only), keeping the current
      revision-only helper as a thin wrapper so no caller changes.
- [ ] Write failing tests for the stored read: sqlite tail returned; legacy
      `.ojsonl`/`.json` and unreadable sqlite yield no tail (fail open); absent
      session yields no tail; revision output unchanged.
- [ ] Run them; confirm they fail for the expected reason.
- [ ] Implement the predicate and the extended read.
- [ ] Run the package tests; green.
- [ ] Mutation-verify: invert the completed-reply predicate → the
      notice-vs-complete tests fail; drop the sentinel exclusion → the waiting
      tests fail; force the legacy path to error instead of failing open → the
      legacy test fails.
- [ ] `gofmt` + `go vet ./internal/session/...`.
- [ ] Ready to commit: `feat(session): expose stored transcript state (revision + last row)`.

### Task 2: `interrupted` on session state (`internal/server`)

- [ ] Write failing handler tests for the state response: an answered-ask tail
      with no resident agent → `interrupted:true`; a completed turn → `false`;
      a pending ask → `false`; `turn_active:true` → `false`; unreadable/absent
      store → `false`.
- [ ] Write the two busy-window tests against the **real mechanism**, not an
      approximation: (a) `executeTurnJob` driven far enough that the user row is
      persisted and bootstrap is in flight with no resident agent — or holding
      `sessionTurnLock(id)` directly → `false`; (b) the agent lock held across
      the read → `false`. A test that only holds `as.mu` cannot cover (a).
- [ ] Run them; confirm they fail.
- [ ] Implement the two probes — a turn-in-flight check (non-creating lookup +
      non-blocking acquire/release) and the settled-ness sibling of
      `livePendingAsks` that reports lock outcomes — then compute `interrupted`
      in the state handler and add the field to the response struct.
- [ ] Run `go test ./internal/server/ -run 'Session|Interrupt' -v` plus the
      package `-race` build.
- [ ] Mutation-verify: drop the turn-lock probe → window (a) fails; drop the
      agent-lock probe → window (b) fails; invert the tail rule → the
      answered-ask test fails.
- [ ] `gofmt` + `go vet ./internal/server/...`; compare the full-package FAIL
      set against a pristine baseline before blaming the change.
- [ ] Ready to commit: `feat(server): report interrupted turns on session state`.

### Task 3: Client state plumbing (`web/src`)

- [ ] Write a failing store/event test: a state payload with `interrupted:true`
      lands as `slice.interrupted` on that session only (and back to `false` on
      a later payload without it).
- [ ] Implement: the API type field, and the assignment in the reconcile and
      revalidation paths that already consume the state payload — no new fetch,
      no extra request per tab.
- [ ] Write a failing test that a remote session's flag travels with its `host`
      (the existing host-threading path, no new plumbing).
- [ ] Run them; confirm they fail, implement, re-run green.
- [ ] Mutation-verify: drop the assignment → the mapping test fails.
- [ ] `tsgo --noEmit`.
- [ ] Ready to commit: `feat(web): track the server's interrupted flag per session`.

### Task 4: The notice + Continue (`web/src/components/Chat`, `web/src/App.tsx`)

- [ ] Write failing component tests: the row renders with `role="status"` and a
      Continue button when `interrupted` is set; it is absent while
      `wasInterrupted`, a turn is active, streaming has live parts, a question
      or permission is pending, the transcript is empty, the tab is `new-*`, or
      the load failed; clicking Continue invokes the wired handler with
      `"continue"` and hides the row.
- [ ] Run them; confirm they fail.
- [ ] Implement the row at the end of the transcript inside the scroll
      container (not as a transcript entry — it must not enter the message list
      or the search index), the `onContinueInterrupted` prop, and the App
      implementation next to the existing command dispatch, reusing the normal
      send/queue path with the literal text `"continue"`.
- [ ] Run the Chat and App suites; green.
- [ ] Mutation-verify: force the render condition false → the render test fails;
      drop the `wasInterrupted` suppression → the coexistence test fails; make
      Continue send a different text → the click test fails.
- [ ] `tsgo --noEmit` + `npm run test` (Chat/App subsets first, then the full
      suite).
- [ ] Ready to commit: `feat(web): show an interrupted-turn notice with Continue`.

### Task 5: Docs, changelog, and end-to-end verification

- [ ] Add the CHANGES.md entry: files touched, the user-visible behaviour, the
      flag's semantics (settled + unfinished; fail-open), and the accepted cases
      (truncate mid-round, stopped turn after a reload, failed bootstrap).
- [ ] Dispatch the context agent to write the concept page for the new response
      field + notice flow; do not hand-edit the bundle index.
- [ ] Add the TODO.md follow-up: TUI parity for the derived flag, and the
      separate crash-log/unclean-shutdown marker that would let the copy name
      the cause instead of saying "interrupted".
- [ ] Add the feature to TESTING.md under "Tested & Working" once green.
- [ ] End-to-end probe against a real server: build web + server, run it, and
      confirm (a) a session whose tail is an answered ask reports
      `interrupted:true` and renders the notice; (b) an ordinary send to a
      session with **no resident agent** never flashes the notice (window 1);
      (c) answering an ask never flashes it (window 2); (d) a pending ask
      reports `false`.
- [ ] Final report: checklist status, the mutation list with what each proved,
      the suites run, and any residual gap (compaction / `/reset-id` tail
      behaviour remains covered only by the table tests).

**Out of scope (recorded, not deferred silently):** the desktop crash log /
unclean-shutdown marker, auto-resume without a click, and any TUI surface.

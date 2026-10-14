# Goal Condition Loop (`/goal`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (default) or superpowers:subagent-driven-development (long plans of independent tasks) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename today's orchestrator alias to `/orchestrate`, then give `/goal` Claude Code semantics: a per-session condition that keeps the main agent taking turns until a separate judge says it is met or impossible.

**Architecture:** One engine in `internal/agent` (`goal.go`) owns the goal state, the judge call (a new `goal` decider slot through the existing `Decider` interface), the stall/cap guards, the re-prompt message and the metadata encoding. The TUI and the web/desktop server are thin hosts: both call the engine at the same point their auto-continue judge runs today, and both persist the state in session metadata next to `todo_text`. The web SPA talks to three session-scoped endpoints.

**Tech Stack:** Go 1.26.1, Bubble Tea v2, TypeSafe/clef decision backends via `Decider`, web SPA (React/TypeScript, `bun check .` for typecheck).

**Spec:** this conversation's review (2026-10-14): Claude Code `/goal` reference at https://code.claude.com/docs/en/goal.md; ocode gaps listed in the review (status/clear, impossible verdict, per-turn evaluator, no agent switch, headless support).

**Detail:** decisions, not code — the default; the implementer writes the implementation. Per the user's global rules this plan carries **no code snippets**: tests are described by name and assertion.

## Global Constraints

- Prompt-cache rule (CLAUDE.md "Prompt Cache Stability"): every goal injection is **user-role**, wrapped in an `[ocode:goal]` marker; never `system`-role, never in `tools`.
- Decision backends only through `Decider` (`internal/agent/decider.go`); never widen it; never cache a nil decider; label usage with `deciderLabel`.
- Judge confidence floor is per-judge (CLAUDE.md): the goal floor is its own constant, not the permission floor.
- `Handler.mu` is a map lock: no LLM/judge call under it. Per-turn work stays under `agentSession.mu`.
- Slash-command queuing: `isInstantCmd` in `handleCommand` (`internal/tui/model.go`) is the sole instant list.
- Session-scoped routes live in `Server.registerRoutes()` behind one of the four middleware wrappers and thread `?host=`.
- Any new config key gets a `.env.example`-style placeholder only if it is an env var; `goal.judge_model` is a config-file key (no env var), so no `.env.example` change.
- Condition length limit: **4000 characters** (Claude Code parity). Hard turn cap: **50**. Stall pause: **3** consecutive turns with no tool call. Judge confidence floor: **0.6** (same reasoning as `autoContinueMinConfidenceDefault`: cheap to be wrong, bounded).
- `go test -race ./...` is the gate; `web/`: `bun check .`.

## Review Focus

1. A turn ends on an unresolved permission/question ask while a goal is active → the host must NOT auto-fire a resume (the dialog owns the next move). Test: Task 3 `TestStreamDoneGoalSkipsWhenQueueDrainBlocked`, Task 4 `TestRunTurnGoalPausedOnPendingAsk`.
2. Judge unreachable (no API key, network error) → goal **pauses** with a visible notice and stays set; it must never silently clear or silently loop. Test: Task 2 `TestGoalJudge_NoDeciderPauses`, `TestGoalJudge_ErrorPauses`.
3. Model stops calling tools and just narrates "done" → stall guard pauses after 3 such turns instead of burning the cap. Test: Task 2 `TestGoalJudge_StallPausesAfterThreeNoToolTurns`.
4. `/new`, `/clear` and a new `/goal <text>` while a goal is active → previous goal fully replaced/cleared in memory AND in persisted metadata (no zombie goal on resume). Test: Task 3 `TestNewSessionClearsGoal`, Task 4 `TestSessionGoalPutReplacesPrior`.
5. Judge says `met` but with confidence below the floor → treated as `not_met` (one more turn), never as met. Test: Task 2 `TestGoalJudge_LowConfidenceMetTreatedNotMet`.

---

### Task 1: Rename the orchestrator alias to `/orchestrate`; remove the dead TUI pipeline path

**Risk:** standard — mechanical rename plus deletion of code that is already unreachable.

**Files:**
- Modify: `internal/tui/commands.go:175` (command table entry), `internal/tui/commands.go:1823-1841` (delete `runGoalCmd`)
- Modify: `internal/tui/model.go:9002-9009`, `:9097`, `:9111-9117` (alias flag + instant list + rewrite), `:5748-5771` (delete `goalDoneMsg`/`goalStatusMsg` cases)
- Delete: `internal/tui/goal.go`, `internal/tui/goal_test.go` (it asserts `strings.Join`, not a feature)
- Rename: `internal/cli/goal.go` → `internal/cli/orchestrate.go`, `internal/cli/goal_test.go` → `internal/cli/orchestrate_test.go`; `main.go:221` subcommand `goal` → `orchestrate`
- Modify: `internal/agent/agent_registry.go:157-160` (comment: there is no TUI intercept; `/orchestrate` switches to the plugin's `orchestrator` primary agent and sends the goal as a normal turn; the Go pipeline is CLI-only)
- Modify: `web/src/components/Chat/commands.ts:119`, `:578`, `:1931-1948` (`/goal` → `/orchestrate`, `handleGoal` → `handleOrchestrate`; the `/goal` slot is reused in Task 5)
- Modify: `internal/tui/command_test.go:1686`
- Modify docs: `README.md:562`, `:605`, `:670`, `:877`, `:883`; `skills/ocode-usage/SKILL.md:285`, `:527`; `skills/ocode-tui/SKILL.md:151`; `docs/web-desktop-parity-todo.md:111-115`; `internal/tui/commands.go:1893` (help copy "not just /goal" → "not just /orchestrate")

**Interfaces:**
- Produces: slash command `/orchestrate <goal>` (TUI + web), CLI `ocode orchestrate …` with unchanged flags. `cli.ParseGoalArgs`/`GoalOptions` renamed to `ParseOrchestrateArgs`/`OrchestrateOptions`.

- [ ] **Step 1: Apply the rename and deletions, update the tests in place**
  - `command_test.go`: the `/goal` entry becomes `/orchestrate`.
  - `orchestrate_test.go`: the five `TestParseGoalArgs_*` tests become `TestParseOrchestrateArgs_*` with identical assertions.
  - Add `TestOrchestrateAliasIsInstantAndRewrites` in `internal/tui/command_test.go`: `/orchestrate x` while `m.streaming` is false switches `m.agent` to the `orchestrator` definition and returns a non-nil `tea.Cmd`; `/goal` no longer hits the alias branch.
- [ ] **Step 2: Run** `go build ./... && go vet ./... && go test ./internal/tui/ ./internal/cli/ ./internal/agent/ -run 'Orchestrat|Command' && (cd web && bun check .)` — expected: all pass, no references to `runGoalCmd`, `goalDoneMsg`, `ParseGoalArgs` remain (`rg` returns nothing).

---

### Task 2: Goal engine in `internal/agent`

**Risk:** high — branching verdict logic, a new decider slot, state that persists.

**Files:**
- Create: `internal/agent/goal.go`, `internal/agent/goal_test.go`
- Modify: `internal/agent/decider.go` (new slot `slotGoal = "goal"`; `slotModel` reads `oc.Goal.JudgeModel`)
- Modify: `internal/config/ocodeconfig.go:884` area (add `Goal JudgeModelConfig` to the `Ocode` struct next to `ContentGuard`), `:1703-1707` area (`applyJudgeModelConfig(&cfg.Goal.JudgeModel, file.Goal.JudgeModel)`), and the file-schema struct that `file.ContentGuard` comes from
- Modify: `internal/agent/agent.go` (new field `OnGoalJudge func(GoalJudgeResult)` beside `OnAutoContinueJudge` at `:660`; new private field `goal *GoalState` guarded by the agent's existing state mutex)

**Interfaces (produces; Tasks 3–5 consume exactly these):**
- `type GoalStatus string` with `GoalActive`, `GoalPaused`, `GoalMet`, `GoalImpossible` (`"active"`, `"paused"`, `"met"`, `"impossible"`).
- `type GoalState struct { Condition string; Status GoalStatus; Turns int; NoToolTurns int; LastReason string; SetAt time.Time }`.
- Constants: `GoalMaxConditionLen = 4000`, `GoalMaxTurns = 50`, `goalStallTurns = 3`, `goalMinConfidenceDefault = 0.6`, metadata key `GoalMetadataKey = "goal"`, marker `"[ocode:goal]"`.
- `func (a *Agent) SetGoal(condition string) error` — trims; empty or >4000 → error; replaces any prior state with `Status=GoalActive`, `Turns=0`, `NoToolTurns=0`, `SetAt=now`.
- `func (a *Agent) ClearGoal()`; `func (a *Agent) Goal() (GoalState, bool)` (copy; ok=false when none was ever set or it was cleared); `func (a *Agent) GoalEvaluable() bool` (true for `active` or `paused`); `func (a *Agent) RestoreGoal(gs GoalState)` (metadata restore, no validation beyond non-empty condition).
- `func (a *Agent) GoalMarkHumanTurn()` — a human-submitted message flips `paused` → `active` and resets `NoToolTurns`; no-op otherwise.
- `func (a *Agent) GoalKickoffPrompt() string` — the first user turn after `/goal <text>`: copy pinned as "Work toward this goal until it is met: <condition>\nVerify with tools before claiming it is met; do not stop to ask the user unless genuinely blocked."
- `type GoalJudgeResult struct { Gen uint64; Verdict string; Status GoalStatus; Detail string; Resume bool; Err error }` — `Verdict` ∈ `met|not_met|impossible|stalled|capped|unavailable`; `Resume` is true only when `Status==GoalActive && Verdict=="not_met"`.
- `func (a *Agent) GoalJudgeSync(messages []Message) GoalJudgeResult` — the whole post-turn evaluation (order below); mutates state; records side usage via `RecordSideUsage(..., deciderLabel(d))`; emits one `emitDebug("AGENT", …)` line with verdict/confidence/reason.
- `func (a *Agent) GoalJudgeAsync(messages []Message, gen uint64) bool` — same as `AutoContinueJudgeAsync` shape (`:2789`): false when nothing to evaluate; otherwise runs `GoalJudgeSync` in `crashguard.Go` and delivers to `OnGoalJudge` with `Gen=gen`.
- `func (a *Agent) GoalResumeMessage() Message` — user-role; content = marker line, then "Goal not yet met: <condition>", then "Evaluator: <reason label>" and "Continue working toward the goal; verify with tools before reporting it met."
- `func (a *Agent) GoalStatusText() string` — shared copy for TUI `/goal` and the GET endpoint: "No goal set." or condition, status, turns, last reason, judge model id (`deciderLabel` of the resolved decider, or "unavailable").
- `func GoalStateToMetadata(gs GoalState) map[string]any` and `func GoalStateFromMetadata(meta map[string]any) (GoalState, bool)` — one object under `GoalMetadataKey` with keys `condition`, `status`, `turns`, `no_tool_turns`, `last_reason`, `set_at` (RFC3339); numbers tolerate `float64` after JSON round-trip (as `sessionThinkingBudget` does).

**Evaluation order inside `GoalJudgeSync`** (each step returns if it decides):
1. No goal or status `met`/`impossible` → `Verdict="unavailable"`, `Resume=false`, no mutation. (Hosts should not call it then; this is defensive.)
2. `Turns++`. Tool-use detection: scan messages after the last user-role message; any `assistant` message with non-empty `ToolCalls` counts as tool use. No tool use → `NoToolTurns++`, else reset to 0. `NoToolTurns >= goalStallTurns` → `Status=GoalPaused`, `Verdict="stalled"`, detail names the count.
3. `Turns >= GoalMaxTurns` → `Status=GoalPaused`, `Verdict="capped"`.
4. `resolveDecider(slotGoal)` nil → `Status=GoalPaused`, `Verdict="unavailable"`, detail says which model id is unconfigured/keyless.
5. Build state: `condition`, `turns`, `transcript_tail` (same shape and 4000-char trim as `buildTypesafeAutoContinueState`, tail of **12** messages), `turn_ended` (reuse the same three descriptions). Questions: `verdict` choice `{met, not_met, impossible}`; `reason` choice closed set `{all_satisfied, work_remains, unverified_claim, blocked_by_error, awaiting_user, cannot_be_done}` with one-line labels in that order. Rubric (instructions) pins: judge only what the transcript shows; a claim of completion with no supporting tool output is `not_met`/`unverified_claim`; `impossible` only when the condition contradicts itself or needs something the assistant demonstrably cannot reach; a reply that ends on a question for the user is `not_met`/`awaiting_user`.
6. `Decide` error → `Status=GoalPaused`, `Verdict="unavailable"`, `Err` set, detail includes `deciderLabel`. Missing/invalid `verdict` answer → same.
7. `met` with confidence ≥ floor → `Status=GoalMet`. `impossible` with confidence ≥ floor → `Status=GoalImpossible`. Either below floor → fall through as `not_met` with the floor named in detail.
8. `not_met` with reason `awaiting_user` → `Status=GoalPaused` (decisive, like auto-continue). Otherwise `Status` stays `GoalActive` (a `paused` goal reaching here becomes `active` again), `Resume=true`.

- [ ] **Step 1: Write the failing tests in `internal/agent/goal_test.go`** (use a test-local stub implementing `Decider` whose `Decide` returns a scripted `TypesafeResponse`; inject it through `newClientFn` the way `TestDecider_*` would, or by setting the agent's config to a `typesafe/…` model and overriding `newClientFn`):
  - `TestSetGoalValidation`: empty → error; 4001 chars → error; exactly 4000 → ok; a second `SetGoal` resets `Turns` to 0 and status to active.
  - `TestGoalJudge_MetEndsGoal`: scripted `met` at 0.9 → `Status==GoalMet`, `Resume==false`, `GoalEvaluable()==false`.
  - `TestGoalJudge_NotMetResumes`: `not_met`/`work_remains` → `Resume==true`, `Turns==1`, status active.
  - `TestGoalJudge_ImpossibleEndsGoal`: `impossible` at 0.9 → `Status==GoalImpossible`.
  - `TestGoalJudge_LowConfidenceMetTreatedNotMet`: `met` at 0.4 → `Resume==true`, detail mentions the floor.
  - `TestGoalJudge_AwaitingUserPauses`: `not_met`/`awaiting_user` → `GoalPaused`, `Resume==false`.
  - `TestGoalJudge_NoDeciderPauses`: no judge credential → `Verdict=="unavailable"`, `GoalPaused`, condition retained.
  - `TestGoalJudge_ErrorPauses`: stub returns an error → `Err!=nil`, `GoalPaused`.
  - `TestGoalJudge_StallPausesAfterThreeNoToolTurns`: three evaluations over transcripts whose last assistant turns have no `ToolCalls` → third result `Verdict=="stalled"`; a transcript with a tool call in between resets the counter.
  - `TestGoalJudge_TurnCapPauses`: state restored with `Turns=49` → next evaluation `Verdict=="capped"`.
  - `TestGoalMarkHumanTurnReactivatesPaused`: paused → `GoalMarkHumanTurn` → active, `NoToolTurns==0`; met stays met.
  - `TestGoalResumeMessageIsUserRoleWithMarker`: role `user`, content starts with `[ocode:goal]`, contains the condition.
  - `TestGoalMetadataRoundTrip`: `GoalStateToMetadata` → JSON marshal/unmarshal → `GoalStateFromMetadata` equals the original (status, turns as float64 tolerated, `set_at` second precision).
  - `TestDecider_SlotModelGoalReadsConfig` in `decider_test.go`: `Ocode.Goal.JudgeModel="clef/x"` → `slotModel(slotGoal)=="clef/x"`; blank → `defaultJudgeModel`.
- [ ] **Step 2: Run** `go test ./internal/agent/ -run 'Goal|SlotModelGoal'` — expected: compile failure / FAIL on the undefined symbols.
- [ ] **Step 3: Implement `goal.go`, the slot, the config key and the agent fields** as specified above. Follow `autocontinue_typesafe.go` for the question/criteria structure and `validateAnswer`-style guards on confidence range.
- [ ] **Step 4: Run** `go test -race ./internal/agent/ ./internal/config/` — expected: PASS, including `TestSharedStateBudget_*` untouched.

---

### Task 3: TUI host — `/goal` command, stream-done hook, persistence

**Risk:** high — touches the stream-done dispatch order and session save/restore.

**Files:**
- Modify: `internal/tui/commands.go` (new table entry `/goal` with usage `/goal [<condition> | clear]`, handler `runGoalCmd`)
- Modify: `internal/tui/model.go`: `isInstantCmd` chain (`:9041-9097`), stream-done branch (`:5471-5494`), new `case goalJudgeFinishedMsg` beside `:5675`, `sessionSidebarMetadata` (`:20997`), the two `restoreTodoState` call sites (`:2875`, `:11408`), the three `tool.ResetTodoState()` sites (`:2604`, `:11406`, `:11865`), the human-send path where `settleAutoContinueChain` runs (`:5259`, `:5324`) and the model fields block near `autoContinueGen` (`:1337`)
- Test: `internal/tui/command_test.go`, `internal/tui/model_test.go` (or the file that already tests `shouldAutoContinue`)

**Interfaces:**
- Consumes: Task 2 engine (`SetGoal`, `ClearGoal`, `Goal`, `GoalEvaluable`, `GoalKickoffPrompt`, `GoalJudgeAsync`, `OnGoalJudge`, `GoalResumeMessage`, `GoalStatusText`, `GoalMarkHumanTurn`, `GoalStateToMetadata`, `GoalStateFromMetadata`).
- Produces: `goalJudgeFinishedMsg{gen uint64; result agent.GoalJudgeResult}`; model fields `goalGen uint64`, `lastTurnWasGoalResume bool`; `isGoalClearVerb(s string) bool` in `commands.go` (the six clear verbs, case-insensitive).

**Decisions:**
- `/goal <text>`: `SetGoal`; on error show it; else append a transient hint "◎ goal set — evaluating after every turn" and send `GoalKickoffPrompt()` as the user message via the normal `askAgent` path (it is a turn start, so it **queues** while streaming — not instant).
- `/goal` (no args) → `GoalStatusText()`; `/goal clear|stop|off|reset|none|cancel` → `ClearGoal()` + "Goal cleared." Both are **instant**: extend `isInstantCmd` with the `/agents status` pattern (`cmd == "/goal" && (len(args)==0 || isGoalClearVerb(args[0]))`).
- Stream-done order (inside the `!m.queueDrainBlocked()` block): pending job resume → queue drain → step-limit hard resume (unchanged) → **if `m.agent.GoalEvaluable()` and `msg.err == nil`: dispatch `GoalJudgeAsync` (bump `goalGen`) and return — the auto-continue judge is NOT dispatched while a goal is evaluable** → existing auto-continue judge.
- `goalJudgeFinishedMsg`: drop if `gen != m.goalGen`. Append a transient hint with `result.Detail` ("◎ goal: not yet met — <reason>, resuming (turn N/50)" / "◎ goal met after N turns" / "◎ goal paused: <detail>"). On `Resume`: append `GoalResumeMessage()` as a user message (with `raw`), set `lastTurnWasGoalResume`, call `askAgent()`. Save session after every result (metadata changed).
- Human-send path: where `settleAutoContinueChain` is called for a human turn, also call `m.agent.GoalMarkHumanTurn()` unless `lastTurnWasGoalResume` (then reset the flag).
- Persistence: `sessionSidebarMetadata` adds `GoalMetadataKey` when `Goal()` ok; both restore sites call `RestoreGoal` when `GoalStateFromMetadata` ok. Every `ResetTodoState` site also `ClearGoal()`.
- `/new` and `/clear` therefore clear the goal; `/reset-id` needs nothing (state is on the agent, metadata moves with the session file).

- [ ] **Step 1: Write the failing tests**
  - `TestGoalCommandSetsGoalAndStartsTurn`: `/goal all tests pass` → `m.agent.Goal()` ok with that condition; a user message equal to `GoalKickoffPrompt()` is appended; returned cmd non-nil.
  - `TestGoalCommandStatusAndClearAreInstant`: with `m.streaming=true`, `/goal` and `/goal clear` run immediately (not appended to `m.queuedItems`); `/goal fix it` is queued.
  - `TestGoalCommandRejectsOverlongCondition`: 4001-char arg → assistant message contains "4000"; no goal set.
  - `TestStreamDoneDispatchesGoalJudgeNotAutoContinue`: goal active, `autoContinueEnabled=true`, natural end → `goalGen` incremented, `autoContinueGen` unchanged.
  - `TestStreamDoneGoalSkipsWhenQueueDrainBlocked`: question dialog active → no judge dispatch.
  - `TestGoalJudgeFinishedResumesOnNotMet`: deliver `goalJudgeFinishedMsg{Resume:true}` with matching gen → last message is user-role starting with `[ocode:goal]`, cmd non-nil; stale gen → ignored.
  - `TestGoalStateSurvivesSessionSaveRestore`: `sessionSidebarMetadata()` contains the `goal` object; `restoreTodoState`'s neighbour restore path repopulates `Goal()`.
  - `TestNewSessionClearsGoal`: `/new` → `Goal()` ok==false.
- [ ] **Step 2: Run** `go test ./internal/tui/ -run 'Goal'` — expected: FAIL (undefined handler/msg).
- [ ] **Step 3: Implement** per the decisions above. Reuse `hintStyle` and the `transient` message shape from `fireAutoContinue` (`model.go:198-224`).
- [ ] **Step 4: Run** `go test -race ./internal/tui/ -run 'Goal|AutoContinue|Command'` then the full `go test -race ./internal/tui/` — expected: PASS (pty-dependent failures in a sandbox are pre-existing, see CLAUDE.md).

---

### Task 4: Server host — `runTurn` loop, session endpoints, restore

**Risk:** high — concurrency and lock discipline in `runTurn`; persistence.

**Files:**
- Create: `internal/server/handler_session_goal.go`, `internal/server/handler_session_goal_test.go`
- Modify: `internal/server/agent_session.go` (`runTurn` loop `:1270-1296`; `buildAgentSession` `:437`; the human-turn entry at `:1739`), `internal/server/server.go` (routes beside `:287-288`)
- Test: `internal/server/agent_session_test.go` (or the file holding the existing auto-continue runTurn tests)

**Interfaces:**
- Consumes: Task 2 engine.
- Produces: `GET /api/sessions/{id}/goal` → `{active: bool, condition, status, turns, last_reason, status_text}` (`active=false` and empty fields when none); `PUT /api/sessions/{id}/goal` body `{condition}` → 400 on empty/overlong, else sets on the live agent and returns the GET shape plus `kickoff_prompt` (= `GoalKickoffPrompt()`); `DELETE /api/sessions/{id}/goal` → 204. All three via `authMiddleware`, resolving the session with `ensureAgentSession`/`getOrCreateAgentSession` (never under `h.mu` while touching the agent), persisting with `session.UpdateMetadataForDir(entry.ProjectRoot, id, …)` writing/deleting `GoalMetadataKey`.

**Decisions:**
- `runTurn`: after the step-limit/auto-continue decision block, **before** it, insert: if `as.agent.GoalEvaluable()` and the tail is not a permission/question ask (`tailIsPermissionAsk`/`tailIsQuestionAsk`), call `GoalJudgeSync(as.messages)`; `appendTranscriptNotice` with `Detail`; persist metadata; on `Resume` append `GoalResumeMessage()` to `as.messages` and `messages`, then `continue`. When a goal is evaluable the auto-continue judge is skipped (step-limit hard resume still applies first, as it is intra-turn). Goal resumes do not touch `autoContinueCount`.
- Human turn: the `:1739` entry (non-empty API content) calls `GoalMarkHumanTurn()` before `runTurn`; the job-resume callers (`:1943`, `:1972`) do not.
- `buildAgentSession`: after messages are loaded, read the session's metadata (`session.LoadForDir`) and `RestoreGoal` when present (same place other per-session overrides are read).
- Remote projects: the three routes are session-scoped and therefore follow the existing proxy rule; nothing extra.
- No new SSE event: notices ride the existing transcript-notice path; the SPA polls GET for status.

- [ ] **Step 1: Write the failing tests**
  - `TestRunTurnGoalLoopResumesUntilMet`: stub decider scripted `not_met`, `not_met`, `met` → three `Step` calls in one `runTurn`, two `[ocode:goal]` user messages in `as.messages`, final notice contains "met", persisted metadata status `met`.
  - `TestRunTurnGoalPausedOnPendingAsk`: last message is a permission ask → no judge call, no resume.
  - `TestRunTurnGoalSkipsAutoContinueJudge`: goal active + auto-continue enabled → auto-continue judge never consulted.
  - `TestSessionGoalEndpoints`: PUT → 200 with `kickoff_prompt`; GET → active; DELETE → 204 then GET `active=false`; PUT with 4001 chars → 400.
  - `TestSessionGoalPutReplacesPrior`: second PUT → `turns==0`, new condition in metadata.
  - `TestBuildAgentSessionRestoresGoal`: metadata with a goal object → `as.agent.Goal()` ok.
- [ ] **Step 2: Run** `go test ./internal/server/ -run 'Goal'` — expected: FAIL.
- [ ] **Step 3: Implement** per the decisions; model the handler file on `handler_session_thinking_budget.go`.
- [ ] **Step 4: Run** `go test -race ./internal/server/ -run 'Goal|AutoContinue|RunTurn'` then full `go test -race ./internal/server/` — expected: PASS (known-flaky race tests per memory are not regressions).

---

### Task 5: Web SPA `/goal` command

**Risk:** standard — thin client over Task 4's endpoints.

**Files:**
- Modify: `web/src/components/Chat/commands.ts` (reuse the `/goal` slot freed in Task 1: `handleGoal(args, ctx)`), `web/src/api/client.ts` (beside `getAutoContinue`/`setAutoContinue` at `:3608`: `getSessionGoal`, `setSessionGoal`, `clearSessionGoal`, host-threaded like the thinking-budget calls at `:1327`)
- Test: `web/src/components/Chat/commands.goal.test.tsx` (pattern: `commands.autocontinue.test.tsx`)

**Interfaces:**
- Consumes: Task 4 endpoints.
- Produces: `/goal <text>` → PUT then returns `{handled:true, prompt: kickoff_prompt}` so the normal send path starts the turn; `/goal` → GET → `ok(status_text)`; `/goal clear|stop|off|reset|none|cancel` → DELETE → `ok("Goal cleared.")`. Command palette description: "Keep working until a condition is met".

- [ ] **Step 1: Implement with tests alongside**: `sets goal then sends kickoff prompt`, `status shows status_text`, `clear verbs call DELETE`, `empty condition shows usage without a request`, `overlong condition surfaces the 400 message`.
- [ ] **Step 2: Run** `cd web && bun test commands.goal && bun check .` — expected: PASS.

---

### Task 6: Docs, CLAUDE.md invariants, TODO entries

**Risk:** standard.

**Files:**
- Create: `docs/concepts/goal-condition-loop.md` (OKF frontmatter like `server-auto-continue.md`): command syntax, evaluation order, verdict table, pause/resume rules, persistence key, endpoints, difference from `/orchestrate`.
- Modify: `CLAUDE.md` — add a short "Goal condition loop" section listing the invariants: user-role injection only; goal judge supersedes the auto-continue judge while evaluable; pending ask never auto-resumes; paused never clears; `/new`/`/clear` clear it; `slotGoal` has its own floor. Add `/goal` (status/clear forms) to the instant-command note and `/orchestrate` to the queuing doc.
- Modify: `docs/concepts/tui-slash-command-queuing.md`, `docs/concepts/server-auto-continue.md` (one paragraph: interaction with goals), `README.md` command table (`/goal`, `/orchestrate`), `skills/ocode-usage/SKILL.md`, `skills/ocode-tui/SKILL.md` (file pointer `internal/agent/goal.go`), `docs/web-desktop-parity-todo.md` (item 10 now at parity).
- Modify: `TODO.md` — three deferred items with reasons: (1) `ocode run "/goal …"` headless CLI parsing (today `runAttach` posts raw content to `/api/chat`; the server does not parse slash commands); (2) background-work deferral (Claude Code defers evaluation while subagents/background shells run); (3) check-in cadence while a goal is active.

- [ ] **Step 1: Write the docs and TODO entries**; re-derive every `file.go:NNN` anchor you cite against the final source (CLAUDE.md "Code line anchors drift silently").
- [ ] **Step 2: Verify** `rg -n '/goal' README.md skills docs CLAUDE.md` shows only the new semantics; `go test -race ./...` and `cd web && bun check .` pass.

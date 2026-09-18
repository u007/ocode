# Discovery TypeSafe (Jev) Relevance Judge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When discovery is enabled and the TypeSafe provider is connected, ask Jev per turn whether each embedder-selected skill, project doc, or MCP tool is actually needed for the current request, and attach only the ones it confirms.

**Architecture:** Split `discovery.Session.Discover` into a non-mutating `Select` (rank + rank-relative policy, returns not-yet-attached candidates) and the existing `Seed` (mark attached). `RunDiscovery` runs `Select`, hands the candidates plus the transcript tail to a new TypeSafe judge (`internal/agent/discovery_typesafe.go`) that asks one **noul** question per candidate in a single `Decide` call, keeps candidates whose noul meets `permissions.auto.min_confidence`, and seeds only those. TypeSafe not connected, or any judge failure → every candidate is seeded (today's behavior, fail-open like the rest of discovery).

**Tech Stack:** Go; existing `TypesafeClient.Decide` (`internal/agent/typesafe.go`); `internal/discovery` engine/session; `httptest` for judge tests; `discovery.FakeEmbedder` for session tests.

**Spec:** Design approved in chat 2026-09-18 (no separate spec file — bounded change). Decisions: judge all three doc kinds (`skill`, `md`, `mcp`); threshold reuses `permissions.auto.min_confidence` (default `autoJudgeMinConfidenceDefault = 0.85` in `internal/agent/permissions.go`); no new config flag — "connected" means `newClientFn(cfg, "typesafe/jev-latest")` yields a `*TypesafeClient` with a non-empty API key.

## Global Constraints

- Plans are high-level: this file names files/functions, never code. Executors write the code.
- Caching invariants from AGENTS.md "Split tail injection by volatility": the judge must not change `renderDiscoveryContext` output (names-index stays a function of the doc set). It only changes which IDs become attached.
- Discovery is fail-open everywhere. The judge must never make a turn attach *fewer* docs because of an error; only a real Jev verdict may veto.
- No flags, no fallbacks with `??`-style defaults, no empty catch. Every judge failure is logged via `a.emitDebug("DISCOVERY", ...)`.
- Jev usage is billed side usage: record via `a.RecordSideUsage(in, out, 0, 0, "typesafe/"+model)` exactly as `runAutoContinueJudgeTypesafe` does.
- Keep `TypesafeClient` decision-only: the judge calls `Decide` only.
- Run `gofmt` and `go vet ./internal/agent/... ./internal/discovery/...` before every commit. Never `git stash`.
- Update AGENTS.md and `docs/` in the same change (docs are source of truth).

---

### Task 1: `Session.Select` — non-mutating candidate selection

**Files:**
- Modify: `internal/discovery/engine.go` (`Session` type, `Discover`, add `Select`)
- Test: `internal/discovery/engine_test.go`

**Interfaces:**
- Consumes: `Engine.Rank(ctx, query) ([]Scored, error)`, `SelectRankRelative([]Scored) []Scored` (`internal/discovery/index.go`).
- Produces: `func (s *Session) Select(ctx context.Context, query string) ([]Doc, error)` — ranks, applies `SelectRankRelative`, returns the selected docs that are **not** already attached, in rank order, without mutating the sticky set. `Discover` must become exactly `Select` followed by `Seed` of the returned IDs, returning the same slice (so existing callers keep identical behavior).

- [ ] **Step 1: Write failing tests** in `engine_test.go`: (a) `TestSelectDoesNotAttach` — warm two docs with `FakeEmbedder`, call `Select`, assert both returned and `IsAttached` false for both; (b) `TestSelectSkipsAlreadyAttached` — `Seed` one ID, `Select` again, assert only the other is returned; (c) `TestDiscoverEqualsSelectPlusSeed` — `Discover` on a fresh session returns the same IDs `Select` would and leaves them attached.
- [ ] **Step 2: Run** `go test ./internal/discovery/ -run 'TestSelect|TestDiscoverEquals' -v` → expect compile failure (no `Select`).
- [ ] **Step 3: Implement** `Select`; rewrite `Discover` as `Select` + `Seed`. Keep the existing lock discipline (`Seed` already locks).
- [ ] **Step 4: Run** the same tests plus `go test ./internal/discovery/` (existing `TestStickyGrowsNeverShrinks` must still pass).
- [ ] **Step 5: Commit** `refactor(discovery): split Session.Discover into Select + Seed`.

---

### Task 2: TypeSafe discovery judge (pure decision helper)

**Files:**
- Create: `internal/agent/discovery_typesafe.go`
- Create: `internal/agent/discovery_typesafe_test.go`

**Interfaces:**
- Consumes: `TypesafeClient.Decide`, `TypesafeQuestion`, `TypesafeAnswer` (`typesafe.go`); `discovery.Doc`; `a.resolveAutoJudgeMinConfidence()` (`permissions.go`); `a.RecordSideUsage`; `a.emitDebug`.
- Produces:
  - `const discoveryJudgeModel = "typesafe/jev-latest"` (reuse the catalog constant from `models_registry.go` if one exists for the id; otherwise define here).
  - `func (a *Agent) discoveryJudgeClient() *TypesafeClient` — calls `newClientFn(a.config, discoveryJudgeModel)`, returns the client only when the type assertion succeeds **and** `APIKey != ""`; otherwise nil. This is the sole "provider connected" check.
  - `func (a *Agent) judgeDiscoveryCandidates(client *TypesafeClient, tail []Message, query string, candidates []discovery.Doc) (keep []discovery.Doc, err error)` — builds state, asks one noul per candidate, returns the subset to attach. On transport/decode error returns `(nil, err)`; the caller decides fail-open. A candidate whose answer is missing or not `type:"noul"` is **kept** (fail-open per candidate) and logged.
  - `func buildDiscoveryJudgeState(tail []Message, query string, candidates []discovery.Doc) map[string]any` — pure; keys: `request` (the discovery query text), `transcript_tail` (same bounding rules as `buildTypesafeAutoContinueState`: last 6 non-empty messages, 4000-char cap), `candidates` (array of `{id, kind, name, summary}` where `summary` is `Doc.Text` capped at 1000 chars). Keep it pure so state shape is unit-testable.
  - Question design (one per candidate, key = `Doc.ID`, type `noul`): instructions state that the assistant is an AI coding agent mid-conversation, the state holds the current request plus transcript tail, and the question is whether loading **this specific** candidate (referenced by backticked path `candidates[i]`) into the assistant's context is needed to complete the current request. Criteria: `true` = the request cannot be handled well without this skill/doc/tool, or it directly covers the task; `false` = topically adjacent but not required, or the task is already covered by the conversation. Kind-specific wording: skill = "a reusable procedure the assistant follows", md = "a project document", mcp = "a callable tool".
  - Threshold: keep when `answer.Noul >= a.resolveAutoJudgeMinConfidence()`.

- [ ] **Step 1: Write failing tests** using an `httptest` server that decodes the request and returns canned answers (mirror `typesafe_test.go`), and a `newClientFn` override with `t.Cleanup` restore (mirror `autocontinue_typesafe_test.go`):
  - `TestDiscoveryJudgeStateShape` — `buildDiscoveryJudgeState` includes `request`, bounded `transcript_tail`, and one candidate entry per doc with `id/kind/name/summary`; summary capped.
  - `TestDiscoveryJudgeKeepsAboveThreshold` — three candidates, server answers nouls 0.95 / 0.10 / 0.85 with `min_confidence` 0.85 → keeps first and third only; request body carries exactly three `noul` questions keyed by doc ID.
  - `TestDiscoveryJudgeMissingAnswerKept` — server omits one candidate's answer → that candidate kept.
  - `TestDiscoveryJudgeTransportError` — server returns 500 → `err != nil`, `keep == nil`.
  - `TestDiscoveryJudgeRecordsSideUsage` — usage in response shows up via whatever accessor the agent exposes for side usage (check how `autocontinue_typesafe_test.go` asserts it; reuse).
  - `TestDiscoveryJudgeClientNilWhenNotConnected` — `newClientFn` returns a `*TypesafeClient` with empty `APIKey` → `discoveryJudgeClient()` nil; returns a non-typesafe client → nil.
- [ ] **Step 2: Run** `go test ./internal/agent/ -run TestDiscoveryJudge -v` → compile failure.
- [ ] **Step 3: Implement** `discovery_typesafe.go` per the interfaces above. Debug line format: `discovery_typesafe kept=%d vetoed=%d min=%.2f model=%s` plus one line per candidate `id=%s noul=%.3f verdict=keep|veto`.
- [ ] **Step 4: Run** the tests → PASS. Run `go vet ./internal/agent/`.
- [ ] **Step 5: Commit** `feat(discovery): typesafe relevance judge for embedder-selected docs`.

---

### Task 3: Wire the judge into `RunDiscovery`

**Files:**
- Modify: `internal/agent/discovery_glue.go` (`RunDiscovery`, ~line 301)
- Modify: `internal/agent/agent.go` call site (~line 1249: `a.RunDiscovery(discoveryQueryFromMessages(messages, a.workDir))`)
- Test: `internal/agent/discovery_glue_test.go`

**Interfaces:**
- Consumes: Task 1 `Session.Select`/`Seed`; Task 2 `discoveryJudgeClient`, `judgeDiscoveryCandidates`.
- Produces:
  - `func (a *Agent) RunDiscoveryForMessages(messages []Message)` — computes the query with `discoveryQueryFromMessages` and calls the internal runner with the messages as the judge tail. `agent.go` switches to this.
  - `func (a *Agent) RunDiscovery(query string)` — kept for existing callers/tests; delegates to the internal runner with a nil tail (judge still runs if connected; state simply has an empty tail).
  - Internal runner `runDiscovery(query string, tail []Message)`: after the existing warm logic, replace `session.Discover` with `session.Select` (same 500 ms budget). If `Select` returns no new candidates → return (no judge call, no `OnDiscovery`). If `discoveryJudgeClient()` is nil → `Seed` all candidates (identical to today). Otherwise call `judgeDiscoveryCandidates`; on error → `emitDebug` with the error and `Seed` all candidates (fail-open); on success → `Seed` only `keep`. `OnDiscovery` and the existing `turn rank:` debug line report only what was seeded.

- [ ] **Step 1: Write failing tests** in `discovery_glue_test.go` (use `newGateAgent()`, `discovery.FakeEmbedder`, `newClientFn` override, `httptest` judge server as in Task 2):
  - `TestRunDiscoveryNoJudgeWhenTypesafeNotConnected` — `newClientFn` returns non-typesafe client; both docs attached; judge server never hit (assert request count 0).
  - `TestRunDiscoveryJudgeVetoesCandidate` — judge answers one keep / one veto → only the kept doc `IsAttached`; `OnDiscovery` names only the kept doc.
  - `TestRunDiscoveryJudgeFailureAttachesAll` — judge server 500 → both attached.
  - `TestRunDiscoveryVetoedDocReJudgedNextTurn` — after a veto, second `RunDiscovery` with the same query hits the judge again (request count 2) and, if it now answers keep, the doc becomes attached.
  - `TestRunDiscoveryNoJudgeCallWhenNothingNew` — everything already attached → judge request count stays 0.
- [ ] **Step 2: Run** `go test ./internal/agent/ -run TestRunDiscovery -v` → fail.
- [ ] **Step 3: Implement** the wiring; update the `agent.go` call site.
- [ ] **Step 4: Run** `go test ./internal/agent/ -run 'TestRunDiscovery|TestOnDiscovery|TestDiscoveryJudge'` and the full `go test ./internal/agent/ ./internal/discovery/` → PASS. Existing `TestOnDiscoveryCallback` must pass unchanged (its `newClientFn` is the default, which must not resolve a typesafe key in tests — verify `TYPESAFE_API_KEY` is unset in the test env; if the default factory could pick up a developer's real key, the test must override `newClientFn` to a non-typesafe client).
- [ ] **Step 5: Commit** `feat(discovery): gate embedder selections through the typesafe judge`.

---

### Task 4: Surface the veto in status/debug and TUI notice

**Files:**
- Modify: `internal/agent/discovery_glue.go` (`DiscoveryStatusInfo`, `DiscoveryStatus`, ~lines 388–450)
- Modify: `internal/tui/commands.go` (wherever `/discovery` status renders `DiscoveryStatusInfo`; grep `DiscoveryStatus()`)
- Test: `internal/agent/discovery_glue_test.go`

**Interfaces:**
- Produces: `DiscoveryStatusInfo.Judge string` — `""` when not connected, `"typesafe/jev-latest"` when the judge is active; `DiscoveryStatusInfo.JudgeVetoed int` — running count of vetoes this session (field on `discoveryState`, incremented in Task 3's runner).

- [ ] **Step 1: Write failing test** `TestDiscoveryStatusReportsJudge` — with a connected fake typesafe client the status names the judge model and, after one vetoed turn, `JudgeVetoed == 1`; not connected → `Judge == ""`.
- [ ] **Step 2: Run** → fail.
- [ ] **Step 3: Implement**: add the counter to `discoveryState`, populate the two fields in `DiscoveryStatus`, and render one extra line in the TUI `/discovery` status output (`judge: typesafe/jev-latest (vetoed N this session)`; omit the line when `Judge` is empty). Use single-width ASCII only (AGENTS.md rule on wide emoji).
- [ ] **Step 4: Run** `go test ./internal/agent/ ./internal/tui/` → PASS.
- [ ] **Step 5: Commit** `feat(discovery): report typesafe judge in /discovery status`.

---

### Task 5: Documentation

**Files:**
- Modify: `AGENTS.md` — the `typesafe` provider bullet (~line 21) lists its consumers; add the discovery judge (`judgeDiscoveryCandidates` in `internal/agent/discovery_typesafe.go`). In the discovery section (~line 913 "Tool sets that grow must be grow-only/sticky" and ~959 "Split tail injection by volatility") add one bullet: the judge runs between `Session.Select` and `Seed`, activates only when TypeSafe is connected, thresholds nouls on `permissions.auto.min_confidence`, fails open, and never alters the names-index.
- Create: `docs/concepts/discovery-typesafe-judge.md` — frontmatter matching `docs/concepts/server-auto-continue.md`; sections: what it does, activation condition, state/question shape, threshold, fail-open matrix (not connected / transport error / missing answer / real veto), how to observe (`/discovery` status, `DISCOVERY` debug lines), cost note (one `Decide` per turn that has new candidates, at most `SelectCap` questions).
- Modify: `docs/index.md` — add the new concept page to the concepts list.
- Modify: `CHANGES.md` — one entry under the current unreleased section.
- Modify: `skills/ocode-usage/SKILL.md` — one sentence in the discovery/typesafe area noting the judge activates automatically once the TypeSafe provider is connected.

- [ ] **Step 1: Write the docs** as listed.
- [ ] **Step 2: Verify** every function/file name in the docs exists (`grep -n` each).
- [ ] **Step 3: Commit** `docs: discovery typesafe judge`.

---

### Task 6: End-to-end verification

- [ ] **Step 1:** `go build ./... && go test ./internal/agent/ ./internal/discovery/ ./internal/tui/ ./internal/config/` → all PASS; paste the summary lines into the completion report.
- [ ] **Step 2:** Manual smoke with a real key: `TYPESAFE_API_KEY=… ocode` in a repo with discovery enabled, send a request unrelated to most skills, run `/discovery` → status shows the judge and a non-zero veto count; debug log shows per-candidate nouls. Without the key → status has no judge line and attachments match the previous behavior.
- [ ] **Step 3:** Report results honestly (including anything skipped) and stop.

---

## Self-review

- **Coverage:** connected-only activation (T2 `discoveryJudgeClient`, T3), skills+docs+MCP judged (T2 judges every candidate kind), threshold reuse (T2), fail-open (T2/T3 tests), no names-index change (T3 only changes seeding), observability (T4), docs (T5).
- **Placeholders:** none; every step names the test and assertion.
- **Type consistency:** `Session.Select` (T1) used in T3; `discoveryJudgeClient` / `judgeDiscoveryCandidates` / `buildDiscoveryJudgeState` (T2) used in T3; `DiscoveryStatusInfo.Judge` / `JudgeVetoed` (T4) used in TUI.

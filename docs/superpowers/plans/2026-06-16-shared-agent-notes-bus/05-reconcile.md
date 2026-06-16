# Part 05 — Reconcile: mechanical pre-pass, strong-model judgment, verify escalation, coverage-gap report

**Design:** `docs/superpowers/specs/2026-06-16-shared-agent-notes-bus-design.md`

**Context:** Parts 01–04 produce a finished group: a populated, archived log (notes,
write-touches, resolves) and per-agent completion statuses (completed / failed /
cancelled). Reconcile is the single place quality is paid for — it turns the cheap,
noisy log + each child's final report into one deduped, contradiction-resolved,
severity-ranked report, and it must surface unreviewed partitions rather than imply full
coverage. Small models never decide truth here.

---

### Task 1: Mechanical pre-pass (pure code, no model)

**Files:**
- Create: `internal/notebus/reconcile.go`
- Test: `internal/notebus/reconcile_test.go`

- [ ] **Step 1: Write the failing test**
  - Given a log snapshot + per-agent statuses, the pre-pass returns a structured
    clustering: entries grouped by `file`/symbol anchor, resolved notes dropped, exact
    duplicate notes collapsed (keeping provenance of all authors), and a list of agents
    whose status is `failed`/`cancelled` with their assigned partitions.
  - Determinism: same input → same output ordering (sorted by file then anchor then seq).
  - No model call, no network — pure function.

- [ ] **Step 2: Run and confirm failure**
  `go test ./internal/notebus -run TestReconcilePrepass -v`.

- [ ] **Step 3: Implement**
  Group, drop-resolved, dedup-exact, and attach the unreviewed-partition list. Output a
  compact structure suitable for handing to the strong model.

- [ ] **Step 4: Verify** — pre-pass test passes; output is sorted/deterministic.

---

### Task 2: Reconcile hand-off to the orchestrator (strong model)

**Files:**
- Modify: `internal/agent/subagent.go` (group result assembly returned to parent)
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test**
  - When an enabled group completes, the parent-visible result includes: the pre-pass
    clustering, each child's final report, and an explicit `Unreviewed partitions`
    section listing partitions of `failed`/`cancelled` agents.
  - The result is shaped so the orchestrator (main LLM) performs the judgment — the code
    does **not** itself rank severity or resolve contradictions.
  - A group where every agent completed has an empty unreviewed section.

- [ ] **Step 2: Run and confirm failure**
  `go test ./internal/agent -run 'TestReconcileHandoff|TestCoverageGap' -v`.

- [ ] **Step 3: Implement**
  After teardown, run the pre-pass and assemble the parent result (clustering + reports +
  coverage gaps). Return it as the group's task result so the orchestrator reconciles.

- [ ] **Step 4: Verify** — hand-off + coverage-gap tests pass.

---

### Task 3: Contradiction verify-agent escalation

**Files:**
- Modify: `the review-changes skill markdown` (reconcile phase instructions)
- Modify: `internal/agent/subagent.go` only if a structured re-spawn hook is needed
- Test: manual dry-run

- [ ] **Step 1: Define the check**
  - The reconcile instructions tell the orchestrator: when two notes contradict on the
    same anchor and it cannot settle them from the notes alone, spawn one focused verify
    agent that re-reads the actual code; the orchestrator decides whether to trust it.
  - The verify agent is a narrow single-purpose run (medium tier acceptable), not a new
    grouped fan-out.

- [ ] **Step 2: Confirm current behavior lacks this** (reconcile would otherwise pick a
  note arbitrarily).

- [ ] **Step 3: Implement**
  Add the escalation rule to the skill's reconcile phase. If a programmatic re-spawn hook
  is cleaner than prose instruction, expose a minimal one; otherwise rely on the existing
  `task` tool.

- [ ] **Step 4: Verify** — dry-run a diff with a planted contradiction; confirm the
  orchestrator spawns a verifier and the final report reflects the verified verdict, not
  an arbitrary pick.

---

### Task 4: End-to-end integration test

**Files:**
- Create: `internal/agent/notes_e2e_test.go`
- Test: same

- [ ] **Step 1: Write the failing test**
  Drive a fake-LLM group of 3 children with `shared_notes:true` over a small synthetic
  change set and assert end to end:
  - children receive first-loop brief; one child's note appears in the others' next
    delta; nothing-new loops inject nothing.
  - a child `edit` surfaces as a touch to peers.
  - one child fails → its partition is flagged unreviewed in the result.
  - the log persists to the sidecar and reloads with recovered seq + watermarks.
  - a forged `</oc-note>` body cannot create a second entry.

- [ ] **Step 2: Run and confirm failure**
  `go test ./internal/agent -run TestNotesBusE2E -race -v`.

- [ ] **Step 3: Implement**
  Fill any gaps surfaced by the e2e test; do not weaken earlier unit tests to make it
  pass.

- [ ] **Step 4: Verify** — e2e passes under `-race`; full `go test ./...` green.

---

## Part 05 Done When

- Mechanical pre-pass deterministically clusters/dedups/drops-resolved and lists
  unreviewed partitions.
- The orchestrator receives a hand-off shaped for strong-model judgment; severity and
  contradiction resolution are not done in code.
- Contradictions escalate to a focused verify agent; the final report reflects verified
  verdicts.
- The end-to-end test exercises brief → delta → touch → failure-gap → persist/reload →
  forgery, all green under `-race`.

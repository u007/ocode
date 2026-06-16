# Part 04 — Child + orchestrator prompts

**Design:** `docs/superpowers/specs/2026-06-16-shared-agent-notes-bus-design.md`

**Context:** Parts 01–03 deliver the mechanism: bus, group lifecycle, touches, note
emit/parse/escape, cache-preserving delta injection. Mechanism without instruction is
useless — a child must know its name, when to share, and that notes are leads not facts;
the orchestrator must know how to seed the brief, stay passive, and trigger reconcile.
This part is prompt/text wiring only — no new control flow.

---

### Task 1: Child agent prompt additions (name, emit policy, leads-not-facts)

**Files:**
- Modify: `internal/agent/subagent.go` (system-prompt assembly for grouped children)
- Modify: `internal/agent/prompt.go` (shared prompt fragments, if present)
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test**
  - A child spawned in an enabled group has, in its system prompt: its own id
    (`You are agent a2`), the emit format (`<oc-note at="symbol-or-snippet">caveman
    text</oc-note>`), and the rule that `seq`/`by` are filled by the system.
  - The prompt states the **emit policy**: share only cross-agent-value findings (a
    cross-cutting fact, a claim/edit on a shared file, a risk that changes another
    agent's scope); keep own-report-only findings out of the bus.
  - The prompt states **leads-not-facts**: received notes are leads to verify, not
    confirmed truth.
  - A child in a disabled/single run gets **none** of this text (assert absence).

- [ ] **Step 2: Run and confirm failure**
  `go test ./internal/agent -run TestGroupChildPrompt -v`.

- [ ] **Step 3: Implement**
  Append a notes-protocol fragment to grouped children's system prompt, parameterized by
  id. Gate strictly on bus presence so non-grouped runs are unchanged.

- [ ] **Step 4: Verify** — prompt presence/absence test passes.

---

### Task 2: Orchestrator brief-seeding at group start

**Files:**
- Modify: `internal/agent/subagent.go` (group construction, before child spawn)
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test**
  - When an enabled group is created, the orchestrator's pre-computed shared brief
    (change set summary + per-agent partition assignment, supplied by the caller) is
    appended to the bus as the first entries, authored as `by="main"`.
  - Each child's first-loop delta therefore contains the brief (since `by != agent`).
  - No brief supplied → group still works, bus simply starts empty.

- [ ] **Step 2: Run and confirm failure**
  `go test ./internal/agent -run TestBriefSeeding -v`.

- [ ] **Step 3: Implement**
  Accept an optional brief on the group-spawn path; append its entries before spawning
  children. Keep it data the orchestrator computes — this part does not compute it.

- [ ] **Step 4: Verify** — seeding test passes.

---

### Task 3: `/review-changes` skill rewrite to use the group + reconcile

**Files:**
- Modify: `the review-changes skill/command markdown` (locate via the command registry;
  the skill currently delegates to a single `code-reviewer` subagent)
- Test: manual + a skill-presence assertion if the command registry is unit-tested

- [ ] **Step 1: Write/adjust the failing check**
  - The skill instructs the orchestrator to: (a) establish the change set + caller map +
    doc-rule digest once (the brief), (b) spawn a grouped fan-out with `shared_notes:true`
    partitioned by dimension (or by file for large diffs), (c) run reconcile at the end.
  - The skill no longer tells one agent to do all phases serially.

- [ ] **Step 2: Confirm current skill fails the new check** (it delegates to a single agent).

- [ ] **Step 3: Implement**
  Rewrite the skill body: Phase 1+2 → brief; Phase 3–5 → per-dimension grouped agents
  with the notes protocol; new closing phase → reconcile (Part 05). Preserve the existing
  report format and interactive-resolution section.

- [ ] **Step 4: Verify** — re-read the skill; manually dry-run on a small diff and confirm a
  group spawns with notes and a single reconciled report is produced.

---

## Part 04 Done When

- Grouped children carry id + emit policy + leads-not-facts; non-grouped runs are
  unchanged.
- The orchestrator seeds the brief as `by="main"` entries visible in every child's first
  delta.
- `/review-changes` drives a grouped, notes-enabled fan-out + reconcile instead of a
  single serial reviewer, preserving its report/interaction format.

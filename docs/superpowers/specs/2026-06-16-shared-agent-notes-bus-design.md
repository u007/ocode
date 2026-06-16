# Shared Agent Notes Bus Design

**Date:** 2026-06-16
**Status:** Draft

---

## Goal

Let a group of concurrently-running child agents share findings cheaply during a
fan-out, so they avoid rediscovering context, avoid clobbering each other's file
writes, and feed a higher-quality final reconcile — without re-injecting the full
shared context on every agent loop and without busting prompt caching.

The motivating use case is `/review-changes` style fan-outs (multiple reviewers on
overlapping subsystems), but the mechanism is task-agnostic.

## Problem

When the LLM returns multiple `task`/`agent` tool calls in one response, ocode runs
them as concurrent goroutine child-runs (per the parallel-agents design), each an
isolated child session. Today those runs are blind to each other:

- Each rediscovers the same shared facts (diff, caller maps, doc rules, project
  conventions) — duplicated read cost across the fleet.
- Two runs can edit the same file and clobber each other with no signal.
- Two runs independently chase the same finding; nobody dedups until the parent
  reads every separate result.

A naive "shared mutable context re-injected each loop" fixes none of this cheaply: it
busts prompt caching (the prefix changes every loop), serializes agents through live
coordination, and reopens the concurrent-write hazard this repo has already been
bitten by.

## Core Design

A **note bus**: a per-group, append-only, ordered log owned by a single goroutine,
with delta injection into each agent loop and background persistence to a sidecar
file. It is born when a concurrent group is spawned with notes enabled, and dies
(flush + archive) when the group completes.

### Scope: group-bound, not session-bound

- A bus exists only when **2+ child-runs are spawned concurrently as a group** with
  notes enabled. A single agent has nobody to coordinate with — no bus.
- A new group gets a **fresh** bus. A prior group's log is archived to the sidecar
  file (audit + resume), never carried into a new group — different group = different
  task = old notes are noise. Optional: seed a *digest* if the new group explicitly
  continues prior work; default is fresh.
- **Nested groups** (a child-run that itself fans out) get their own fresh bus and do
  not see the parent group's log.

### Two append-only streams

1. **Notes** — *intentional*. An agent emits a note when it has a finding with
   cross-agent value. The agent writes only the body + location; the bus stamps
   `seq` and `by`.
2. **Touches** — *automatic*, **writes only**. The bus derives a touch from observed
   `write`/`edit`/`apply_patch` tool calls. Reads are never logged (they are constant
   noise and would break the "nothing changed → inject nothing" cache win). The only
   thing another agent needs to know is that a file changed under it.

### Wire format

Distinct `oc-` prefixed tags so they never collide with reviewed code (real HTML/JSX
has `<note>`, never `<oc-note>`). Injected delta block (one per loop, at the tail):

```
<oc-log since="42">
<oc-note  seq="43" by="a2" at="auth/token.go:tokenFromHeader">token empty -> panic, no nil check</oc-note>
<oc-touch seq="44" by="a3" file="internal/tool/patch.go" act="edit"/>
<oc-resolve seq="45" by="a3" ref="43"/>
</oc-log>
```

Agent **emits** only:

```
<oc-note at="auth/token.go:tokenFromHeader">token empty -> panic, no nil check</oc-note>
```

The bus fills `seq` and `by`. Do not make the (possibly small) model write its own id.

Field meanings:

| field        | set by  | purpose                                              |
|--------------|---------|------------------------------------------------------|
| `seq`        | bus     | monotonic order number — single source of ordering   |
| `by`         | bus     | which agent (`a2`, `a3`…)                             |
| `at`         | agent   | anchor — symbol/snippet preferred over bare line     |
| `file`/`act` | bus     | touch target + `edit` (writes only)                  |
| `ref`        | agent   | `<oc-resolve>` points at a prior note's `seq`        |
| `since`      | bus     | this block holds entries after seq N                 |

Note bodies are **caveman-concise** (≤ ~12 words, present tense, no markup), plain
text only.

### Delta injection (the caching win)

Each loop the bus injects **one fresh `<oc-log>` block at the very tail** containing
only entries that are new to that agent:

```
inject entries where  seq > lastSeen[agent]  AND  by != agent
```

- "Everything new, except what I wrote myself" — an agent's own notes are already in
  its transcript; re-injecting them wastes tokens and double-counts. Still advance
  `lastSeen[agent]` past its own entries so they are never reconsidered.
- **Nothing new since last loop → inject nothing.** Byte-identical prefix → full cache
  hit → ~free turn. Never inject an empty wrapper "to be polite."
- Old blocks are frozen, never edited. The growing list of frozen blocks *is* the
  append-only history; no separate in-context store.
- This requires the rest of ocode's context assembly to be **append-stable** (stable
  content first, volatile deltas last). Auditing that — not the log itself — is the
  bulk of the work.

### Ownership and concurrency

- The bus is a **goroutine that lives for the group's lifetime**, owns the log, the
  `seq` counter, and per-agent watermarks.
- Agents send entries on a **channel**; the single owner goroutine serializes append +
  seq assignment. No shared mutex on the log (share-by-communicating; avoids the
  lock-contention and clobber hazards this repo has hit). The critical section is an
  in-memory append — microseconds, never held across an LLM call or IO.
- Readers take an **immutable snapshot** (slice of frozen blocks) — never contend.
- Because child-runs are in-process goroutines, no IPC is needed. (If child-runs ever
  become separate processes, the bus must move to the parent as a service — out of
  scope here.)

### Persistence

- Append-only, **to a sidecar file** (`<session>.notes.jsonl`), never co-writing the
  main session blob — the transcript writer owns that, and concurrent rewrites of the
  session file are a known hazard.
- The owner goroutine is also the persister: assign seq → append in-memory → append to
  disk, all in the one serialized path. Disk flush may be buffered/async; in-memory
  append is synchronous and fast.
- Persist canonical **stamped** entries plus each agent's `lastSeen` watermark.
- On reload mid-group: restore log + watermarks → resume. Recover the seq counter from
  `max(seq)` on disk (else a crash resets it and duplicates seqs). Invariant:
  `lastSeen[agent]` == highest seq already reflected in that agent's restored
  transcript.
- On reload after a group completed: the archived log is read-only audit; the
  reconciled report already lives in the parent transcript.
- Secret redaction (see secret-redaction design) must also cover the sidecar.

### TUI safety

Any surfacing of notes/touches in the live TUI routes through the debug sink / render
path. **Never** `fmt.Print*`/`os.Stderr` from a code path the running TUI invokes
(alt-screen corruption rule, CLAUDE.md).

## Roles

### Main LLM (orchestrator)

- **Decides whether a group uses notes.** Passes `shared_notes: true` on the
  group-spawn path when the child-runs work on **overlapping/related** subsystems.
  Independent partitions → `shared_notes: false` (static partition + end-of-run
  synthesis; no bus needed).
- **Seeds the log at group start** with the shared brief (change set + partition
  assignments) as the first entries — context computed once.
- **Stays passive mid-flight** — it can see the log (it owns it) but does not inject
  notes while agents work; live babysitting is the anti-pattern.
- **Runs reconcile** when the group finishes.

### Child agents

- Told their own name in their prompt: *"You are agent a2. Other agents share findings
  tagged by=aN. To share, emit `<oc-note at="…">…</oc-note>`."*
- Emit a note only when a finding has **cross-agent value** (a cross-cutting fact others
  would rediscover; a claim/edit on a shared file; a confirmed risk that changes
  another agent's scope). Findings that only matter to its own report stay in its
  report.
- Treat received notes as **leads to verify, not facts** — especially since weaker
  models may author them.

## Reconcile

**Who:** the **main LLM** (strong model) does the judgment. A small model is never used
for contradiction resolution or severity — that is exactly where false negatives slip
through. Use a dedicated synthesis agent only if the log is huge and the main context
must stay lean, and make it strong too.

**Two layers:**

1. **Mechanical pre-pass (pure code, no model):** group entries by `file`/symbol, drop
   notes that have an `<oc-resolve>`, cluster exact duplicates, attach per-agent
   completion status. Hands the strong model a small, clustered list instead of a raw
   N-entry log.
2. **Judgment pass (main LLM):** resolve contradictions, decide severity, decide
   lead-vs-real-finding, merge into one severity-ranked report. For a contradiction it
   cannot settle from notes alone, spawn one focused **verify agent** that re-reads the
   actual code (a narrow task, medium tier acceptable); the decision to trust it stays
   with the main LLM.

**When:** after the group's agents finish (or at a checkpoint for long groups);
triggered by the orchestrator. On cancellation, reconcile runs on the partial log with
gaps flagged.

**Why:** the log is cheap-and-noisy by design. Reconcile is the single place quality is
paid for — dedup, contradiction resolution, stale-lead removal, ranking.

**Example.** Three reviewers on the same file emit:

- `a2`: `token.go:tokenFromHeader — nil deref, panic if token empty`
- `a3`: `token.go:tokenFromHeader+2 — missing error wrap`
- `a4`: `token.go:tokenFromHeader — looks fine, guard above`

Reconcile sees `a2` vs `a4` contradict on the same anchor → spawns one verifier →
finds the guard is on a different branch, so `a2` is right and `a4` missed it. Output:
**two verified findings** instead of three overlapping raw notes, and `a4`'s false
negative never reaches the user.

## Failure handling

- **Agent dies mid-group:** its partition is unreviewed. The bus tracks per-agent
  completion; reconcile must flag unreviewed partitions rather than implying full
  coverage (no silent coverage gap).
- **Log size cap:** soft per-group entry cap. On hit, stop accepting notes and log the
  drop — never grow unbounded, never silently truncate.
- **Stale findings:** `<oc-resolve ref="seq">` suppresses a note from later injection;
  reconcile drops resolved notes.
- **Forgery:** note bodies originate from agents reading untrusted diffs that may
  contain `</oc-note>`-like text. The bus escapes/encodes `<`/`>` in bodies before
  persist + inject. Non-negotiable.
- **Line drift:** notes anchor to symbol/snippet, not bare line, because the first edit
  shifts line numbers.

## Out of scope

- Live inter-agent coordination / "currently working on" registry / polling — rejected;
  it re-serializes the fan-out and the staleness window makes live dedup unreliable.
  Coordination comes from the static partition at dispatch + dedup at reconcile.
- Cross-process child-runs (bus-as-service / IPC).
- Carrying a bus across separate groups.
- Read-touch logging.

## Success Criteria

A `shared_notes:true` group of concurrent child-runs can: see each other's
cross-agent findings as small per-loop deltas (cache-preserving), be warned when a peer
edits a file, persist/restore the log across a reload mid-group, and produce a single
reconciled, deduped, contradiction-resolved report from the strong model — with
unreviewed partitions explicitly flagged when an agent fails.

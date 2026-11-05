# 05 — Phases, cost, risks, open decisions

## Phase 0 — prerequisites (no new concept, all fixes)

1. DONE 2026-11-05: memory resolves from the session root (`LoadContext` passes `root` to
   `memory.PromptFragment`; `""` keeps the legacy cwd fallback).
2. DONE 2026-11-05 for web/desktop: `handleServerJobEvent` queues memory maintenance for durable job
   events only (Pulse sessions and synchronous agent runs excluded; deferred to turn end while the live transcript is busy, one pass per turn). Still open: the scheduler runner (`RunScheduledJob`) does not queue it after a cron run.
3. Add `Stop` and `SessionEnd` hook events in the plugin-hook format; no consumer yet besides tests.
4. `/reset-id` journal move list gets a placeholder for session-keyed learning data.

Exit: web/desktop sessions update memory; a Finder-launched desktop app injects the right project's memory.

## Phase 1 — capture + feedback (value on its own: visibility)

- Signal recorder in the agent loop and `signals.jsonl` per project; `signals_summary` session metadata.
- `message_rating` and `session_feedback`: TUI keys + `/feedback`, web thumbs + command, API routes.
  Log-only.
- `GET /api/sessions/{id}/signals`; sessions-list badge; `/kaizen-review` reads the signal file instead of
  grepping the transcript.

Exit: a session's errors, retries, undos, stops, corrections and ratings are countable without an LLM.

## Phase 2 — reflect + approve (the loop, human-gated)

- `internal/learning` store with the lifecycle, flock, revisions, archive.
- `learn` sub-agent with `lesson_propose`; reflection worker triggered at turn end (budgeted) and session end.
- `slotLearning` structural judge (`candidate → trial`).
- `/lessons`, `/learn` re-purpose, sidebar row, web Lessons panel with approval cards, Settings form.
- Lessons tail injection (`active`, plus `trial` for the two auto kinds only), relevance-selected, bounded,
  with the random withhold recorded per turn.
- `memory:<scope>` promotion renders a `## Lessons` block from the store at agent build (memory files untouched).
- Deferred `reflect_pending` marker on session end; `signals_summary` added to `sessionOwnedMetadataKeys`.

Exit: a correction the user gives on Monday shows up as an approved lesson in Tuesday's session, with a quote.

## Phase 3 — measure + auto-gate (the recursion)

- Trial window: per-lesson eligible-session set split into shown vs randomly withheld (matched holdout),
  targeted signal rates compared between the two halves. Phase 2 already records `eligible`/`shown`/`withheld`
  so the window has data on day one of phase 3.
- Memory planner prompt stops writing "reusable lessons" into `global.md`; promoted lessons render from the
  store.
- Auto `trial → active` and `active → demoted` for `memory_fact` / `tool_guidance`; dedupe consolidation job
  on the scheduler (Jev `same / refines / distinct`); cross-project roll-up to `global` when the same lesson is
  active in ≥ 2 projects.
- Model tagging: a lesson is `trial` again under a new model.
- `question` kind → Kaizen corpus draft.

Exit: a lesson that does not reduce its signal is demoted without anyone reading it.

## Phase 4 — optional: Propose-Probe-Compose for scheduled jobs

Per-job opt-in, 3× cost, candidates are edits to the job's notes and attached skills only, selector ranks
anonymised deliverables, user approves from the Cron panel. Build only if phase 3's measurements show that
lessons for recurring jobs are where the value is.

## Cost model

| stage | LLM cost | when |
|---|---|---|
| capture | none (one Jev choice per user message for `user_correction`, ~1 k tokens of state) | every turn |
| reflect | one small-model sub-agent run, bounded input (signals + windows ≤ ~12 KB, same cap as the memory planner) | only turns with a negative signal, or every N turns; session end |
| gate | one Jev decision per candidate | per candidate |
| inject | ≤ 2 KB of user-role tail; one Jev relevance call per turn only when > K lessons exist | per turn |
| consolidate | one scheduled small-model job | daily |

Nothing here touches the cached `tools` or `system` prefix except a human promotion into memory.

## Risks and mitigations

| risk | mitigation |
|---|---|
| **Prompt injection through lessons.** Tool output (web page, repo file) tells the agent to "remember to always run X"; the reflector dutifully proposes it | reflector input is signals + quoted windows, never raw tool output; lessons are data in a marked user-role block; `rule` kind needs a human; the writable-target set is closed and excludes permissions, `CLAUDE.md`, hooks, settings; the structural judge rejects guidance that names commands with side effects outside the project |
| **Learning the wrong thing** (Harness² direct-refinement regression, PAST-Bench memory not helping) | evidence required; trial window demotes; auto-promotion only for the two low-stakes kinds; archive never delete |
| **Redundancy / bloat** (Recuris 17 near-duplicates) | dedupe job, caps (K, bytes, per-scope count), archive |
| **Cache busting** | user-role tail only, byte-stable ordering, omitted when empty; memory section changes only on human promotion |
| **Cost creep** | reflection budgeted to negative-signal turns; small model; per-project kill switch |
| **Wrong-project lessons** on a multi-project server | phase 0 root fix; store keyed by project slug like sessions |
| **Model drift** | model tag; re-trial under a new model |
| **Privacy**: evidence quotes contain user text and code | every lesson keeps its quotes in every scope (a quote-less lesson does not parse); the store stays under `$GD` with the sessions, is never synced, and export is an explicit user action |
| **Users ignore the approval queue** | queue is capped (oldest candidates archive after 30 days); trial injection still works for the two auto kinds; a `lessons N pending` row, not a modal |

## Open decisions (recommendation first)

1. **Store format.** JSONL event log + materialised view (recommended; matches `runs.jsonl`, append-only,
   diffable) vs sqlite per project (matches sessions, better for queries once there are thousands of
   signals). Start JSONL; signals may move to sqlite in phase 3 if the window math gets slow.
2. **Reflector model.** Small model (recommended; same as memory planner) vs session model. The session model
   writes better lessons but costs a full-size call per reflection.
3. **Trial window default.** 5 shown + 2 withheld eligible sessions (recommended) vs 10 + 4. Smaller windows
   are noisier (Recuris's 10-14-task dev sets rejected good patches); larger windows delay demotion. Withhold
   probability 0.3 in trial, 0.1 when active.
4. **Should `global.md`'s existing "global history" content migrate?** Recommended: leave it; the memory
   planner stops writing lessons there from phase 2, and the user can promote lessons into it.
5. **Rating granularity.** Thumbs only (recommended for phase 1) vs DSH's category taxonomy from day one.
6. **Phase 4 at all.** Decide after phase 3 numbers.
7. **Sub-agent name.** `learn` (recommended) vs extending `context`. Separate keeps the OKF sole-writer and
   the lesson sole-writer independently auditable (`TestLessonToolsAreOnlyForTheLearnAgent`, like the Pulse
   terminal-tool test).

## Tracking

If accepted: a `TODO.md` section "Recursive learning (plan 2026-11-05)" with one entry per phase, and a
`docs/concepts/recursive-learning.md` concept page written by the `context` agent once phase 2 lands, plus
the invariant block in `CLAUDE.md`.

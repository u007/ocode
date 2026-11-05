# 03 — Design: the lesson loop

## Vocabulary

- **Signal**: a structured, machine-recorded event that says something went well or badly in a session.
  Never prose. Never enters the prompt.
- **Lesson**: a conditional guidance record, *when `<situation>`, do `<guidance>`, because `<evidence>`*, with a
  scope, a kind, a target component, a model tag and a lifecycle status. Lessons are **data**: the model reads
  them, a sub-agent proposes them, the gate and the user decide them.
- **Reflection**: the pass that turns a session's signals + transcript into candidate lessons.
- **Gate**: the decision that moves a lesson between statuses. The recursive part of the design lives here.

Memory (`user.md` / `project.md` / `global.md`) stays what it is: human-readable facts and preferences. A
lesson is not a memory fact; it carries evidence and can be retracted by measurement. The memory planner's
"global history for reusable lessons" role moves to the lesson store so lessons stop being rewritten in a
32 KB full-body replacement with no provenance.

## The four stages

```
 every turn, every host          end of turn / idle / /learn         user + Jev             next sessions
 ┌──────────┐                    ┌───────────────┐                  ┌─────────┐          ┌──────────────┐
 │ CAPTURE  │ signals.jsonl ───▶ │ REFLECT       │ candidates ────▶ │ GATE    │ active ─▶│ INJECT       │
 │ (agent)  │                    │ (learn agent) │                  │ (judge, │          │ [ocode:lessons]
 └──────────┘                    └───────────────┘                  │  user)  │          │ user-role tail│
      ▲                                                             └────┬────┘          └──────┬───────┘
      │                                   trial window: did the signals improve? ◀──────────────┘
      └──────────────────────────────────────────── demote / archive ◀──┘
```

### 1. Capture (agent core, both hosts)

Recorded by the agent loop itself, so TUI, server, desktop and scheduled runs all produce the same stream.

| signal | source | note |
|---|---|---|
| `tool_error` | tool result marked error | tool name, error class, args hash |
| `tool_retry_same` | same tool + same args hash within N calls | the "model repeated a failed call" pattern `/kaizen-review` greps for |
| `permission_denied` / `permission_ask` | permission path | includes judge verdict and confidence (already in `permission-judge.log`) |
| `undo` | `undo_file_change` call, Changes-tab undo | which write call was undone |
| `stop` | Stop/Esc cancellation | the round that was cancelled |
| `user_correction` | a user message following an assistant turn, classified by a Jev **choice** question (`correction` / `approval` / `new_request` / `clarification`) | the only judged signal; low confidence → `unknown`, never guessed |
| `compaction_extract` | the **User Directives** and **Errors & Fixes** sections of a compaction summary | reuses work compaction already does; copied verbatim at compaction time |
| `goal_verdict`, `auto_continue_action` | goal loop, auto-continue judge | `met` / `impossible` / action key |
| `task_contract` | `expected_output` verifier outcome | `satisfied` / `not_satisfied` / `check_failed` |
| `message_rating` | explicit thumbs up/down + optional category + note | DSH model: **log-only, never in model history** |
| `session_feedback` | `/feedback <text>` | one remark per session, categorised |
| `turn_done` | every turn | tokens, tool-call count, model, duration; the denominator for rates |

Storage: append-only `$GD/project/<slug>/learning/signals.jsonl` (same shape as `scheduler/runs.jsonl`),
keyed by session id, message seq, model, through one `learning.Store.AppendSignal(project, session, signal)`
API. Two producers write to it: the agent loop (tool, permission, cancel, compaction, judge signals) and the
hosts (ratings, `/feedback`, Changes-tab undo), so a rating on a session with no live agent still lands. The
session store gains one metadata key `signals_summary` (counts) so pickers and Pulse can show it without
reading the file; it is **session-owned** (added to `sessionOwnedMetadataKeys` next to `goal`) so the TUI's
carry-forward copy cannot overwrite it. `$GD/usage/records.jsonl` is left alone.

### 2. Reflect (sole-writer sub-agent)

A new built-in sub-agent `learn`, modelled on `context` (the OKF sole writer): it is the **only** agent path
that holds `lesson_propose` (and later `lesson_update`); the main agent never gets it. It runs:

- at turn end on a **budget**: only when the turn produced at least one negative signal, or every N turns,
  whichever is rarer. A clean turn costs nothing.
- at session end / idle, on the full session. This is **deferred and idempotent**, never tied to the agent's
  own worker: the memory-maintenance worker drops queued requests once shutdown starts, so a reflection queued
  on `/exit` would never run. Instead the host appends a `reflect_pending` marker (with the last reflected
  `seq`) to the session's signals, and the next agent build in that project on any host, or the scheduled
  consolidation job, runs the reflection for every session with a pending marker. Running it twice is a
  no-op because the marker carries the last reflected `seq`.
- on `/learn` (re-purposed: today it only prints a prompt).
- as a scheduled consolidation job (dedupe, demote stale, roll up cross-project patterns to global scope).

Inputs: the session's signals, the transcript window around each negative signal (uncompacted, from the
sqlite store, the way `/kaizen-review` reads it), existing lessons for this project + model (to update rather
than duplicate), and the memory files (to avoid restating facts).

Output: zero or more candidate lessons, each with:

| field | values |
|---|---|
| `scope` | `user` (this human, every project) / `project` / `global` (every project, every human on this machine) / `stack:<name>` |
| `kind` | `rule` (always-on guidance), `memory_fact`, `tool_guidance` (one tool), `skill_patch` (proposes a change to a named skill), `question` (a Kaizen corpus question draft) |
| `target` | the ocode component it would edit: `lessons-tail` (default), `memory:<scope>`, `skill:<name>`, `okf:conduct` |
| `model` | the provider-stripped model id the evidence came from, or `*` |
| `situation`, `guidance` | short, imperative; `guidance` ≤ 400 chars |
| `evidence[]` | `{session, seq, quote}` — verbatim, at least one; a lesson without a quote is rejected at parse |
| `signals[]` | which signal kinds it is meant to reduce (the metric the trial window watches) |
| `status` | `candidate` |

The reflector prompt follows Harness²'s editor rules: every lesson cites a trajectory moment; prefer the
narrowest target (`tool_guidance` over `rule`, `skill_patch` over `rule`); `KEEP` (no lessons) is a
legitimate answer; deliverable shape and user intent are never "lessons".

Store: `$GD/project/<slug>/learning/lessons.jsonl` and `$GD/learning/lessons.jsonl` (user + global scopes),
**append-only event logs** (`propose`, `approve`, `reject`, `promote`, `demote`, `archive`, `edit`) with the
current state materialised in memory, mutated under a flock with a cited revision, exactly the todo-plan
protocol. Markdown is not used for the store: a lesson needs structured fields and a measurable lifecycle.

### 3. Gate (where the recursion is)

Status machine: `candidate → trial → active → archived`, with `rejected` and `demoted` as terminal/side states.

| transition | who decides | rule |
|---|---|---|
| `candidate → trial` for `kind=memory_fact`, `tool_guidance` only | Jev slot `slotLearning` (own floor; starts 0.7) on structure: is the guidance specific, is it supported by the quoted evidence, is it not a user intent or a one-off | a reject keeps the candidate for the user to see; never silently dropped. `rule`, `skill_patch`, `question` and anything with `target != lessons-tail` never enter `trial`: they wait as `candidate` and reach the model only after a human |
| `candidate → active` for `kind=rule`, `skill_patch`, anything with `target != lessons-tail` | **the user**, in the TUI or web approval surface | no auto-promotion for anything that changes always-on text or a skill |
| `trial → active` for `kind=memory_fact`, `tool_guidance` | automatic after the trial window | **matched holdout, not a baseline comparison**: on every turn where the relevance judge selects a trial lesson, the injector withholds it with probability `p` (default 0.3) and records `eligible` + `shown`/`withheld`. A session is *eligible* when the lesson was selected on any turn. After ≥ N eligible sessions (default 5 shown, 2 withheld) the targeted signal rate in shown sessions must not exceed the withheld sessions' rate; otherwise `demoted`. Comparing against the project-wide baseline is wrong because a lesson is selected exactly in the sessions that trigger its signal |
| `active → demoted` | the same holdout measurement, rolling (active lessons keep a smaller `p`, default 0.1), or user | — |
| `* → archived` | user, or consolidation job | archived lessons stay on disk with their history; never deleted |
| duplicate | consolidation job (Jev choice: `same` / `refines` / `distinct`) | merge evidence into the survivor |

The evaluator is outside the writable scope: the signals come from the agent loop and the human, the
reflector cannot write signals, and the gate reads only signals. This is the one property every external
result says is load-bearing.

### 4. Inject

- **`active` lessons of any kind with `target=lessons-tail`, plus `trial` lessons of kind `memory_fact` /
  `tool_guidance`** (a `rule` is never shown before a human approved it): one **user-role** tail block
  `[ocode:lessons] … [/ocode:lessons]`, after the todo re-anchor, selected per turn by the discovery
  relevance judge (same path as skill auto-inject: index of `situation` lines → Jev relevance → top-K),
  capped at K=5 lessons / 2 KB. Trial lessons are tagged `(trial)` so the reflector can see what was in
  effect, and are withheld on a random fraction of eligible turns for the holdout measurement. The block is
  omitted when empty, so a project with no lessons sends a byte-identical transcript.
- **Promoted lessons the user chose to make permanent** (`target=memory:<scope>`): rendered **from the lesson
  store** by `memory.PromptFragment` as a `## Lessons` block appended after the memory file bodies at agent
  build. The memory `.md` files are not written, so the memory planner's full-body replacement cannot drop
  them. In the same phase the planner prompt stops asking for "reusable lessons" in `global.md`; that role
  belongs to the store. This is the only path into the cached system prefix, and it changes only when a human
  promotes or archives.
- **`skill_patch`**: never applied automatically. Approval opens the existing `skill-creator` flow with the
  patch as its brief; the write goes through the normal edit tool with the read-before-write contract.
- **`question`**: handed to the Kaizen corpus as a draft under `docs/okf/<stack>/` for the usual closed-book
  loop; the derived skill, not the lesson, is what ships.

Scheduled runs and sub-agents get the tail too (project scope), the Pulse assistant does not (its own memory).

## Where Harness² Propose-Probe-Compose fits: scheduled jobs

A scheduled job is the one ocode surface that re-runs the same task. Opt-in per job ("tune this job"): on
firing, run the reference (current job prompt + skills), ask the `learn` agent for one invariant and one domain
candidate (edits to the job's `notes` and attached skills only), run them, let a selector rank anonymised
deliverables, and record the composed variant as a `candidate` lesson with target `job:<id>`. The user approves
from the Cron panel. Cost is 3× the job; it is never on by default, it is the last thing to build, and it may
be dropped.

## Invariants (to carry into CLAUDE.md if built)

1. **The main agent never writes lessons or signals.** `lesson_propose` exists only on the `learn` sub-agent;
   signals are recorded by the loop, not by a tool.
2. **Lessons are data.** They are quoted into a user-role block with a marker, never into `system`, never into
   a tool description, never executed. A lesson can never add a permission rule, edit `CLAUDE.md`/`OCODE.md`,
   `.ocode/settings.json`, hooks, or permission-defining files; the writable-target set is closed
   (`lessons-tail`, `memory:<scope>`, `skill:<name>` via approval, `okf:conduct` as a draft).
3. **Human feedback never enters model history.** Ratings and `/feedback` are signals.
4. **Nothing is auto-promoted into always-on text, and a `rule` is never shown to the model before a human
   approved it.** Only `memory_fact` and `tool_guidance` enter `trial`, pass the holdout window on their own,
   and can be demoted by it.
5. **Every lesson cites verbatim evidence** or it does not parse. Evidence stays with the lesson in every
   status and scope, under `$GD`, never synced or exported implicitly.
6. **The tail is bounded and byte-stable when nothing changed**: same K, same ordering (by id), omitted when
   empty, so the Anthropic breakpoints and the system cache are untouched.
7. **Reflection is budgeted and asynchronous**; it never blocks a turn, never holds `Handler.mu` or
   `agentSession.mu`, and uses the small model unless configured otherwise.
8. **Model-tagged.** A lesson learned under one model is `trial` for another until its own window passes
   (the Kaizen `tuned_for` lesson).
9. **Append-only store, flock + cited revision for mutations, archive instead of delete.**
10. **`/reset-id` moves the session's signals** with the session (new session-id-keyed journal rule).

## What this is not

- Not fine-tuning, not RL, not a self-modifying tool loop. The model and the Go harness stay fixed; only
  data the model reads changes, and the user owns the objective.
- Not a replacement for Kaizen: Kaizen validates stack knowledge offline per model; this loop validates
  conduct and project lessons online per project. The `question` kind is the bridge.

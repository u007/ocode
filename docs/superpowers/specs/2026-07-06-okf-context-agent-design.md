# OKF Knowledge Bundle + Context Agent — Design Spec

Date: 2026-07-06
Status: approved for planning

## Summary

Introduce an [OKF v0.1](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md)
knowledge bundle rooted at the project's `docs/` directory, curated by a new
`context` subagent that is the only writer of the bundle. The system activates
with the existing `/docs` toggle. Reads are cheap (index injected into the
system prompt + on-demand small-model lookups); writes happen through a gated
post-job maintenance worker modeled on memory maintenance. A new `task_cancel`
tool lets the main agent race `context` against `explore` and kill the loser.

## Goals

- Durable, human-readable project knowledge (decisions, rationale, playbooks,
  schemas, gotchas) in git-committed `docs/`, OKF v0.1 conformant.
- Main agent consults knowledge cheaply before exploring code.
- Knowledge stays fresh automatically: post-job triage creates, updates, or
  deprecates docs without user prompting, gated so chatty sessions are noops.
- Small-model routing for all context-agent work, with automatic fallback to
  the main client (existing `smallModelEligibleNames` mechanism).

## Non-goals

- Replacing the discovery engine (`internal/discovery/`) — it is reused as the
  search backend, not replaced.
- Deleting docs automatically. Deletion is explicit-only via `/docs cleanup`.
- Migrating `CLAUDE.md` / `AGENTS.md` / memory scopes into the bundle.

## OKF bundle

- Root: `docs/` in the project working directory.
- Reserved files: `docs/index.md` (progressive-disclosure index, no
  frontmatter, `okf_version: "0.1"` declared in root index frontmatter per
  spec §11 — note: OKF allows frontmatter on the *root* index for the version
  declaration only), `docs/log.md` (chronological change history, newest
  first, `## YYYY-MM-DD` headings, `**Update**`/`**Creation**`/
  `**Deprecation**` prefixes).
- Concept docs: any other `.md` under `docs/` with YAML frontmatter. `type`
  required; `title`, `description`, `resource`, `tags`, `timestamp`
  recommended. Producer extensions allowed; **unknown frontmatter keys must be
  preserved on rewrite** (round-trip rule, spec §4).
- Deprecation: extension field `status: deprecated` + `deprecated_reason` in
  frontmatter; doc stays on disk until `/docs cleanup`.
- Cross-links: bundle-relative (`/playbooks/foo.md`) preferred. Broken links
  tolerated (they mark not-yet-written knowledge).
- Non-conforming files (no frontmatter, e.g. pre-existing docs the init pass
  could not classify) are tolerated, never rejected, and listed in the index
  under an "Unclassified" section.

## Command surface

Extend the existing `/docs` command (`internal/tui/commands.go:144`,
handler `model.handleDocsCmd` at `internal/tui/model.go:6980`):

| Subcommand | Behavior |
|---|---|
| `on` / `off` / `status` | As today (doc-prompt toggle), plus `on` now enables the knowledge system: index injection, `knowledge_lookup` tool registration, post-job maintenance worker. `status` additionally reports bundle presence, doc count, last log entry. |
| `init` | Bootstrap existing `docs/` into an OKF bundle. Non-destructive: (1) add frontmatter to files the context agent can classify, preserving all content; (2) generate `index.md` + `log.md`; (3) emit a staleness report — docs contradicting current code, duplicates, orphans — marking candidates `status: deprecated`. Never deletes. Idempotent: re-running re-indexes and re-audits without clobbering existing frontmatter. |
| `update [focus]` | Force a maintenance pass now (analog of `/mem update`), optionally focused on a topic. |
| `cleanup` | List deprecated docs, ask per-file confirmation, delete confirmed ones, log deletions in `log.md`. The only deletion path. |

Config: reuse `Config.Ocode.DocPromptEnabled` as the master switch (no new
flag). The knowledge system is active when the flag is on **and** `docs/`
exists as an OKF bundle (root `index.md` present); otherwise `/docs on`
behaves exactly as today and `status` hints at `/docs init`.

## Context agent

New entry in `DefaultSubAgents` (`internal/agent/subagent.go:71`), name
`context`, mode subagent.

- **Purpose (prompt):** knowledge curator and retriever for the OKF bundle.
  Answers "why / what did we decide / is there a playbook" questions from
  docs; verifies doc claims against code before answering or writing; writes
  only through the doc tools.
- **Tools allowlist** (`AgentSpec.Tools`): read-only code tools (grep, glob,
  read, ls) + the four doc tools below. No bash, no edit/write of arbitrary
  files.
- **Model:** add `context` to `smallModelEligibleNames`
  (`internal/agent/small_model.go:45`) — runs on the configured small model
  when `SmallModelEnabled`, falls back to the main client automatically.

### Context vs explore

| | `explore` | `context` |
|---|---|---|
| Ground truth | source code as-is | curated knowledge in `docs/` |
| Question shape | where is X, how does Y flow | why X, decisions, playbooks, schemas |
| Cost | many greps/reads | index + frontmatter first, small model |
| Writes | never | sole writer of the bundle |

Main-agent guidance (in the `/docs`-enabled prompt fragment): try
`knowledge_lookup` first for why/decision/playbook questions; use `explore`
for code-level questions; for mixed questions dispatch both in background
concurrently, take the first sufficient answer, `task_cancel` the other.

## Tools

### Context-agent-only (scoped via `AgentSpec.Tools`)

| Tool | Signature | Behavior |
|---|---|---|
| `doc_search` | `(query, tags?, type?)` | Frontmatter + content search over the bundle, backed by the discovery engine index (`internal/discovery/engine.go`). Results sorted by relevance, paginated. |
| `doc_get` | `(path)` | Return one concept doc (frontmatter + body). Bundle-relative path. |
| `doc_write` | `(path, frontmatter, body)` | Create or update a concept doc. Enforces `type` present; merges frontmatter preserving unknown keys; sets `timestamp`; appends a `log.md` entry; regenerates `index.md`. |
| `doc_deprecate` | `(path, reason)` | Sets `status: deprecated` + `deprecated_reason`, log entry, index regen. |

`doc_write`/`doc_deprecate` refuse paths outside the bundle root and refuse
to touch reserved files directly (index/log are maintained by the tool
implementation, not by the model).

### Main-agent tools

| Tool | Signature | Behavior |
|---|---|---|
| `knowledge_lookup` | `(question)` | Dispatches the `context` agent via the existing `TaskTool` path; returns a distilled answer + source doc paths. Registered only while the knowledge system is active. |
| `task_cancel` | `(task_id)` | Cancels a background run via the existing per-run `Cancel func()` (`internal/agent/agent_runs.go:36`) and marks it via `tryFinishCancelled` so the TUI updates immediately. Cancellation is cooperative (stops at the next step boundary). Always registered alongside `task_status`. |

Cancellation is **caller-decided, never automatic**: the runtime never kills a
racing agent on its own. The agent that dispatched a background task (main
agent, or any subagent holding the `task` tool) judges when a returned answer
is sufficient and explicitly calls `task_cancel` on the redundant run.
Ownership guard: an agent may only cancel runs it dispatched itself (the run
registry records the dispatcher), so a subagent cannot cancel the main
agent's tasks or a sibling's.

The main agent has no direct write path to the bundle.

## Read path

When the knowledge system is active, inject `docs/index.md` content into the
system prompt under a `[ocode:knowledge]` marker, alongside the existing
memory injection point (`internal/agent/prompt.go:107`). Index only — never
doc bodies. Deep content is fetched on demand via `knowledge_lookup`.

## Write-back (doc maintenance)

Mirror memory maintenance (`internal/agent/memory_maintenance.go`) as the
template:

1. On job completion, `queueDocMaintenance(ev)` — non-blocking push onto a
   buffered channel (cap 64), same call site pattern as
   `queueMemoryMaintenance` (`internal/tui/model.go:3092`).
2. Single worker goroutine serializes all bundle writes (no concurrent-write
   races with racing lookups: lookups are read-only).
3. **Pass 1 — triage (small model, JSON-only):** input = last ~8 conversation
   messages + current `index.md`. Output = `noop` | list of
   `{action: create|update|deprecate, path, reason}`. Gate is strict: only
   durable knowledge (decisions made, gotchas discovered, playbooks executed,
   schema/architecture changes) triggers actions; Q&A and routine edits are
   noops.
4. **Pass 2 — execute:** if not noop, dispatch the `context` agent (small
   model) with the triage plan; it uses `doc_search`/`doc_get` to check
   existing docs, then `doc_write`/`doc_deprecate`. Every change lands in
   `log.md`.
5. `/docs update [focus]` enqueues the same flow with a forced-run flag on the
   queue item and optional focus (not a triage bypass — triage still plans,
   but the gate is relaxed to "review the whole bundle for staleness").

Failure handling: pass failures are logged (structured logger) and dropped —
maintenance is best-effort and must never block or fail the user's session.

## Error handling

- Missing/unparseable frontmatter on read: tolerate, treat as unclassified
  (OKF permissive-consumer rule); log at debug.
- `doc_write` with missing `type` or out-of-bundle path: tool error returned
  to the model (fail loud, no silent fixup).
- Index regeneration failure: log error, leave previous `index.md` intact.
- Small model unavailable: existing fallback to main client (no new code).

## Testing

- Unit: frontmatter round-trip (unknown-key preservation), index generation
  from a fixture bundle, log append format, deprecation flow, path guards on
  `doc_write`, `task_cancel` on a running/finished/unknown task id.
- Integration: `/docs init` on a fixture `docs/` with mixed
  conforming/non-conforming files → verify non-destructive annotation +
  index + report; maintenance triage gating (noop on chat-only transcript,
  action on decision-bearing transcript) with a stubbed model client.

## File touch map (planning input)

- `internal/agent/subagent.go` — `context` in `DefaultSubAgents`
- `internal/agent/small_model.go` — eligibility list
- `internal/agent/prompt.go` — `[ocode:knowledge]` injection
- `internal/agent/agent_runs.go` + new tool file — `task_cancel`
- new `internal/knowledge/` (or `internal/okf/`) package — bundle parsing,
  frontmatter round-trip, index/log generation, doc tools
- new `internal/agent/doc_maintenance.go` — queue + worker + triage
- `internal/tui/commands.go`, `internal/tui/model.go` — `/docs` subcommands
- `internal/discovery/` — reuse as `doc_search` backend

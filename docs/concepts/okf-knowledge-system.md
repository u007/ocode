---
type: Concept
title: 'Knowledge System (OKF Bundle): Agents, Tools and Maintenance'
description: Activation gates, the context and explore agents, knowledge tools, /docs subcommands, maintenance worker and the code-line-anchor drift rule.
resource: CLAUDE.md
tags:
  - okf
  - knowledge
  - context-agent
  - docs
timestamp: 2026-09-30T07:37:10Z
---
# Knowledge System (OKF Bundle): Agents, Tools and Maintenance

The project supports an optional **OKF v0.1 knowledge bundle** at `docs/` — a
curated set of markdown files with YAML frontmatter (type, title, description,
tags, timestamp, status). When active, the agent gains the `knowledge_lookup`
tool (and the `context` sub-agent) for retrieval; nothing from the bundle —
not even `docs/index.md` — is injected into the system prompt (see "OKF docs
access scope" below).

**Activation** (two gates, both required):
1. `DocPromptEnabled` flag — toggled via `/docs on` (persisted in config).
2. Bundle marker — `docs/index.md` must have `okf_version: "0.1"`
   frontmatter, created only by `/docs init`.

### Agents

| Agent | Role | Tools |
|-------|------|-------|
| `context` | Knowledge curator/retriever and **sole automated writer** to the bundle. Answers why/decision/playbook questions; verifies doc claims against code before writing; prefers updating existing docs over near-duplicates; deprecates rather than deletes. Uses the configured context model if set and enabled, else the small model, else the main model (see `/context-model`). When the bundle is active, also handles codebase exploration (where/how/what) — subsumes `explore` — with full explore toolkit plus doc tools; `explore` is hidden from the schema. Priority: `doc_search` first, then `doc_get`, then code tools; parallel `doc_search` + code search in the same batch is allowed for mixed questions. | `grep`, `rgrep`, `glob`, `read`, `list`, `lsp`, `bash`, `webfetch`, `websearch`, `doc_search`, `doc_get`, `doc_write`, `doc_deprecate` |
| `explore` | Code-level exploration — where/how/what in source; general codebase research, not pinned to the knowledge bundle (hidden when `context` is active) | `read`, `glob`, `grep`, `rgrep`, `list`, `lsp`, `bash`, `webfetch`, `websearch` |

Guidance in the `DocPromptEnabled` prompt fragment: try `knowledge_lookup` first
for why/decision/playbook questions; when the bundle is active use `context` (which subsumes `explore`) for code-level or mixed (why+where) questions — `explore` is hidden — with knowledge-first priority: `doc_search` first, `doc_get` as needed, then code tools, parallel batch allowed; when the bundle is inactive, use `explore` for code-level questions as before.

**Sole-automated-writer invariant:** no agent path outside the `context`
subagent may write to the bundle — the main agent's tool set never includes
`doc_write`/`doc_deprecate`. Deletion happens only via `/docs cleanup`
(per-file confirmation required).

**OKF docs access scope:** OKF documentation (the `docs/` bundle, including
`index.md`) is NOT loaded into the main agent's system context. The main
agent only accesses OKF docs through the `context` sub-agent (via
`knowledge_lookup` or `task` with `agent=context`). The `context` sub-agent
discovers and reads docs dynamically through `doc_search`/`doc_get` and
manages the bundle through `doc_write`/`doc_deprecate`. When the bundle is
active, ALL non-exploration work (why/decision/playbook questions, doc
updates, mixed why+where questions) must route through the `context`
sub-agent; pure code exploration (no doc involvement) may use the explore
toolkit directly.

### Tools

| Tool | Availability | Description |
|------|-------------|-------------|
| `knowledge_lookup` | Always registered | Dispatches the `context` sub-agent to answer knowledge questions. Soft-fails (hints `/docs init`) when inactive. |
| `task_cancel` | Always registered | Cancels a background task **you** dispatched (including async context agents), by run ID. Cooperative — stops at the next step boundary. Ownership enforced by dispatcher identity. |
| `doc_search`, `doc_get`, `doc_write`, `doc_deprecate` | Context sub-agent only | Full CRUD on the knowledge bundle. Write/deprecate auto-update `log.md` and regenerate `index.md` under cross-instance file lock (`knowledge.WithBundleLock`, `docs/.okf.lock`). |

### `/docs` subcommands

| Subcommand | Behavior |
|------------|----------|
| `on` / `off` | Toggle doc-first development prompt (master switch for knowledge system) |
| `status` | Bundle presence, doc counts (conforming/non-conforming/deprecated), last log entry, active state |
| `init` | Bootstrap `docs/` into an OKF bundle — add frontmatter, generate index + log, emit staleness report; dispatches the `context` subagent to scan & annotate existing docs. Non-destructive, idempotent (re-run re-audits without clobbering). |
| `update [focus]` | Force a maintenance pass (scan for staleness, duplicates, orphans). Queued asynchronously. |
| `cleanup [--yes]` | List deprecated docs with path and reason; `--yes` deletes them under lock, logs deletions, regenerates index. |

### Maintenance
A post-job doc maintenance worker mirrors memory maintenance:
1. **Triage (small model):** Decides if the last turn produced durable knowledge (decisions, gotchas, playbooks, schema changes). Q&A and routine edits are noops.
2. **Execute:** Dispatches the `context` sub-agent to apply create/update/deprecate actions.
3. All mutations go through `knowledge.WithBundleLock` (`docs/.okf.lock`, flock-style).
4. Worker drains on `Agent.Shutdown()` — drops queued items, finishes the current one.

### Code line anchors in bundle pages drift silently
Bundle pages cite code as `file.go:123`. **Adding or removing any line ABOVE
such a citation invalidates it**, and nothing detects this — the page still
reads as authoritative while pointing at the wrong statement.

- When a change adds/removes lines in a file the bundle cites, **re-derive
  EVERY anchor on that page**, not just the ones in the section you are editing.
  A context-agent amendment asked to fix section N will typically re-derive only
  section N; on 2026-09-27 the same `inactivityContextWithParent` anchor needed
  fixing twice in one session (1008 → 1045 → 1058) as helpers were added.
- Verify mechanically rather than by reading: extract every `\w+\.go:\d+` from
  the page and print the actual source line at each one, then eyeball the whole
  list. Do not trust a sub-agent's "anchors corrected" list — that is how
  `compact.go:854-856` and `compact.go:872-874` shipped pointing at the success
  path and the malformed fallback instead of the two intended sites.
- Ranges (`820-830`) are as fragile as single lines, and easier to get wrong,
  because the start line can look plausible while the range covers the wrong
  block. Check both endpoints.
- If the context sub-agent wanders (it has been observed drifting into reading
  `doc_tools.go`'s own source for 40+ minutes without writing), cancel it and do
  the mechanical correction directly with a verified replace.

### Relationships
- **`docs` primary agent** (ModeDocs) has the `task` tool so it can dispatch `context` for knowledge lookups, but has no direct doc tools.
- **`/doc-sync`** (rules/skills sync) is unrelated — its scope is the root briefing file (`AGENTS.md`, or `CLAUDE.md` when it is the only one), rules, skills, never `docs/`.
- **Memory scopes** (`/mem`) are orthogonal — knowledge bundle is project docs, memory is agent state.

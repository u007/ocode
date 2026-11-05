# 01 — What ocode has today (checked 2026-11-05)

Every mechanism that persists or reuses knowledge across turns or sessions, graded by whether it could
take part in a learning loop. `$GD` = `paths.GlobalDataDir()` (`~/.local/share/opencode` on macOS).

## Mechanisms

| mechanism | on disk | writer | how it reaches the model | scope | loop-relevance |
|---|---|---|---|---|---|
| Layered memory (`internal/memory`) | `$GD/memory/user.md`, `$GD/memory/global.md`, `$GD/project/<slug>/memory.md` | user via `/mem update` (main model); agent via plain file tools + `ocode-mem` skill; `memory_maintenance.go` planner (small model, JSON `noop/update/compress`, full-body replacement, 32 KB cap) | `LoadContext` → `memory.PromptFragment`, **system role, cached prefix**, resolved once per agent build | user / project / global | the only automatic cross-session writer. Its prompt already calls `global.md` "global history for reusable lessons" |
| Compaction summary (`compact.go`) | session transcript, `[ocode:compaction-summary]` system message | compaction LLM, fixed template | stays in that session | session | template already extracts **User Directives (verbatim)** and **Errors & Fixes (including user corrections)**; never promoted anywhere |
| Recap | none (transient TUI message / API response) | small model | human only | — | the only per-turn "what happened" summary; three mechanisms by design (TODO.md) |
| Notes bus `[ocode:notes]` (`internal/notebus`) | in-memory; sidecar `*.notes.jsonl` exists but `SetNoteBusDir` has no production caller | parallel sub-agents (`<oc-note>`, `<oc-resolve>`) | user-role tail delta | one task group | intra-session only; sidecar persistence inert (TODO.md) |
| Skills (`internal/skill`) | `~/.config/opencode/skills`, `~/.agents/skills`, `~/.claude/skills`, project `.opencode/skills` / `.claude/skills` / `skills`, bundled; first-wins by name | humans, `skill-creator`, installer | catalog in system prompt (discovery off) or user-role discovery tail; bodies via `skill`/`load_skill` | global / project | `/learn` (`commandctx/learn.go`) only builds a prompt and writes nothing; one `/learn` pass once overwrote a skill with a weaker draft (TODO.md) |
| Kaizen (`skills/kaizen/*`, `docs/okf/`) | 95 per-model tuning skills; eval corpus per stack (`questions.yaml`, `_prompts/`, `answers/`, `scores/`, `derived/`) | offline: closed-book answerer → separate grader → scorecard → derived skill for tags below threshold → with-skill re-run → `sync-derived-skills.py` | `KaizenDigestBlock` force-injects the `<!-- kaizen:digest -->` into the system prompt when model + stack match | bundled, per model, ships by rebuild | the closest existing learning loop, but offline, manual, per model, and it never learns at runtime |
| `/kaizen-review` skill | none | read-only audit of the on-disk session log (tool errors, context waste, accuracy, tooling gaps) → emits a paste-ready prompt | — | — | the closest existing **reflector**; a human runs everything after it |
| OKF bundle (`internal/knowledge`, `docs/`) | repo `docs/` with `index.md`, append-only `log.md`, `.okf.lock` | `context` sub-agent only (`doc_write`, `doc_deprecate`); `doc_maintenance.go` triage → context dispatch | never injected; only via `knowledge_lookup` tool results | project (committed) | the **sole-writer** pattern and bundle lock are the template for any new automated writer |
| md discovery | `$GD/project/<slug>/md-summaries.json` | background small model | `[ocode:discovery]` user-role tail, relevance-judged | project | the retrieval pattern (index → judge → attach) to reuse for lessons |
| Pulse memory | `$GD/pulse/memory/global.md`, `.../projects/<slug>.md` | Pulse assistant `memory_write` (full replace, 90 KB cap) | `[ocode:pulse-memory]` user-role per-turn tail | Pulse only | separate store; proves the user-role-tail injection pattern |
| Hooks | config shell hooks per tool (pre/post); plugin `SessionStart` (Claude-Code format only, process-cached) | user | system (SessionStart) | — | **no Stop / SessionEnd / UserPromptSubmit / PreCompact events** exist |
| Scheduler | `$GD/scheduler/<slug>/jobs.json`, `runs.jsonl`, `deliveries.jsonl` | user / `cron` tool / web | fresh full agent per firing, session `cron:<id>` | project | runs a whole agent on a timer: the natural host for periodic consolidation and for same-task probes |
| Usage ledger | `$GD/usage/records.jsonl` | every LLM call | none | global | tokens and spend only. No tool-error, retry, outcome or latency columns |
| Session store | `$GD/project/<slug>/sessions/<id>.sqlite` + `index.sqlite` | agent loop | transcript | project | metadata has model, spend, permission mode, goal; **no outcome, correction or lesson fields** |
| Jev decision slots (`resolveDecider`) | — | — | — | — | ten slots, each with its own floor; the gate for a learning judge slots in here |
| Goal loop / auto-continue | session metadata | judges | user-role resume messages | session | verdicts (`met` / `impossible` / `not_met`, action keys) are outcome signals nobody records |
| Changes tab / snapshot store | `$GD/project/<slug>/snapshots/`, `snapshots.sqlite` | every mutating tool | none | project | `undo_file_change` calls are a strong "the edit was wrong" signal nobody records |

## What is missing for a loop

1. **No signal capture.** No thumbs, no `/feedback`, no correction tagging, no tool-error or retry counts in any
   ledger, no outcome on a session. Everything a reflector would need is reconstructed after the fact by
   scanning the raw transcript (what `/kaizen-review` does by hand).
2. **Corrections die with the session.** Compaction already extracts them into a fixed template section, then
   drops them with the session.
3. **One automatic writer, one host.** The memory planner runs only on TUI `jobCompletedMsg` (process exit or
   sub-agent run), never on the web/desktop server and never after a scheduled-job run. Its output is a full-body
   replacement with no provenance, so a "lesson" written there cannot be traced to evidence or later retracted.
4. **No end-of-turn or end-of-session hook** to attach reflection to, on either host.
5. **Nothing is ever validated.** No mechanism checks whether memory or a derived skill helped; Kaizen validates
   offline against a question corpus, not against real sessions.
6. **No agent-side lesson writer.** `/learn` proposes; writes go through plain file tools with no lock, no
   read-before-write contract, no dedupe.

## Bugs a loop would inherit (FIXED 2026-11-05, uncommitted at time of writing)

| bug | where | why it matters here | fix |
|---|---|---|---|
| Memory resolved from process cwd, not the session root | `memory.PromptFragment("")` / `ResolvePaths("")` fell back to `os.Getwd()`; `LoadContext` had `root` but never passed it | a desktop app launched from Finder has cwd `/`; a server hosting several projects injected the wrong project's memory. Contradicted the CLAUDE.md "Context Loading" rule | `LoadContext` passes `root`; `""` keeps the legacy cwd fallback. Test `TestLoadContextInjectsMemoryOfSessionRootNotProcessCwd` |
| Memory maintenance never ran on the server | `QueueMemoryMaintenance` was called only from `internal/tui/model.go` | web/desktop sessions never fed memory; a loop built on the same trigger would be TUI-only too | `handleServerJobEvent` queues it for durable events only (process exits and background agent runs; synchronous agent runs return earlier), with the last 8 user/assistant messages read under `as.mu.TryLock`. When the lock is busy mid-turn the event is deferred: the latest one is kept as a marker on the `agentSession` (guarded by its own `memoryMaintMu`), and the turn end (`runTurn` and the ask continuation, after unlock) queues one pass from the live transcript. There is no disk fallback. Pulse sessions excluded. Tests `TestHandleServerJobEventQueuesMemoryMaintenance`, `...SkipsMemoryMaintenanceForSyncAgentRun`, `...SkipsMemoryMaintenanceForPulseSession`, `TestJobMemoryMaintenanceDefersWhileTurnHoldsLock`, `TestTakeDeferredMemoryMaintenanceReadsLiveTranscript`, `TestFlushDeferredMemoryMaintenanceWithoutPendingQueuesNothing`. Scheduled-job runs still do not queue it |
| `memory_maintenance.go` "no small-model client" skip log was unreachable | the worker falls back to the main client | the log line misled; a reflector that reuses this worker must pick its model explicitly | message now `skipped: no LLM client available` |

## Stale docs found on the way (not fixed by this plan)

- `docs/concepts/prompt-cache-stability.md` says md-summaries live in `.ocode/`; code uses `$GD/project/<slug>/`.
- `docs/concepts/data-storage-layout.md` says sessions are JSON files; they are sqlite.
- `docs/file-edit-snapshot.md` says the undo window is 2; it is 10 and configurable.
- `docs/concepts/command-and-skills-catalog.md` lists `OnMessage` as a hook event; it is an agent callback.
- No concept doc exists for `internal/memory`, the memory planner, the notes bus, `/learn`, or the Kaizen
  runtime digest path.

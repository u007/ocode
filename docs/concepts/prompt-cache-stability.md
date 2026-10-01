---
type: Concept
title: Prompt Cache Stability
description: 'Anthropic prompt-cache prefix order and the rules for any change that touches tools or the base prompt: deterministic tool order, sticky sets, role-determines-caching, tail injection by volatility, breakpoints.'
resource: CLAUDE.md
tags:
  - prompt-cache
  - anthropic
  - tools
  - discovery
timestamp: 2026-10-01T04:50:48Z
---
# Prompt Cache Stability

Anthropic prompt caching reads one linear prefix in a **fixed order: `tools` →
`system` → `messages`** (breakpoints set in `internal/agent/client.go` — system
~:3833, last tool ~:3875, and the last two user-role messages via
`applyAnthropicConversationBreakpoints` ~:4195). Because tools come
first, **any change to the tools array invalidates the `system` block and the
message prefix too** — they sit downstream of tools in the prefix. This is the
dominant cost when adding features that vary what gets sent.

The agent's base system prompt now also includes a static `[ocode:recap]`
contract fragment (`recapPromptContent` in `internal/agent/prompt.go`), a const
so it is cache-safe and re-cached the prefix once when it shipped.

Rules for any change that touches tools or the base prompt:
- **Never put per-turn-varying content in `tools` or `system`.** `LoadContext`
  (system) must be a function of stable, preload-time state (config flags), not
  of a per-turn computed result.
- **`GetToolDefinitions` must emit a deterministic order** (`sort.Strings` over
  names). `a.tools` is a map — unsorted iteration randomizes the tools array
  every turn and busts the cache on every request.
- **Tool sets that grow must be grow-only/sticky within a session** (see the
  discovery `Session`). A no-new-attachment turn then sends a byte-identical
  tools array → full cache hit; only growth turns pay a re-cache.
- **Discovery candidates pass through the TypeSafe relevance judge before
  attaching.** `Session.Select` ranks and returns not-yet-attached candidates
  *without* mutating the sticky set; `RunDiscovery` then judges them and seeds
  only those that meet the lenient shared floor. The judge may only veto: not
  connected, a transport/decode error, or a missing answer all fail open (seed
  everything). It never changes `renderDiscoveryContext` — the names-index stays
  a function of the doc set. See `docs/concepts/discovery-typesafe-judge.md`.
- **Discovery must never fail open the MCP tool gate.** `discoveryAllows`
  (`internal/agent/discovery_glue.go`) gates MCP tools to the sticky attached
  set and consults NO warm/corpus state — a cold embedder cache is the normal
  first turn, so a cold turn legitimately starts with **zero** MCP tool
  definitions and `discover_more` is the recovery path. The only fail-open is
  `disco == nil || !disco.enabled`. A failed `Session.Select` attaches nothing
  for that turn and never sets `disco.enabled = false`. **Every** attach path is
  judged (per-turn `runDiscovery` and on-demand `discover_more` both run
  `judgeDiscoveryCandidates` before `Seed`); a second unjudged attach path is a
  bypass. See `docs/concepts/discovery-mcp-tool-gating.md`.
- **doc_search results pass through the same TypeSafe relevance judge before
  the context subagent sees them** (`newDocToolsWithJudge` with
  `Agent.docSearchJudge()`, nil when TypeSafe is not connected; filtering runs
  before `get_top` body inlining; fail-open like discovery). Shared mechanics
  live in `judgeRelevanceQuestions` (`internal/agent/relevance_typesafe.go`);
  see `docs/concepts/doc-search-relevance-judge.md`.
- **Role determines caching, not array position — because of the hoist.** The
  Anthropic builder (`chatAnthropic` → `collectAndRemoveSystemMessages` in
  `client.go`) pulls **every `system`-role message — including tail ones — into
  the top-level `system` field**, which carries `cache_control`. So a
  `system`-role message appended at the tail is NOT in the uncached suffix; it
  rides the **cached** system block. Consequence: any tail `system` injection
  whose content **varies per turn** (e.g. growing) rewrites and busts the whole
  cached system prompt. Every volatile tail injector is therefore user-role:
  `injectNotesTail`, `injectTodoTail`, `injectDirMDTail`, `injectLSPDelta`.
- **LSP diagnostics are message-level, never system-role** (`internal/agent/
  lsp_inject.go`). A single-file write tool (`write`, `edit`, `multiedit`,
  `replace_lines`, `format`) re-syncs its file with the language server and
  waits up to 2s for a fresh publish; non-empty diagnostics are appended to
  that tool's result (transcript history, byte-stable once persisted). Files
  whose diagnostics changed elsewhere (package-level fallout, user edits
  between turns) are rendered once as a `[ocode:lsp]` user-role tail block by
  `injectLSPDelta` before each model call; the agent keeps a per-URI
  fingerprint of what it last reported, so an unchanged store adds nothing.
  LSP server problems (binary missing) stay out of the prompt entirely: they
  ride `Message.Notice` via `NoticedError` and show only in the transcript UI.
- **Per-turn UI state and transcript notices are user-role too.** The TUI file
  selection (`[ocode:selection]`) is appended by `PrepareMessages` at the
  tail as user-role, not by `BasePromptMessages` (which is system-only and
  takes no per-turn arguments). Background agent/process completion notices
  (`[ocode:event]`) and "add file to context" blocks (`[ocode:context]`) are
  persisted as user-role transcript messages with an explicit marker line so
  the model still reads them as out-of-band. Never persist a `system`-role
  transcript message for anything that can happen more than once per session.
- **Subdirectory `CLAUDE.md`/`AGENTS.md`/`OCODE.md` are lazy and volatile**
  (`internal/agent/dir_docs.go`). After a path-touching tool succeeds
  (`read`, `write`, `edit`, `multiedit`, `replace_lines`, `format`, `lsp`,
  `ast`, `glob`, `list`, `grep`, and every `edits[].path` of
  `multi_file_edit`; `apply_patch` is not covered), the agent walks from the
  touched path up to the project root, exclusive, and queues each unseen
  directory's docs for one user-role `[ocode:discovery]` tail block before the
  next model call. A directory is seen once per session; compaction
  (`runCompact`) clears the seen set because the injected blocks were never
  persisted and are gone with the splice.
- **Anthropic breakpoints (`applyAnthropicConversationBreakpoints` in
  `client.go`):** tools (last tool), system (single block), and the last two
  user-role messages (tool results are user-role). The marker on the most
  recently appended turn is what lets the next request read the whole prior
  transcript from cache; the old placement on the FIRST user message cached
  nothing after turn one. Do not add a fifth marker — Anthropic allows four.
- **Split tail injection by volatility (`injectDiscoveryContext` is the model):**
  - *Stable* content (e.g. the discovery name index + prompt contract — names
    don't change turn to turn) → **`system`-role** → hoisted into the cached
    system block, so it caches.
  - *Volatile* content (e.g. attached-skill full descriptions and attached
    project-doc full file content, which grow with the sticky set) →
    **`user`-role** → `collectAndRemove` leaves it in the messages array
    (uncached suffix), where it coalesces with the current user turn and never
    busts the system cache. Wrap it in the `[ocode:discovery]` marker so the
    model reads it as system-origin, not user speech.
- **Markdown docs are part of the discovery corpus (`md_discovery.go`).** Every
  project `*.md` except the always-on briefing set (`AGENTS.md`, `CLAUDE.md`,
  `OCODE.md`, `.cursorrules`, `.opencode/rules/*.md`, which `LoadContext` injects
  in full) **and files inside an active OKF knowledge bundle's `docs/` directory**
  (owned by the knowledge system — nothing from it is injected into the prompt;
  `knowledge_lookup` / the `context` sub-agent retrieve any concept doc on
  demand) is a `Kind:"md"` Doc whose `Text` is an LLM summary (small model when
  configured, else the main client), cached at `.ocode/md-summaries.json` keyed
  by file content (mtime+size gate, then sha256). The first activation runs a
  **blocking** pass (`mdSummarizePass`, bounded concurrency `mdSummaryWorkers`)
  so the corpus is fully summarized before the turn proceeds; failed
  summarizations are negative-cached (`mdFailBackoff`) and never become
  placeholders. The names-index lists `path — summary`; the full file content is
  attached to the volatile tail only on query match. Editing a doc invalidates
  its summary on the next throttled scan (`mdScanThrottle`), so `/doc-sync` edits
  are reflected automatically.
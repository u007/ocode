# Discovery-based skill/MCP retrieval — design

**Date:** 2026-06-20
**Status:** Approved design, pending implementation plan
**Concept:** Use a small retriever (LFM2-5-style) to embed the current task and
attach only the relevant skills + MCP tools, instead of injecting the full skill
catalog and every MCP tool schema into every request.

## Problem

Today ocode injects, on every request:

- the **full skill catalog** (`skill.BuildCatalog()` → `internal/agent/context.go`), and
- **every MCP tool schema** (`Agent.GetToolDefinitions()` → `internal/agent/agent.go`);
  ocode has no deferred/ToolSearch mechanism, so all MCP tools are eagerly registered.

MCP tool schemas dominate context cost. As more MCP servers are connected, the
fixed per-request context grows regardless of what the task needs.

## Goal

An **opt-in** mode (`/discovery on`, default **off**) that, before each turn and
each sub-agent spawn, ranks skills + MCP **tools** by semantic similarity to the
task and attaches only the relevant ones. Core built-in tools are never gated.
When off, behavior is exactly as today.

## Non-goals (YAGNI)

- Exposing threshold/floor/cap as user-facing settings — they stay internal constants.
- A vector database — the corpus is dozens–hundreds of short texts; in-memory cosine is enough.
- Reranking beyond cosine similarity.

---

## Architecture

New isolated package `internal/discovery/`. The TUI/agent call into it; it owns
the embedder, the corpus index, the cache, and the selection policy.

```
internal/discovery/
  embedder.go   Embedder interface + httpEmbedder + localEmbedder
  index.go      corpus build, disk cache, cosine search, selection policy
  session.go    per-session sticky attached-set state
  download.go   local model runtime download + status
```

### 1. Embedder abstraction

```go
type EmbedKind int // Passage | Query  (asymmetric: doc vs query prefix)

type Embedder interface {
    Embed(ctx, texts []string, kind EmbedKind) ([][]float32, error)
    ID() string   // e.g. "openai/text-embedding-3-small" or "local/lfm2-5-retriever"
    Dim() int
}
```

- **httpEmbedder** — default path. Backends: OpenAI, Voyage, Cohere, Gemini
  embeddings. Reuses existing provider auth (`internal/auth`). Batches the corpus
  in one call; the per-turn query is a single-text call.
- **localEmbedder** — optional. Loads the LFM2-5 retriever locally. The model is
  **downloaded at runtime** to `~/.cache/ocode/models/` only when the user enables
  the local backend (never bundled, never auto-downloaded for HTTP users). Runtime
  binding (ONNX/llama.cpp) is an implementation-plan decision; behind the same
  interface so it is swappable.
  - **Apple Silicon (darwin/arm64, M1+) → MLX build.** When the local backend is
    enabled on Apple Silicon, the localEmbedder must select the **MLX** artifact of
    the LFM2-5 retriever (Apple's Metal-accelerated runtime) rather than the generic
    ONNX/CPU build. Detection: `runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"`.
    The download manifest therefore carries per-platform artifacts (mlx vs onnx/cpu),
    and `local_model_status` tracks whichever artifact matches the host. This is a
    **Plan 2** concern; Plan 1 leaves the seam (backend = "local" returns a clear
    "not available yet" error).

Asymmetric encoding: corpus embedded as `Passage`, query embedded as `Query`
(instruction prefix for LFM2-5; input-type hint for HTTP backends).

**Embedder availability (no silent default).** Anthropic has **no embeddings API**,
and the primary ocode user runs on an Anthropic subscription with no embedding key.
So HTTP is *not* a safe default for that user — it requires a second vendor key
(OpenAI/Voyage/Cohere/Gemini), and **the local backend is their only no-extra-key
path**. Therefore:

- There is **no implicit OpenAI default**. `discovery.embedding_model` starts unset.
- `/discovery on` **validates** that a usable embedder exists — either a configured
  HTTP embedding key for the selected model, or a downloaded/ready local model. If
  neither, it **hard-errors** with guidance ("run `/discover model` to pick an HTTP
  model you have a key for, or select local to download it") and discovery stays off.
- The HTTP embedding-model list is **curated per provider** inside the discovery
  package (each entry carries its `dim`); it is **not** sourced from the models.dev
  chat registry, which lists chat models, not embeddings.

**Scope of savings — context only.** ocode still connects every MCP server and
calls `ListTools()` at startup (`LoadExternalTools`), because the corpus needs each
tool's description. Discovery shrinks **context injection**, not MCP process or
connection cost — startup is unchanged. Stated so users don't expect lighter startup.

### 2. Corpus + search (`index.go`)

**Corpus granularity: per-item.** One vector per **skill**
(`name + description + when_to_use`) and one vector per **MCP tool**
(`name + description`) — *not* per MCP server. This lets discovery attach an
individual tool (`Notion/query-meeting-notes`) without pulling its whole server.

**Caching:**
- Vectors cached at `~/.config/opencode/discovery/corpus-<model_id>.json`.
- Cache key per item: `hash(text) + model_id + dim`.
- A skill/tool description change → re-embed **that item only** (hash mismatch).
- New/removed skill or MCP tool → embed the new one; drop the stale entry.
- **Model switch** (`/discover model`) → dimension changes → **whole cache invalidated**, corpus re-embedded once.

**Warming: background.** At session start, corpus embedding kicks off in a
goroutine. The first turn that runs discovery waits only if warming hasn't
finished; otherwise it is instant. Cancellable; if the model changes before it
finishes, the in-flight warm is discarded and restarted for the new model.

**Selection policy: rank-relative (internal constants).** Absolute cosine
thresholds are **not** portable across embedders — score scales differ (OpenAI vs
Voyage vs LFM2-5, normalized vs not), so a fixed `0.30` is meaningless the moment
the model changes. Instead, sort descending and keep every item within **δ = 0.15**
of the **top score**, bounded by **floor = 5** and **cap = 30**. Deltas-from-top
are far more portable than absolute scores; the cap is sized so a single large MCP
server (Notion ≈ 17 tools, Gmail ≈ 12) can be fully attached for a server-focused
task without starving. These constants are re-derived for **per-tool** granularity
(the earlier 0.30/3/12 were calibrated for per-server) and are tuning knobs for the
implementation plan, not user settings. Empty corpus (no skills/MCP) → discovery is
a no-op, no embedder call.

**Query construction.** The query is not the raw latest message — short follow-ups
("yes", "continue", "fix that") embed to noise and the floor then attaches
irrelevant tools. The per-turn query = the current user message **plus a small
rolling window** of recent user turns, capped to ~512 tokens. Sub-agent query = its
task prompt (already self-contained). This keeps ranking stable across terse
follow-ups.

### 3. Attachment — sticky/monotonic, fail-open

- Re-rank **per user turn** (query = the user message) and **per sub-agent spawn**
  (query = the agent's task prompt).
- Each **agent instance** holds its own **grow-only** attached set: the union of
  everything ranked in for that instance. Later turns **add** newly-relevant items
  but never remove them. This keeps the tool-schema block (which sits in the
  Anthropic prompt-cache prefix) a stable, growing list — preserving cache hits
  while staying far smaller than all-tools. The main set **resets per session**; a
  **sub-agent starts fresh** from its own task ranking (it does **not** inherit the
  parent's accumulated set, or the savings would be lost).
- **Recomputed each step.** The tool list is rebuilt from the sticky set on every
  agent step, so a `discover_more` attachment made this turn is visible to the next
  LLM step.
- **Resumed sessions.** On loading a session from disk, the sticky set is **seeded**
  from the tool names actually referenced in the loaded message history, so a
  resumed conversation keeps the tools it was already using (otherwise the first
  post-resume turn could detach tools the history still references).
- **Known limitation — savings decay.** With per-tool granularity, a long
  multi-topic session accumulates toward "most tools attached"; the benefit is
  front-loaded and trends down over session length while per-turn query-embed +
  name-index overhead persists. Acceptable for typical sessions; if it bites, the
  rejected "sticky-with-evict" variant is the revisit path.
- **Gated:** MCP tools + skill-catalog entries only.
  - **Phasing note:** Plan 1 (first implementation) gates **MCP tools only** and
    leaves the skill catalog fully injected as today. Skill gating requires
    suppressing `skill.BuildCatalog()` from the cached context prefix, which races
    with ocode's context preload + snapshot + marker-dedup path — deferred to a later
    phase since MCP tool schemas are the dominant context cost. The name index and
    `discover_more` therefore cover MCP tools in Plan 1.
- **Never gated (core allowlist).** An explicit constant set, never subject to
  discovery: `read, edit, write, glob, grep, list, bash, bash_output, kill_shell,
  wait, lsp, task, agent_status, task_status, advisor`, and `discover_more`.
  (`webfetch`/`websearch` *are* gateable — they rank like any other tool.)
- **Pinned-always:** skills in `discovery.pinned_skills` (default
  `["brainstorming", "using-superpowers"]`) are always attached regardless of score.
- **MCP server instructions** (the separate instructions text some servers inject)
  are gated **in lockstep** with that server's tools — if no tool from a server is
  attached, its instructions are dropped too, so we never keep instructions for an
  absent tool.
- **Fail-open:** any embedder error (network down, model not downloaded, bad key)
  → fall back to **attach everything** (today's behavior) and write a `WARN` to the
  Log tab. Discovery never breaks a task.
- **Sub-agent integration:** the discovery filter is an **additional** filter,
  intersected with the existing `spec.Tools` whitelist / `isToolAllowed`. It never
  widens a restricted sub-agent's tool set.

### 4. Recovery — two safety nets

1. **Always-visible name index.** A cheap one-line-per-item list of *all* skill +
   MCP tool names (names only, no schemas) stays in context so the model always
   knows what exists, even when a tool's full schema isn't attached. Mirrors
   ocode's existing on-reference skill loading.
2. **`discover_more(need string)` tool.** Always attached. The model calls it with
   a natural-language need ("I need to send email"); it re-runs retrieval against
   the corpus, hot-attaches matches for the rest of the turn (adding to the sticky
   set), and returns what was attached so the model can retry.

**Prompt contract (required — recovery is inert without it).** When discovery is on,
a system-prompt fragment is injected that tells the model: *not every tool is
loaded; the "Available" index below lists every skill and MCP tool by name; if you
need one that isn't currently callable, call `discover_more` with a short
description of what you need before saying you can't do it.* Without this the model
just reports "I can't do X" instead of summoning the tool. The name index is capped
(names + ≤5-word hint) and itself excluded from the savings claim below.

---

## Commands (`internal/tui/commands.go`, `picker.go`, `model.go`)

All discovery commands are added to the `isInstantCmd` list so they run
immediately instead of queueing behind a streaming agent.

- **`/discovery on|off`** — toggle discovery; status when no arg. Persists via
  targeted saver `config.SaveDiscoveryEnabled(bool)`.
- **`/discover`** — status report: enabled/disabled, active backend + embedding
  model, local-model download state (none|downloading|ready), and the skills/MCP
  tools **currently attached for this session** (the sticky set) with counts.
- **`/discover model`** — opens a picker (new `pickerKind = "embedding-model"`)
  listing HTTP embedding models + the local LFM2-5 option (annotated with download
  status). Selecting persists via `config.SaveQueryEmbeddingModel(id)`, sets the
  backend, and invalidates the corpus cache. `/discover model <id>` sets directly
  without the dialog.

Adding the picker kind touches the five existing picker conditionals
(`openModelPicker` wrapper, `pickerVisibleItems`, `pickerRowForY`,
`selectPickerIndex` routing, `renderPicker` title/hint), following the
`redaction-model` precedent.

## `/context` reflection (`handleContextCmd`)

When discovery is **on**, add a **Discovery** section showing:

- backend + embedding model (+ download state if local),
- attached vs total: `skills A/X`, `MCP tools B/Y`,
- **net tokens saved** = gated tool-schema tokens − (name-index tokens + per-turn
  query-embed tokens). Show gross *and* net so the new overhead isn't hidden.
- the retriever's own per-turn embedding cost (tokens/$ for HTTP; "local" otherwise),
- an honest **"N items not attached"** line — no silent caps (per the
  no-silent-truncation rule).

When discovery is off, the section is omitted and the existing breakdown is unchanged.

## Debug logging — Log tab (`internal/debuglog`)

New entry kind `KindDiscovery` (+ TUI alias + a filter toggle in the Log tab).
Lines appended via `debuglog.Log.Append`:

- corpus warm: doc count, ms, model, dim;
- per-turn rank: query snippet → `K/N attached` with the δ/floor/cap used;
- per-item attach/skip decisions with scores;
- `discover_more` calls and what they attached;
- sticky-set growth;
- fail-open fallbacks (WARN);
- local model download progress.

## Config (`internal/config/ocodeconfig.go`)

A `discovery` block on `OcodeConfig`, persisted only through **targeted
load-modify-write savers** (never a whole in-memory snapshot write — concurrent
sessions would erase each other):

```jsonc
"discovery": {
  "enabled": false,
  "embedding_model": "",                 // unset — chosen via /discover model; no implicit default
  "embedding_backend": "http",           // "http" | "local"
  "local_model_status": "none",          // none | downloading | ready
  "pinned_skills": ["brainstorming", "using-superpowers"]
}
```

Savers: `SaveDiscoveryEnabled(bool)` (rejects with guidance if no usable embedder),
`SaveQueryEmbeddingModel(id)` (also sets backend + triggers cache invalidation),
`SaveLocalModelStatus(status)`. Load wiring in `loadOcodeConfigFile`, write wiring
in `writeOcodeConfigFile`, defaults in `defaultOcodeConfig` (enabled=false,
embedding_model unset). Cache files written **atomically** (temp + rename) to avoid
the concurrent-write corruption class seen previously with config.

## Testing

A `fakeEmbedder` (deterministic vectors from text hashes) backs unit tests for the
selection policy (rank-relative δ/floor/cap), sticky/monotonic growth, fail-open
fallback, sub-agent isolation, and resumed-session seeding — no network, no model
download. HTTP and local embedders get thin integration tests behind build
tags/env-gated keys.

---

## Worked example

**User turn:** *"Summarize the latest Notion meeting notes and email the summary to the team."*

Corpus (embedded once, cached). Query embedded with query prefix. Cosine ranks:

```
0.71  Notion/query-meeting-notes   ✓ top score
0.66  Notion/search                ✓ within δ=0.15 of top
0.62  Notion/fetch                 ✓ within δ
0.58  Gmail/create_draft           ✓ within δ (0.71−0.58=0.13)
0.57  skill:agentmail              ✓ within δ
0.54  Gmail/search_threads         ✗ 0.71−0.54=0.17 > δ, floor(5) met
0.40  Calendar/create_event        ✗
0.21  context7/query-docs          ✗
```

Rank-relative policy (within δ=0.15 of top, floor 5, cap 30) → 5 items attached;
the rest stay out of the tool schemas but remain in the cheap name index. Sticky
set = `{notion/query-meeting-notes, notion/search, notion/fetch, gmail/create_draft, agentmail}`.
(Scores illustrative; absolute values vary by embedder — only deltas-from-top drive
selection.)

Mid-turn the model needs the calendar → `discover_more("schedule a follow-up on the calendar")`
→ `Calendar/create_event` scores 0.68 → hot-attached, sticky set grows to 6.

---

## Risks addressed

| Risk | Resolution |
|------|-----------|
| Prompt-cache busting from per-turn tool churn | Sticky/monotonic grow-only set keeps prefix stable |
| Superpowers/brainstorming gated out | `pinned_skills` allowlist, always attached |
| MCP server instructions kept without their tools | Instructions gated in lockstep with the server's tools |
| Per-turn embedding latency (~100–300ms HTTP) | Background-warmed corpus; only query embed per turn; logged |
| Privacy (HTTP sends query + descriptions to 3rd party) | Documented; local backend exists for privacy |
| Needed tool not attached after topic drift | Name index + `discover_more` recovery |
| Tool referenced earlier disappears mid-session | Sticky set never removes |
| Sub-agent over-broadening | Discovery filter ∩ existing spec whitelist |
| Model switch dimension mismatch | Full corpus cache invalidation |
| Embedder failure breaks task | Fail-open: attach all + WARN |
| Absolute threshold not portable across embedders | Rank-relative (within δ of top) selection |
| Anthropic users have no embedding key | No silent default; local first-class; `/discovery on` validates a usable embedder |
| Short follow-ups embed to noise | Query = message + rolling recent-turn window |
| Recovery tools ignored by the model | Required system-prompt contract that activates the index + `discover_more` |
| Overstated savings | `/context` nets out name-index + query-embed overhead |
| Savings decay on long sessions (per-tool sticky) | Documented limitation; evict-variant is the revisit path |
| Concurrent corpus-cache writes corrupt the file | Atomic temp + rename |
| Resumed session detaches in-use tools | Seed sticky set from tool names in loaded history |

## Open implementation-plan decisions (not design-level)

- Local embedder runtime binding (ONNX runtime vs llama.cpp cgo) and exact LFM2-5
  artifact + download URL.
- Exact HTTP embedding endpoints/auth per provider.
- Token-estimate function reused for the `/context` savings figure.

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

Asymmetric encoding: corpus embedded as `Passage`, query embedded as `Query`
(instruction prefix for LFM2-5; input-type hint for HTTP backends).

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

**Selection policy (internal constants):** attach every item with
`cosine ≥ 0.30`, but always at least **floor = 3** and never more than
**cap = 12**. Empty corpus (no skills/MCP) → discovery is a no-op, no embedder call.

### 3. Attachment — sticky/monotonic, fail-open

- Re-rank **per user turn** (query = the user message) and **per sub-agent spawn**
  (query = the agent's task prompt).
- The session holds a **grow-only** attached set: the union of everything ranked
  in this session. Later turns **add** newly-relevant items but never remove them.
  This keeps the tool-schema block (which sits in the Anthropic prompt-cache
  prefix) a stable, growing list — preserving cache hits while staying far smaller
  than all-tools. The set **resets per session**.
- **Gated:** MCP tools + skill-catalog entries only.
- **Never gated:** core built-in tools (read, edit, write, glob, grep, list, bash,
  task, etc.) and the `discover_more` tool.
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
- **tokens saved** vs attaching everything (estimated from the gated tool schemas),
- the retriever's own per-turn embedding cost (tokens/$ for HTTP; "local" otherwise),
- an honest **"N items not attached"** line — no silent caps (per the
  no-silent-truncation rule).

When discovery is off, the section is omitted and the existing breakdown is unchanged.

## Debug logging — Log tab (`internal/debuglog`)

New entry kind `KindDiscovery` (+ TUI alias + a filter toggle in the Log tab).
Lines appended via `debuglog.Log.Append`:

- corpus warm: doc count, ms, model, dim;
- per-turn rank: query snippet → `K/N attached` with the floor/cap/threshold used;
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
  "embedding_model": "openai/text-embedding-3-small",
  "embedding_backend": "http",          // "http" | "local"
  "local_model_status": "none",          // none | downloading | ready
  "pinned_skills": ["brainstorming", "using-superpowers"]
}
```

Savers: `SaveDiscoveryEnabled(bool)`, `SaveQueryEmbeddingModel(id)` (also sets
backend + triggers cache invalidation), `SaveLocalModelStatus(status)`. Load
wiring in `loadOcodeConfigFile`, write wiring in `writeOcodeConfigFile`, defaults
in `defaultOcodeConfig` (enabled=false).

---

## Worked example

**User turn:** *"Summarize the latest Notion meeting notes and email the summary to the team."*

Corpus (embedded once, cached). Query embedded with query prefix. Cosine ranks:

```
0.71  Notion/query-meeting-notes   ✓ attach
0.55  Notion/search                ✓ attach
0.48  Notion/fetch                 ✓ attach
0.44  Gmail/create_draft           ✓ attach
0.33  skill:agentmail              ✓ attach
0.29  Gmail/search_threads         ✗ below 0.30, floor met
0.18  Calendar/create_event        ✗
0.07  context7/query-docs          ✗
```

Policy (≥0.30, floor 3, cap 12) → 5 items attached; ~40 others stay out of the
tool schemas but remain in the cheap name index. Sticky set =
`{notion/query-meeting-notes, notion/search, notion/fetch, gmail/create_draft, agentmail}`.

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

## Open implementation-plan decisions (not design-level)

- Local embedder runtime binding (ONNX runtime vs llama.cpp cgo) and exact LFM2-5
  artifact + download URL.
- Exact HTTP embedding endpoints/auth per provider.
- Token-estimate function reused for the `/context` savings figure.

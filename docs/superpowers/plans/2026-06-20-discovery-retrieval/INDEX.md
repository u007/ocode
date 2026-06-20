# Discovery-based skill/MCP retrieval — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Each part file is self-contained — no "see part N" cross-references for code.

**Goal:** Add an opt-in mode that, before each turn and each sub-agent spawn, embeds the task and attaches only the relevant MCP tools + skills (instead of injecting every MCP schema and the full skill catalog), cutting fixed per-request context.

**Architecture:** A new isolated `internal/discovery` package owns an `Embedder` interface (HTTP backend + deterministic fake for tests), a corpus index (one vector per skill and per MCP tool) with an atomic on-disk cache, rank-relative selection, and a per-agent grow-only sticky attached-set. The agent consults a discovery gate inside `GetToolDefinitions()` (filtering + deterministic ordering) and injects a name index + prompt contract on the volatile message tail. TUI commands `/discovery` and `/discover` toggle/inspect it; `/context` reports it; a `KindDiscovery` debug-log kind surfaces ranking on the Log tab.

**Tech Stack:** Go (module `github.com/u007/ocode`); standard library only for the core (crypto/sha256, encoding/json, math, sort, sync); HTTP embedder uses `net/http`. Bubble Tea TUI. No new third-party dependencies in Plan 1.

**Scope:** A single plan covering both embedder backends. The **HTTP backend**
(OpenAI/Voyage) and the **local backend** — a shared local model-server subprocess
that ocode spawns and talks to over HTTP, with an **MLX** build on Apple Silicon
(darwin/arm64) and llama.cpp/ONNX elsewhere. Both implement the same `Embedder`
interface. Gating covers **MCP tools and skills**. Build order is bottom-up: the
HTTP-backed core (Tasks 1–14) is fully working and testable on its own; the local
server backend (Tasks 15–18) plugs into the `ResolveEmbedder` seam afterward.

**Spec:** `docs/superpowers/specs/2026-06-20-discovery-retrieval-design.md`

---

## Global Constraints

Every task implicitly includes these. Exact values:

- **Go module path:** `github.com/u007/ocode`. Import the discovery package as `"github.com/u007/ocode/internal/discovery"`.
- **Discovery defaults OFF.** `discovery.enabled` default `false`. When off, behavior is byte-identical to today (no embedder calls, no context change, no tool gating).
- **No implicit embedding-model default.** `discovery.embedding_model` defaults to `""`. Enabling discovery without a usable embedder is a hard error with guidance — never silently pick a vendor.
- **No fallbacks / no silent recovery** except the one explicit, documented **fail-open** path (embedder error → attach everything), which **must** log a `WARN`/`DISCOVERY` line. No empty catch/ignored errors. Every caught error is logged with what was attempted + the error.
- **Targeted config savers only.** Never call `SaveOcodeConfig(in-memory snapshot)` from a session. Add `SaveX`-style load-modify-write savers mirroring `SaveUploadDir`.
- **Atomic file writes.** The corpus cache is written via temp-file + `os.Rename` (never a bare `os.WriteFile` over the live path).
- **Deterministic tool ordering.** `GetToolDefinitions()` must emit tools in a stable sorted order (core allowlist in fixed order, then attached items sorted by name). `a.tools` is a map (random iteration) — unordered output busts the provider tool-cache and defeats the sticky-set benefit.
- **Cache-stability contract** (see below) — discovery's per-turn-varying text rides the volatile message tail, never the cached prefix.
- **TDD, frequent commits.** Each task: failing test → run (fail) → minimal impl → run (pass) → commit.
- **Listings sorted.** Any user-facing list (`/discover` status, name index, picker) is sorted.

---

## Cache-Stability Contract (read before Parts 06–07)

ocode keeps the Anthropic/OpenAI prompt-cache prefix stable by appending volatile
content at the message tail (see `Step()` → `injectLSPDiagnostics` → `injectNotesTail`
in `internal/agent/agent.go`, documented in `append_stable.go`). Discovery must obey
the same rule:

1. **Tool schemas** (the `tools` API param) are produced by `GetToolDefinitions()`.
   The attached set is **grow-only within an agent instance** (sticky), and the
   output is **sorted deterministically**. Precise benefit: on a turn that attaches
   nothing new, the tool list is byte-identical → full cache hit. A newly-attached
   tool inserts at its sorted position and invalidates the cache from that point
   (not "append-only"), but a no-new-attachment turn pays nothing — which is the
   common case once the session warms up.
2. **MCP name-index + prompt contract** are injected as a **single system message
   appended at the tail** of `messages` inside `Step()` (a new
   `injectDiscoveryContext`), exactly like `injectNotesTail`. They are **never** placed
   inside `LoadContext()` (the cached prefix). When discovery is off, the injector is a
   no-op and the bytes are identical to today.
3. **Skill catalog** is suppressed from `LoadContext()` based on the **config flag**
   `discovery.enabled` (known at preload time — no race with the discovery result).
   The tail injector supplies the skill name index + attached-skill descriptions; on
   fail-open it emits the full catalog instead. Suppression keys on the config flag at
   both call sites (`BasePromptMessages`, `askAgent` preload).

---

## File Map

**New package `internal/discovery/`:**
| File | Responsibility |
|------|----------------|
| `embedder.go` | `Embedder` interface, `EmbedKind`, `FakeEmbedder` (deterministic), curated HTTP model registry (`HTTPModels`) |
| `http.go` | `httpEmbedder` — OpenAI-compatible `/v1/embeddings` backend |
| `index.go` | `Doc`, `Corpus`, `BuildDocs`, cosine, rank-relative `Select` |
| `cache.go` | atomic on-disk vector cache keyed by `hash(text)+model+dim` |
| `session.go` | `Session` — per-instance grow-only sticky set; `Discover(query)` |
| `engine.go` | `Engine` — facade tying embedder+corpus+cache; `ResolveEmbedder` backend seam; background-warm goroutine |
| `localserver.go` | shared local model-server manager: probe-first, download (per-platform/MLX), spawn via supervisor, `localEmbedder` HTTP client |
| `download.go` | artifact download + sha256 verify + atomic cache write + per-platform manifest |
| `*_test.go` | unit tests per file (use `FakeEmbedder`) |

**New agent glue file `internal/agent/discovery_glue.go`:**
| Responsibility |
|----------------|
| `*Agent` discovery field accessors, `RunDiscovery(query)`, gate predicate `discoveryAllows(name)`, `injectDiscoveryContext(messages)`, `discoverMoreTool` |

**Modified files:**
| File | Change |
|------|--------|
| `internal/config/ocodeconfig.go` | `DiscoveryConfig` struct + file mirror + default + load/write wiring + 3 savers |
| `internal/agent/agent.go` | `Agent.disco` field; `GetToolDefinitions` gate + sort; move tool-def computation into Step loop; call `RunDiscovery` + `injectDiscoveryContext` in `Step` |
| `internal/agent/context.go` | `LoadContext` skips `BuildCatalog` when the discovery config flag is on |
| `internal/agent/subagent.go` | mark sub-agent MCP tools so its own `Step` ranks its task |
| `internal/debuglog/debuglog.go` | add `KindDiscovery` |
| `internal/tui/debuglog.go` | add `DebugKindDiscovery` alias |
| `internal/tui/commands.go` | `/discovery`, `/discover` specs + handlers |
| `internal/tui/model.go` | `isInstantCmd` entries; `handleDiscoveryCmd`/`handleDiscoverCmd`; `/context` Discovery section; Log-tab filter+color for the new kind |
| `internal/tui/picker.go` | `embedding-model` picker kind (5 conditional sites) + `openEmbeddingModelPicker` |

---

## Tasks & Execution Order

Build bottom-up so each task compiles and tests green on its own.

| # | Part file | Task | Deliverable |
|---|-----------|------|-------------|
| 1 | `01-config.md` | Discovery config + savers | `discovery` block persists round-trip |
| 2 | `02-embedder.md` | `Embedder` iface + `FakeEmbedder` | deterministic embedding + cosine-testable |
| 3 | `02-embedder.md` | `httpEmbedder` + model registry | real OpenAI-compatible embeddings |
| 4 | `03-index.md` | `Doc`/`Corpus`/`BuildCorpus` (skill + MCP docs) | corpus docs with stable IDs |
| 5 | `03-index.md` | cosine + rank-relative `Select` | δ/floor/cap selection |
| 6 | `04-cache.md` | atomic disk cache + invalidation | warm-start, model-switch invalidation |
| 7 | `05-engine.md` | `Engine` + `Session` sticky + `ResolveEmbedder` | end-to-end `Discover(query) → attached set` |
| 8 | `06-agent-gate.md` | gate in `GetToolDefinitions` + Step hook | MCP tools filtered+sorted; per-turn rank |
| 9 | `07-context.md` | name index + prompt contract tail inject | recovery primed |
| 10 | `08-recovery.md` | `discover_more` tool | mid-turn hot-attach |
| 11 | `08-recovery.md` | sub-agent MCP marking | sub-agents get own attached set |
| 12 | `09-logging.md` | `KindDiscovery` + Log tab | ranking visible on Log tab |
| 13 | `10-commands.md` | `/discovery`, `/discover`, picker | toggle/select/inspect |
| 14 | `10-commands.md` | `/context` Discovery section | net savings reported |
| 15 | `11-skill-gating.md` | suppress catalog (config flag) + skill docs in corpus | skills ranked + gated; fail-open full catalog |
| 16 | `12-background-warm.md` | background corpus warm goroutine | first turn doesn't block on cold embed |
| 17 | `13-local-server.md` | pin artifacts (manifest: server + LFM2-5, per-platform/MLX, sha256) | concrete download manifest |
| 18 | `13-local-server.md` | local model-server manager + `localEmbedder` + `ResolveEmbedder("local")` | shared subprocess, probe-first, MLX on Apple |

**Dependency note:** Tasks 1–7 are pure `internal/discovery` + config (no TUI/agent coupling) — implement and review before any integration. Tasks 8–14 wire the HTTP-backed feature into the agent + TUI (fully usable with an HTTP key after Task 14). Task 15 adds skill gating. Task 16 adds background warming. Tasks 17–18 add the local backend behind the `ResolveEmbedder` seam — start only after Task 7 is green and the agent path (8–14) works. Do not start Task 8 until Task 7 is green.

---

## Out of scope (explicitly deferred)
- Per-tool gating finer than "one vector per MCP tool" (already per-tool here).
- Exposing δ/floor/cap as user settings (internal constants).
- Reducing MCP process/connection cost — MCP servers still connect + `ListTools()` at startup; discovery saves **context only**.
- **Resumed-session sticky seeding** — the `Session.Seed(ids)` primitive exists (Part 05); wiring the call that scans a loaded session's history for tool-use names is deferred. (Low impact: the set re-grows from the first post-resume turn; past tool_use/tool_result pairs stay valid regardless.)
- **Sub-agent `discover_more`** — registered on the main agent; sub-agents with a `spec.Tools` whitelist can't call it (they still get the name index + seeded gating). Widening whitelists is a follow-up.
- Embedding-key validity check at toggle time (presence only; invalid keys fail-open on turn 1 with a logged WARN).

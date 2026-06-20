# Discovery-based skill/MCP retrieval — Implementation Plan (Plan 1: HTTP path)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Each part file is self-contained — no "see part N" cross-references for code.

**Goal:** Add an opt-in mode that, before each turn and each sub-agent spawn, embeds the task and attaches only the relevant MCP tools + skills (instead of injecting every MCP schema and the full skill catalog), cutting fixed per-request context.

**Architecture:** A new isolated `internal/discovery` package owns an `Embedder` interface (HTTP backend + deterministic fake for tests), a corpus index (one vector per skill and per MCP tool) with an atomic on-disk cache, rank-relative selection, and a per-agent grow-only sticky attached-set. The agent consults a discovery gate inside `GetToolDefinitions()` (filtering + deterministic ordering) and injects a name index + prompt contract on the volatile message tail. TUI commands `/discovery` and `/discover` toggle/inspect it; `/context` reports it; a `KindDiscovery` debug-log kind surfaces ranking on the Log tab.

**Tech Stack:** Go (module `github.com/u007/ocode`); standard library only for the core (crypto/sha256, encoding/json, math, sort, sync); HTTP embedder uses `net/http`. Bubble Tea TUI. No new third-party dependencies in Plan 1.

**Scope:** Plan 1 ships the entire feature on the HTTP embedder. The **local LFM2-5 backend** (runtime model download + ONNX/llama.cpp binding) is **Plan 2** — it implements the same `Embedder` interface and plugs in with no changes to Plan 1 code.

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
   output is **sorted deterministically**, so the tool block only ever grows by
   appended entries → cache invalidates only from the growth point, not every turn.
2. **MCP name-index + prompt contract** are injected as a **single system message
   appended at the tail** of `messages` inside `Step()` (a new
   `injectDiscoveryContext`), exactly like `injectNotesTail`. They are **never** placed
   inside `LoadContext()` (the cached prefix). When discovery is off, the injector is a
   no-op and the bytes are identical to today.
3. **Plan 1 does not touch `LoadContext()` or the skill catalog.** Skill gating
   (suppressing `skill.BuildCatalog()`) is deferred — it would race with the context
   preload/snapshot/marker-dedup path. Plan 1 gates MCP tools only.

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
| `engine.go` | `Engine` — config-driven facade tying embedder+corpus+cache+warming |
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
| `internal/agent/subagent.go` | seed sub-agent discovery from `params.Prompt` |
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
| 4 | `03-index.md` | `BuildDocs` from skills + MCP tools | corpus docs with stable IDs |
| 5 | `03-index.md` | cosine + rank-relative `Select` | δ/floor/cap selection |
| 6 | `04-cache.md` | atomic disk cache + invalidation | warm-start, model-switch invalidation |
| 7 | `05-engine.md` | `Engine` + `Session` sticky + warming | end-to-end `Discover(query) → attached set` |
| 8 | `06-agent-gate.md` | gate in `GetToolDefinitions` + Step hook | tools filtered+sorted; per-turn rank |
| 9 | `07-context.md` | MCP name index + prompt contract tail inject | recovery primed |
| 10 | `08-recovery.md` | `discover_more` tool | mid-turn hot-attach |
| 11 | `08-recovery.md` | sub-agent discovery seeding | sub-agents get own attached set |
| 12 | `09-logging.md` | `KindDiscovery` + Log tab | ranking visible on Log tab |
| 13 | `10-commands.md` | `/discovery`, `/discover`, picker | toggle/select/inspect |
| 14 | `10-commands.md` | `/context` Discovery section | net savings reported |

**Dependency note:** Tasks 1–7 are pure `internal/discovery` + config and have no TUI/agent coupling — they can be implemented and reviewed before any integration. Tasks 8–11 wire into the agent. Tasks 12–14 are TUI surface. Do not start Task 8 until Task 7 is green.

---

## Out of scope (Plan 2 / explicitly deferred)
- Local LFM2-5 embedder (runtime download, ONNX/llama.cpp binding). **Apple Silicon
  (darwin/arm64, M1+) must load the MLX build of the model**; other platforms use the
  ONNX/CPU build. Plan 1 leaves the seam: `engine` returns a clear "local backend not
  available yet" error when `embedding_backend == "local"`. The platform branch
  (`runtime.GOOS=="darwin" && runtime.GOARCH=="arm64"` → MLX artifact) and the
  per-platform download manifest are implemented in Plan 2.
- Per-tool gating finer than "one vector per MCP tool" (already per-tool here).
- Exposing δ/floor/cap as user settings (internal constants).
- Reducing MCP process/connection cost — servers still connect + `ListTools()` at startup; discovery saves **context only**.
- **Skill gating** — Plan 1 gates MCP tools only; the skill catalog stays as today (avoids the cached-prefix race). The MCP name index + `discover_more` cover MCP.
- **Background-at-session-start warming** — Plan 1 warms the corpus **lazily** on the first discovery run (one cached batch call; effectively one-time). True background warming in a goroutine at session start is a later optimization.
- **Resumed-session sticky seeding** — the `Session.Seed(ids)` primitive exists (Part 05); wiring the call that scans a loaded session's history for tool-use names and seeds the set is deferred. (Low impact: the sticky set re-grows from the first post-resume turn, and previously-used tool_use/tool_result pairs remain valid in history regardless.)

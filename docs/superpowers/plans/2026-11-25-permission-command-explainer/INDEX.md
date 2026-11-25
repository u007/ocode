---
type: Plan
title: Permission Command Explainer Implementation Plan
description: 'Split implementation plan (INDEX.md + parts 01–06) for the isolated on-demand permission-call explainer: full secret-masked parameter paging, six restricted read-only tools, hardened metadata-only Git inspection, process-wide ask-keyed coordination and cache, race-safe client snapshots, TUI/web Markdown output, and full-suite verification. Supersedes the single-file plan of the same date.'
tags:
  - permissions
  - explainer
  - plan
  - superpowers
timestamp: 2026-11-25T07:19:25Z
---
# Permission Command Explainer Implementation Plan

> REQUIRED SUB-SKILL: Use superpowers:executing-plans (default) or superpowers:subagent-driven-development to implement this plan part-by-part.

> **For agentic workers:** implement part-by-part in the order below, ticking each `- [ ]` step. This is a decisions-not-code plan: every file, name, signature, value and test is pinned; the implementer writes the bodies.

**Goal:** Replace the one-shot permission-call digest with an isolated, on-demand explainer that pages the complete secret-masked call parameters, inspects project files and hardened read-only shell/Git metadata, and returns full Markdown, without touching the chat loop.

**Architecture:** A fresh side client runs a six-step local tool loop over exactly six restricted tools (`read`, `glob`, `grep`, `list`, `inspect_shell`, `call_parameters`). A process-wide coordinator keys active runs and a short-lived success cache by owner+ask, cancels only the same key, and admits at most three runs; all mutable agent state is snapshotted under a new leaf lock before the loop. TUI, web, and server keep the existing endpoint and `{summary, model}` response.

**Tech Stack:** Go 1.26, Charm TUI (Bubble Tea / Lipgloss v2, `renderMarkdown`), React SPA (`react-markdown` + `remark-gfm` already installed), existing `internal/tool` process supervisor and `internal/shell/sandbox`.

**Spec:** `docs/superpowers/specs/2026-11-25-permission-command-explainer-design.md` (commit `9ec4fa05`).

**Detail:** decisions, not code — the default; the implementer writes the implementation.

## Global Constraints

- 80s overall; 25s per model call; 6 model steps; 12 project/shell inspection calls.
- 8 KiB parameter page; 8 KiB evidence result; 40 KiB total evidence; 10s shell; 8 KiB shell output.
- 1 MiB HTTP fallback body; process-wide semaphore 3; success cache 2min/128; full output no clamp.
- No cumulative parameter cap: every masked parameter byte stays pageable across sequential calls.
- No `Agent.Step` / transcript / `OnMessage` / `OnDelta` / hooks / discovery / todo / notes / advisors.
- Secrets-only masking, no PII; no content guard.
- Windows `inspect_shell` disabled; mandatory no-write sandbox on macOS/Linux.
- Metadata-only hardened Git; synchronous POST; no new endpoint / config / schema / migration.

## Review Focus

1. Huge or opaque parameter payloads, and pages requested after the first, must stay exactly reconstructable — Part 01 `TestToolDigestPayloadPage_ExactWindowsLargePayloads`, `TestToolDigestPayloadPage_UTF8Boundaries`; Part 02 `TestRunToolDigestLoop_MultiPageSequential`.
2. A hostile repository's Git config/attributes (fsmonitor, pager, hooks, diff.external, clean/smudge/process filters, textconv) must never execute — Part 04 `TestInspectShell_HostileRepoConfigAndAttributes`, `TestInspectShell_FullDiffNegativeControl`.
3. Sensitive paths and content must not leak through grep/glob/list/find or Git metadata output — Part 03 `TestFileTools_SensitivePathsFiltered`, `TestFileTools_GrepSkipsSensitiveBeforeRead`; Part 04 `TestInspectShell_GitMetadataFiltersSensitivePaths`.
4. Concurrent asks, session switch/reset, and stale UI must not cross-contaminate — Part 02 `TestToolDigestCoordinator_DifferentAsksIndependent`; Part 05 `TestHandleToolCallDigest_CrossTabSuperseded`, `TestPermDigest_StaleReplyDropped`.
5. Cancellation and timeouts must kill whole process groups and discard late replies — Part 03 `TestSearchTools_CancelPromptly`; Part 04 `TestInspectShell_TimeoutKillsProcessGroup`; Part 05 `TestHandleToolCallDigest_CancelOnRequestAbort`.

## Parts (in order)

- [Part 01 — Contracts, full-parameter pager, context guard, client snapshot](01-contracts.md)
- [Part 02 — Isolated runner and coordinator](02-runner-coordinator.md)
- [Part 03 — Read-only file/search boundary and cancellation](03-file-search-boundary.md)
- [Part 04 — inspect_shell and hardened Git metadata](04-inspect-shell.md)
- [Part 05 — Server, TUI, and web integration](05-ui-server-integration.md)
- [Part 06 — Documentation and full verification](06-docs-verification.md)

# Secret Redaction Model — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Session-scoped secret redaction — detect secrets before any LLM send, replace with `[[OCSEC:<nonce>:<n>]]` placeholders (stored on disk), re-render real values in TUI, unmask at tool execution behind a secret-aware permission prompt.

**Architecture:** New `internal/redact` package (registry + tier-1 detectors + tier-2 local-model scanner + vault) injected as a shared `Redactor` into the four ingestion carriers; regex safety net + tripwire inside `GenericClient.ChatWithContext`; display re-substitution across TUI surfaces; config persisted via targeted saver; sidebar toggle + `/mask` command.

**Tech Stack:** Go, Bubble Tea TUI, existing config/permission patterns.

**Spec:** `docs/superpowers/specs/2026-06-12-secret-redaction-design.md` (source of truth — read before any part).

**Plan style note:** Per project planning rules, parts are high-level (files + functions/methods, no code snippets). Each part is self-contained. TDD throughout: failing test first, minimal implementation, run, commit.

## Execution order & dependencies

| Part | File | Depends on |
|---|---|---|
| 1 | `01-core-engine.md` — registry, token, tier-1 detectors, vault | — |
| 2 | `02-tier2-scanner.md` — local-model scan + fail-mode plumbing | 1 |
| 3 | `03-config-sidebar.md` — config section, targeted saver, sidebar toggle | 1 |
| 4 | `04-ingestion.md` — Redactor wiring into 4 carriers + title call-site | 1, 2, 3 |
| 5 | `05-chokepoint-net.md` — safety net + tripwire in ChatWithContext | 1, 3 |
| 6 | `06-tool-unmask.md` — HandleToolCall unmask + secret-aware permission prompt | 1, 4 |
| 7 | `07-display.md` — re-substitution across TUI render surfaces | 1, 4 |
| 8 | `08-secrets-command.md` — /mask modal (model picker, secret list, toggles) | 3, 7 |
| 9 | `09-edge-cases-integration.md` — compaction validation, mid-session scrub, end-to-end tests | all |

Parts 1–3 can run independently after Part 1. Parts 4+ are sequential as listed.

## Definition of done

- All part checklists complete; `go test ./...` green; `go vet ./...` clean.
- Manual smoke: enable via sidebar, paste a GitHub PAT, confirm session file on disk has placeholder, TUI shows real value, bash unmask prompts with secret-aware banner.
- Spec §Testing matrix fully covered.

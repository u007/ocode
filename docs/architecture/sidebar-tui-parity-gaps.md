---
type: Architecture
title: Sidebar TUI/Web Parity Gaps
description: Gap analysis of web frontend sidebar features missing relative to the TUI sidebar, covering backend fields not consumed and missing TS types.
resource: internal/tui/model.go, web/src/components/Layout/CoworkSidebar.tsx, internal/server/tui_status.go
tags:
  - architecture
  - sidebar
  - tui
  - web
  - parity
  - gap-analysis
timestamp: 2026-07-08T16:40:44Z
---
# Sidebar TUI / Web Parity Gaps

**Type:** Architecture  
**Status:** Current as of 2026-07-09  
**Files:** `internal/tui/model.go` ↔ `web/src/components/Layout/CoworkSidebar.tsx`

## Overview

The TUI sidebar and the web sidebar (`CoworkSidebar`) should present equivalent information to the user. The TUI sidebar is the canonical view; the web sidebar mirrors it via a combination of SSE-pushed `TUIStatus` snapshots and dedicated REST endpoints. This document catalogues features present in the TUI sidebar that are missing, partially implemented, or differently rendered in the web sidebar.

---

## TUI sidebar layout

The TUI sidebar (`buildSidebarRenderData` in `internal/tui/model.go:15507`) has three zones:

### Pinned top (rendered in `topLines`)
- Mode badge (`[CODE]`/`[PLAN]`) + model name
- Temperature · Reasoning level
- Context: token count / max (%)
- CWD (clickable)
- Advisor model ●on/○off + model name (clickable toggle)
- Small model ●on/○off + model name (clickable toggle)
- Perm model ●on/○off + model name (clickable toggle)
- Recap model ●on/○off + model name (clickable toggle)
- IDE status (clickable toggle)
- OCR model ●on/○off + model name (clickable toggle)

### Scrollable sections
1. Agents — running agents (name, model, up/down tokens) + "N completed" summary
2. Git — branch, ahead/behind, staged count, modified count, untracked count
3. Selection — current file selection with paths
4. Files — files changed this session, with git status prefix
5. TODO — tool-assigned todo items
6. Tools/MCP — "N configured, M loaded, E errors" one-liner
7. LSP — per-server cmd, state, lang_id, diagnostics errors/warnings

### Pinned bottom (rendered in `bottomLines`)
- Allowed section — permission mode, extra paths (N), bash auto-allow prefixes (N)
- Usage/spending lines
- Current theme name
- Shortcuts bar

---

## Web CoworkSidebar layout

The web sidebar (`web/src/components/Layout/CoworkSidebar.tsx`) has collapsible sections:

1. Agent — current agent name, description, session ID
2. Git — branch name + CWD (from `tuiStatus.cwd`)
3. Models — main/small/advisor text, advisor on/off toggle, OCR backend+model+on/off toggle, permission mode text
4. Context — token bar from `tuiStatus.context_current_tokens` / `context_max_tokens`
5. Extra allowed paths — from `tuiStatus.extra_allowed_paths`
6. LSP — per-server cmd, state, lang_id, diagnostics errors/warnings
7. Files — modified files
8. Tools/MCP — per-server name + on/off
9. TODO — empty/placeholder
10. Theme — theme selector dropdown

---

## Gap Catalog

### Gap 1: Mode badge (`[CODE]`/`[PLAN]`)

| | TUI | Web |
|---|---|---|
| **Source** | `m.agent.Mode()` → `strings.ToUpper(string(mode))` | Not in `TUIStatus` struct |
| **Status** | Rendered in pinned top | **Missing** |

The TUIStatus struct (`internal/server/tui_status.go`) has no `mode` field. The web cannot display the CODE/PLAN badge because the backend never sends it. Adding a `Mode string` field to `TUIStatus` and populating it from `m.agent.Mode()` would close this gap.

### Gap 2: Temperature + Reasoning effort

| | TUI | Web |
|---|---|---|
| **Source** | `m.agent.EffectiveTemperature()` + `thinkingBudgetLabels[m.thinkingLevelIdx]` | Not in `TUIStatus` struct |
| **Status** | Rendered in pinned top as `temp: 0.3 · reason: high` | **Missing** |

Neither temperature nor reasoning effort is exposed through `TUIStatus`. The web has no way to display them. Adding fields like `Temperature` and `ReasoningEffort` to `TUIStatus` and populating them in the RC bridge would close this gap.

### Gap 3: Recap model toggle

| | TUI | Web |
|---|---|---|
| **Source** | `m.recapModelEnabled` + `m.config.Ocode.RecapModel` | `tuiStatus.recap_model` and `tuiStatus.recap_model_enabled` exist in types.ts **but are not consumed** |
| **Status** | Pinned top toggle (●on/○off) | **Backend sends data, web ignores it** |

The `TUIStatus` Go struct has `RecapModel`/`RecapModelOn` fields and the TS type has `recap_model`/`recap_model_enabled`. However:

- The `chatStore` (`web/src/stores/chatStore.tsx`) does not have `recapModel`/`recapModelEnabled` in its state shape.
- `CoworkSidebar` does not destructure or render these fields — there is no recap row or toggle.
- The web could read `tuiStatus?.recap_model` directly from the snapshot, but no component does.

**Fix:** Add a recap model row to the Models section in `CoworkSidebar`, reading from `tuiStatus.recap_model` and `tuiStatus.recap_model_enabled`, with an on/off toggle analogous to the advisor toggle.

### Gap 4: Perm model toggle

| | TUI | Web |
|---|---|---|
| **Source** | `m.agent.Permissions().AutoPermissionEnabled()` + config | Permission mode shown as static text |
| **Status** | Clickable toggle (●on/○off) | **Static text only, no toggle** |

The web Models section shows the permission mode (e.g. "Auto-allow" or "normal") as plain text from `/api/permissions`. The TUI sidebar renders it as an interactive toggle. The web cannot toggle auto-permission from the sidebar.

### Gap 5: IDE status

| | TUI | Web |
|---|---|---|
| **Source** | `m.ideSidebarStatusLine()` | `tuiStatus.ide_mode` / `tuiStatus.ide_status` exist in types.ts |
| **Status** | Pinned top line | **Not rendered in CoworkSidebar** |

The IDE status (`ide_mode` + `ide_status`) is sent in TUIStatus but the web sidebar doesn't show it anywhere. The StatusBar shows it only in a condensed form.

### Gap 6: Selection section

| | TUI | Web |
|---|---|---|
| **Source** | `m.buildSelectionSidebarData()` | No equivalent data source |
| **Status** | Scrollable section with file paths | **Missing entirely** |

The TUI sidebar shows the user's current file/path selection with clickable file paths. The web has no selection tracking — users cannot see or interact with the current selection from the sidebar.

### Gap 7: Git details (ahead/behind, staged/modified/untracked counts)

| | TUI | Web |
|---|---|---|
| **Source** | `m.git.aheadBehind`, `stagedFiles`, `unstagedFiles`, `untrackedFiles` | Branch from `/api/git/status`, CWD from `tuiStatus.cwd` |
| **Status** | Branch + ahead/behind + staged/modified/untracked badges | **Branch + CWD only** |

The web sidebar fetches the git branch via `/api/git/status` and shows CWD from `tuiStatus.cwd`. It does not show ahead/behind (from TUI) or staged/modified/untracked file counts. The `/api/git/status` endpoint may return this data but the web doesn't consume it.

### Gap 8: Agent run registry

| | TUI | Web |
|---|---|---|
| **Source** | `m.agent.Runs().Snapshot()` | `/api/agents/runs` endpoint exists |
| **Status** | Shows running agents (name, model label, up/down tokens) + "N completed" summary | **Current agent name/description only** |

The web sidebar has an Agent section that shows only the current agent name, description, and session ID. The TUI sidebar lists all running sub-agent runs with their model labels and token usage, plus a summary of completed runs. The `/api/agents/runs` endpoint returns the full run registry but the web sidebar doesn't render it.

### Gap 9: Allowed/paths section (permission mode, extra paths, bash auto-allow)

| | TUI | Web |
|---|---|---|
| **Source** | `m.agent.Permissions()` + config | `tuiStatus.extra_allowed_paths` consumed; permissions from `/api/permissions` |
| **Status** | Bottom-pinned: mode label, extra paths (N), bash auto-allow prefixes (N) | **Extra paths shown in section; mode & bash auto-allow not shown** |

The web sidebar has a "Extra Allowed Paths" section that lists `tuiStatus.extra_allowed_paths`. The TUI sidebar shows this plus the permission mode label (e.g. "normal · auto") and bash auto-allow prefixes. The web does not show permission mode or bash auto-allow.

### Gap 10: Usage/spending in sidebar

| | TUI | Web |
|---|---|---|
| **Source** | `sidebarUsageLines(telemetry)` | `tuiStatus.spending_usd` consumed in StatusPanel |
| **Status** | Bottom-pinned: session total, per-model breakdown | **Only in StatusPanel drill-down, not in sidebar** |

The TUI sidebar shows token usage totals (input/output) and spending in the pinned bottom area. The web sidebar does not show any usage information — it's only available in the StatusPanel modal that opens from the status bar.

### Gap 11: Current theme display

| | TUI | Web |
|---|---|---|
| **Source** | `m.config.Ocode.TUI.Theme` (default "tokyonight") | Theme list from `/api/themes` |
| **Status** | Bottom-pinned: `theme: tokyonight` | **Theme selector in collapsible section, no pinned display** |

The web sidebar has a full theme selector with a list from `/api/themes` and clickable application. The TUI sidebar only shows the current theme name as a pinned badge. These are different UX patterns — the web is richer in this area.

### Gap 12: Shortcuts bar

| | TUI | Web |
|---|---|---|
| **Source** | Hardcoded `"Ctrl+B bg bash  r run  l lint  b build"` | No equivalent |
| **Status** | Bottom-pinned hints | **Missing** |

The TUI sidebar shows keyboard shortcut hints in the pinned bottom area. The web sidebar has no equivalent.

---

## Data Flow Summary

```
TUI Agent/MainModel
  ↓ m.agent.Mode() / EffectiveTemperature() / etc.
  ↓ m.recapModelEnabled / smallModelEnabled / etc.
  ↓ m.git.* / m.agent.Runs().Snapshot()
  ↓
TUI Sidebar (buildSidebarRenderData)
  ↓ RC Bridge (250ms debounce)
  ↓
TUIStatus (internal/server/tui_status.go — 20 fields)
  ↓ SSE "status" event (live) + GET /api/tui-status (poll)
  ↓
Web chatStore (tuiStatus field)
  ↓
CoworkSidebar consumes subset of tuiStatus + dedicated REST calls
```

**Fields in TUIStatus that ARE consumed by the web:**
- `main_model`, `small_model`, `small_model_enabled` — in Models section
- `advisor_model`, `advisor_enabled` — in Models section with toggle
- `ocr_backend`, `ocr_model`, `ocr_enabled` — in Models section with toggle
- `session_id`, `session_title` — Agent section
- `cwd` — Git section
- `context_current_tokens`, `context_max_tokens`, `context_model` — Context section
- `modified_files` — Files section
- `lsp_servers` — LSP section
- `extra_allowed_paths` — Extra Paths section
- `spending_usd` — StatusPanel only

**Fields in TUIStatus that are NOT consumed by the web:**
- `recap_model`, `recap_model_enabled` — backend sends, web ignores
- `ide_mode`, `ide_status` — backend sends, web ignores (except StatusBar condensed display)
- `subagent_model` — backend sends, web ignores

**Fields MISSING from TUIStatus (and thus unavailable to web):**
- `mode` (CODE/PLAN) — would need to be added to Go struct
- `temperature` — would need to be added
- `reasoning_effort` — would need to be added

## Recommended Actions

1. **Add `mode` field to `TUIStatus`** and populate from `m.agent.Mode()`.
2. **Add `temperature` and `reasoning_effort` fields to `TUIStatus`**.
3. **Render recap model toggle in CoworkSidebar** from existing `tuiStatus.recap_model`/`recap_model_enabled`.
4. **Render agent run registry in CoworkSidebar** from `/api/agents/runs`.
5. **Enrich the Git section** with ahead/behind, staged/modified/untracked counts.
6. **Add a Selection section** to CoworkSidebar (requires a backend data source for the current selection).
7. **Add usage/spending to the web sidebar** (not just StatusPanel).

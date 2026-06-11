# UI Overhaul Implementation Plan — Index

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** `docs/superpowers/specs/2026-06-11-ui-overhaul-design.md`

**Goal:** Reusable TUI component layer, true dimmed overlays with scroll-through during permission requests, consistent hover, and web UI completion (Git/Logs/theme sync).

**Architecture:** New component files inside the existing `internal/tui` package (Scrollbar, Button, ListBox, Dialog + overlay compositor, ModalStack). Surfaces migrate one per commit onto the components; detail drill-in views stay out of ModalStack. Web phase adds two read-only server endpoints and finishes the stub panels on existing shadcn/Radix primitives.

**Tech Stack:** Go, Bubble Tea v2, lipgloss, bubbles viewport/textinput; React 18 + Vite + Tailwind + Radix/shadcn.

**Plan rules (apply to every part):** High-level only — reference files and functions, no code in the plan. TDD: failing test → minimal implementation → pass → commit, one surface per commit. After every migration task, the full `go test ./internal/tui/...` suite must pass. Respect CLAUDE.md TUI rules: no raw stdout/stderr writes, AllMotion hover handlers must be cheap (cached hit-test maps), one-line rows clamped.

## Parts

| Part | File | Tasks | Delivers |
|------|------|-------|----------|
| 1 | `01-component-layer.md` | 1–6 | Scrollbar, Button, overlay compositor, Dialog, ListBox, ModalStack — all unit-tested, nothing wired yet |
| 2 | `02-tui-migrations-polish.md` | 7–14 | Each modal surface migrated to components; centered dimmed overlays; permission scroll-through; hover everywhere; consistency pass |
| 3 | `03-web-ui.md` | 15–19 | `GET /api/git/diff`, Git panel diff view, Logs panel wiring, theme-sync endpoint + CSS variable mapping, web consistency pass |

## Execution Order

Parts run strictly in order 1 → 2 → 3. Within Part 2, the permission dialog (Task 7) goes first — it exercises the compositor, fall-through routing, and key routing together; if it proves unstable, fall back to migrating the picker (Task 8) first to mature the components, then return to Task 7. Part 3 is independent of Part 2 except that the theme endpoint (Task 18) reuses `ThemeColors` from `internal/tui/theme.go` and may need that type exported/moved — decide at implementation time, preferring a small shared package only if an import cycle forces it.

## Definition of Done (whole plan)

- All modal surfaces render as centered overlays over a dimmed, live backdrop.
- With the permission dialog open: wheel scrolls the pane under the cursor; PgUp/PgDn and ctrl+u/d scroll the transcript; up/down scroll the dialog body; left/right move button focus; y/n/a/esc/enter decide.
- Every clickable TUI surface has a hover state; AllMotion handling stays allocation-light.
- `go test ./...` green; web `bun run typecheck` (tsgo) green; TUI remains shippable after every commit.

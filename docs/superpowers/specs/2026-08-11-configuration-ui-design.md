# Configuration UI — Design

- **Date:** 2026-08-11
- **Status:** Approved (design) — pending implementation plan
- **Goal:** Give ocode's web/desktop UI a unified Configuration surface,
  replacing today's scattered sidebar settings sections, and explicitly
  separate **ocode's own config** (`ocodeconfig.json`) from **opencode's
  config** (`opencode.json` — MCP servers, legacy plugins, model-selection
  state) instead of mixing them in one flat list.

## Context

Desktop (`cmd/ocode-desktop`) is a Wails v3 shell that embeds the same
`web/` React app used by the browser UI — they are one UI surface, not two
codebases (see `docs/web-desktop-parity-todo.md`).

Today there is no unified settings/configuration UI. Config-related controls
are scattered across independent, individually-toggled sections in
`web/src/components/Layout/CoworkSidebar.tsx` (`agent`, `git`, `models`,
`context`, `paths`, `lsp`, `files`, `tools`, `todo`, `theme`) plus standalone
dialogs (`ModelDialog.tsx`, `PluginsPanel.tsx`, `MonacoSettingsPanel.tsx`).
The desktop native app menu has no Settings entry at all.

The backend already maintains two distinct config sources, kept separate in
code comments (`internal/config/config.go:355-357`, `529-531`, `697-699`;
`internal/config/ocodeconfig.go:133`, `574-576`; `internal/config/state.go`):

- **`ocodeconfig.json` / `OcodeConfig` struct** (`internal/config/ocodeconfig.go`)
  — ocode's own settings: Compact, Advisor, Permissions, Plugins/ExternalPlugins,
  LocalModels, Security(Redaction), Discovery, MemoryEnabled, DocPromptEnabled,
  Terminal, ExtraAllowedPaths, Editor/EditorMode/IDEMode, SmallModel,
  RecapModel, CommitMsgModel/Prompt, TUI, MaxSteps, MaxImageDim,
  RecapTimeoutSeconds, UndoMaxAgeDelta, MaxConcurrentAgents, UploadDir, Ocr,
  ImageGen.
- **`opencode.json`** (`internal/config/config.go`) — a different tool's
  config format ocode reads/writes for compatibility: MCP server
  definitions (`"mcp"`) and a legacy `"plugins"` key. opencode also owns
  `${XDG_STATE_HOME}/opencode/model.json` (recent/favorite model selections).

Only a fraction of `OcodeConfig` fields have REST endpoints today (model,
theme, terminal, OCR, redaction — `internal/server/server.go:159-185`).
Most (permissions, security details, TUI, discovery, imagegen, path
allowlist, undo/recap timings) have no HTTP API and no UI.

Confirmed with user: **out of scope** — moving `ocodeconfig.json` off its
current path (`~/.config/opencode/ocodeconfig.json`, nested inside
opencode's directory). This spec addresses the UI/API mixing only, not
on-disk file layout.

## 1. User-facing summary

A new **Configuration** page, reachable via:

- A "Configuration" button in `CoworkSidebar.tsx` (replacing the individual
  `models` / `theme` / `tools` (MCP) / `plugins` / `paths` sections it
  currently exposes inline).
- A native **"Settings…"** item in the desktop app menu
  (`cmd/ocode-desktop/main.go` `buildAppMenu`), which emits an event into
  the shared webview to navigate there — same pattern as `confirmQuit`.

The page has a left nav of category groups and a right pane showing the
selected group's form, under two top-level headings:

**ocode**
- Models (moved from `ModelDialog.tsx`)
- Theme (moved from sidebar `theme` section)
- Terminal
- OCR
- Security / Redaction
- Permissions (tool & bash permission modes, prefix allow/deny lists)
- Compact & Advisor
- Discovery
- TUI (theme, mouse, scroll, keybinds — desktop/web read-only view where a
  setting is TUI-specific and inapplicable to web, otherwise editable)
- Editor / Editor Mode / IDE Mode
- Image Generation
- Paths (`ExtraAllowedPaths`, moved from sidebar `paths` section)
- Plugins (moved from `PluginsPanel.tsx` — this covers ocode's
  `ExternalPlugins`, distinct from opencode's legacy `plugins` key below)

**opencode**
- MCP Servers (moved from sidebar `tools` section)
- Plugins (legacy `opencode.json` `"plugins"` key, kept separate from
  ocode's Plugins group above since they are different data)
- Model Selection State (read-only view of `opencode/model.json`
  recent/favorite models, informational — this file is opencode-owned)

Sidebar sections that are session-workflow, not settings — `agent`, `git`,
`context`, `lsp`, `files`, `todo` — are unchanged and stay in
`CoworkSidebar.tsx`.

## 2. Decisions (confirmed with user)

| Topic | Decision |
|-------|----------|
| Backend scope | Expose every currently-hidden `OcodeConfig` field group via new REST endpoints, not just what's already wired up |
| UI shape | Dedicated full page/route (`/settings`), not a modal — the field count doesn't fit a dialog well |
| Entry points | Both a sidebar "Configuration" button and a native desktop "Settings…" menu item |
| Existing sidebar sections | Settings-like sections (models, theme, MCP/opencode tools, plugins, paths) move into the new page; workflow sections (agent, git, context, lsp, files, todo) stay in the sidebar |
| ocode vs opencode grouping | Two explicit top-level nav groups in the new page, mirroring the separation already enforced in Go code comments |
| `ocodeconfig.json` file path | Left unchanged — out of scope for this spec |

## 3. Architecture

### Routing

`web/src/App.tsx` has no router today (only `SessionPage.tsx` is rendered).
Add a minimal view switch — a `currentView: 'session' | 'settings'` state
in `App.tsx` — rather than pulling in a router library for one new page.
The "Configuration" sidebar button and the desktop menu event both set
`currentView = 'settings'`; a "Back" affordance in the Settings page sets
it back to `'session'`.

### Frontend components

New `web/src/pages/SettingsPage.tsx`:
- Left nav: two headed groups (`ocode`, `opencode`), each listing its
  category items (per section 1). Active item highlighted, matches
  existing sidebar nav visual style.
- Right pane: renders one `Settings/<Group>Form.tsx` component per
  selected category, under `web/src/components/Settings/`.
- Each form component: loads its section's current value on mount via
  `api/client.ts`, edits with existing shadcn/ui form primitives (already
  used in `PluginsPanel.tsx` / `ModelDialog.tsx` for consistency), saves via
  PUT with toast feedback matching current dialog behavior.
- Existing components being relocated (`ModelDialog.tsx` picker logic,
  `PluginsPanel.tsx`, the sidebar's MCP/tools list, theme selector, paths
  editor) are refactored into `Settings/` form components rather than
  duplicated; their trigger buttons are removed from `CoworkSidebar.tsx`.

### Desktop native menu

`cmd/ocode-desktop/main.go` `buildAppMenu`: add a "Settings…" menu item
(macOS: in the app menu near About; Windows/Linux: under File) that emits
a Wails event consumed by the webview to set `currentView = 'settings'`,
following the existing `confirmQuit` event-wiring pattern.

### Backend

New endpoints in `internal/server/handler_config.go`, registered in
`internal/server/server.go` alongside the existing model/theme/terminal/
OCR/redaction handlers, one `GET/PUT /api/config/ocode/<section>` pair per
currently-unexposed field group:

- `/api/config/ocode/permissions`
- `/api/config/ocode/security` (redaction)
- `/api/config/ocode/tui`
- `/api/config/ocode/discovery`
- `/api/config/ocode/imagegen`
- `/api/config/ocode/paths` (`ExtraAllowedPaths`)
- `/api/config/ocode/timings` (`RecapTimeoutSeconds`, `UndoMaxAgeDelta`)

Each follows the existing handler pattern: load `OcodeConfig` via the
current load helper, mutate the relevant field(s) on PUT, save via the
existing save path, return the section on GET. `opencode.json`-backed
settings (MCP, legacy plugins) keep using their existing handlers —
already REST-exposed today — just re-surfaced under the new page's
"opencode" nav group instead of the sidebar.

### Data flow

Standard load-on-mount / edit / PUT-on-save per form, matching the
existing dialog patterns already in the codebase. No new client-side state
management library needed.

## 4. Error handling

- PUT validation errors (e.g. malformed permission prefix, invalid path)
  surface as inline form errors, consistent with existing dialog error
  handling in `PluginsPanel.tsx`.
- GET failures (e.g. corrupt `ocodeconfig.json`) show a section-level error
  state in the right pane rather than blocking the whole page — other
  categories remain usable.

## 5. Testing

- Backend: table-driven handler tests per new endpoint (get/put roundtrip,
  validation, matching existing test patterns for the model/theme
  handlers).
- Frontend: no existing frontend test suite was found in `web/`; skip
  automated frontend tests unless one exists at implementation time.
  Manually verify via the `run` skill (launch app, navigate to
  Configuration, confirm both nav groups render and save round-trips)
  after implementation.

## 6. Out of scope

- Moving `ocodeconfig.json`'s on-disk path out of the opencode config
  directory.
- Any change to opencode's own file formats or ownership of
  `opencode/model.json`.
- New settings fields not already present in `OcodeConfig` or
  `opencode.json`.

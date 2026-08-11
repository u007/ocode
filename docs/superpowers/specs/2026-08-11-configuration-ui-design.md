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

The app already has a **global top-level tab bar**: `TopTabs.tsx`
(`web/src/components/Layout/TopTabs.tsx:12-18`) renders a fixed
`mainTabs` array (Sessions/Files/Git/Cron/Assets), mounted once in
`App.tsx:339`, with active-tab state in a single `App`-level `useState`
(`activeView`, `App.tsx:108`) — one instance per window, not per session.
This is distinct from **per-session sub-tabs** (`SessionSubTabs.tsx`:
Chat/Agents/Changes/Logs/Status/Terminal), whose active tab lives on each
session's `Tab` object in `projectStore.tsx` (`activeSubTab` field,
`SET_TAB_SUB_TAB` action). The `2026-07-24-web-cron-parity-design.md` spec
added Cron this same way — precedent for adding **Settings** as a sixth
global top-level tab rather than a bespoke page/view-switch.

The backend already maintains two distinct config sources, kept separate in
code comments (`internal/config/config.go:355-357`, `529-531`, `697-699`;
`internal/config/ocodeconfig.go:133`, `574-576`; `internal/config/state.go`):

- **`ocodeconfig.json` / `OcodeConfig` struct**
  (`internal/config/ocodeconfig.go:124-200`) — ocode's own settings, full
  field list in section 3 below.
- **`opencode.json`** (`internal/config/config.go`) — a different tool's
  config format ocode reads/writes for compatibility: MCP server
  definitions (`"mcp"`) and a legacy `"plugins"` key (intentionally *not*
  persisted by ocode on save — see `config.go:529-531`). opencode also owns
  `${XDG_STATE_HOME}/opencode/model.json` (recent/favorite model
  selections).

Confirmed with user: **out of scope** — moving `ocodeconfig.json` off its
current path (`~/.config/opencode/ocodeconfig.json`, nested inside
opencode's directory). This spec addresses the UI/API mixing only, not
on-disk file layout.

## 1. User-facing summary

A new **Settings** tab, added to `TopTabs.tsx`'s `mainTabs` array alongside
Sessions/Files/Git/Cron/Assets, reachable via:

- Clicking the Settings tab (web and desktop — same shared UI).
- A native **"Settings…"** item in the desktop app menu
  (`cmd/ocode-desktop/main.go` `buildAppMenu`), which emits an event
  consumed by the webview to set `activeView = 'settings'` — same
  event-bridge pattern already used for `confirmQuit`.

The tab has a left nav of category groups and a right pane showing the
selected group's form, under two top-level headings, **ocode** and
**opencode**. Section 3 gives the exhaustive field-to-group-to-endpoint
mapping; summarized here:

**ocode** — Model Defaults & Recap, Commit Message, Compact, Advisor,
Permissions, Security & Redaction, Terminal, OCR, Discovery, TUI, Editor
Mode, Image Generation, Paths & Uploads, Limits, Features, Plugins & Local
Models.

**opencode** — MCP Servers (editable, existing handlers), Legacy Plugins
Key (read-only — matches ocode's existing intentional non-persistence),
Model Selection State (read-only view of `opencode/model.json`).

Sidebar sections that are session-workflow, not settings — `agent`, `git`,
`context`, `lsp`, `files`, `todo` — are unchanged and stay in
`CoworkSidebar.tsx`. The `models` / `theme` / `tools` (MCP) / `paths`
sidebar sections and the standalone `PluginsPanel.tsx` dialog are removed;
their functionality moves into the Settings tab.

**Explicitly out of scope / unchanged:**
- The primary chat model picker (`ModelDialog.tsx`, `openModelDialog` in
  `App.tsx`) — this picks the *live agent's* current model, which is
  runtime/session state, not a field on `OcodeConfig` (confirmed: no
  `Model` field exists on the struct). It stays exactly where it is today;
  Settings' "Model Defaults" group only covers `SmallModel`/`RecapModel`
  persisted defaults.
- `MonacoSettingsPanel.tsx` (theme/font/tab-size/word-wrap for the file
  editor) — a different concern from `OcodeConfig.Editor`/`EditorMode`/
  `IDEMode` (which control editor *mode selection*, not Monaco's visual
  prefs). Monaco's panel is untouched.
- `Advisor.Enabled` persistence — today's `/api/config/advisor-enabled`
  endpoint is explicitly documented as runtime-only and does not persist to
  `OcodeConfig.Advisor.Enabled` on disk. This spec preserves that behavior;
  the Settings Advisor group's Enabled toggle remains a runtime/session
  toggle, not a persisted setting, matching current semantics.
- `OcodeConfig.Extra` (`map[string]json.RawMessage`, unknown-key
  passthrough) — never rendered in the UI; preserved as opaque data on
  every save so unrecognized keys survive round-trips.
- TUI settings (`OcodeConfig.TUI`) — theme/mouse/scroll/keybind config only
  ever *read* by the TUI, but it's plain JSON in the same file ocode
  already lets you edit from any surface. Decision: fully editable from
  Settings, not read-only, since a web/desktop user may also use the TUI
  and wants to configure it without touching JSON by hand.

## 2. Decisions (confirmed with user)

| Topic | Decision |
|-------|----------|
| Backend scope | Expose every currently-hidden `OcodeConfig` field via new REST endpoints (or by extending an existing endpoint's response/request shape), not just what's already wired up — see section 3 for the exhaustive mapping |
| UI shape | New top-level **Settings** tab in the existing global `TopTabs` bar, matching the precedent set by the Cron tab — not a bespoke view-switch or a modal |
| Entry points | Both the Settings tab and a native desktop "Settings…" menu item, which activates the same tab |
| Existing sidebar sections | Settings-like sections (models, theme, MCP/opencode tools, plugins, paths) move into the new tab; workflow sections (agent, git, context, lsp, files, todo) stay in the sidebar |
| ocode vs opencode grouping | Two explicit top-level nav headings in the new tab, mirroring the separation already enforced in Go code comments |
| `ocodeconfig.json` file path | Left unchanged — out of scope for this spec |
| Chat model picker, Monaco editor prefs, `Advisor.Enabled` persistence, `Extra` passthrough | Explicitly out of scope / unchanged — see callouts in section 1 |
| TUI config editability | Fully editable from Settings (it's one JSON file both surfaces share), not read-only |

## 3. Field → group → endpoint mapping (exhaustive)

Every field on `OcodeConfig` (`internal/config/ocodeconfig.go:124-200`),
assigned to exactly one Settings nav group and one endpoint. "existing"
means the route exists today unchanged; "extend" means the route exists
today but its request/response shape must grow to cover more of the
field/subfields; "new" means no route exists today.

| Nav group | Field(s) | Endpoint | Status |
|---|---|---|---|
| Model Defaults & Recap | `SmallModel`, `SmallModelEnabled` | `GET/PUT /api/config/small-model` | existing |
| Model Defaults & Recap | `RecapModel`, `RecapModelEnabled`, `RecapTimeoutSeconds` | `GET/PUT /api/config/ocode/recap` | new |
| Commit Message | `CommitMsgModel`, `CommitMsgPrompt` | `GET/PUT /api/config/ocode/commit-msg` | new |
| Compact | `Compact` (`CompactConfig`, all subfields) | `GET/PUT /api/config/ocode/compact` | new |
| Advisor | `Advisor.Model` | `GET/PUT /api/config/advisor` | extend (request/response grows to cover the fields below) |
| Advisor | `Advisor.Provider`, `Advisor.ClaudeCode`, `Advisor.Checkpoints` | `GET/PUT /api/config/advisor` | extend |
| Advisor | `Advisor.Enabled` | `GET/PUT /api/config/advisor-enabled` | existing, unchanged (runtime-only, not persisted — see out-of-scope callout) |
| Permissions | `Permissions.Tools` | `GET /api/permissions`, `POST /api/permissions` | existing |
| Permissions | `Permissions.Mode` | `GET/PUT /api/permissions/yolo` | existing |
| Permissions | `Permissions.Auto` (`AutoPermissionConfig`: grants/prompt/context limits) | `GET/PUT /api/config/ocode/permissions-auto` | new |
| Security & Redaction | `Security.Redaction.Enabled`, `.Mode`, `.Model` | `GET/PUT /api/config/mask`, `/api/config/mask/enabled`, `/mode`, `/model` | existing |
| Security & Redaction | `Security.Redaction.BaseURL`, `.FailMode`, `.AllowRemoteTier2`, `.SkipLLMIfClean`, `.CustomWords` | `GET/PUT /api/config/mask` | extend (full object, not just Enabled/Mode/Model) |
| Terminal | `TerminalScrollbackLines` | `GET/PUT /api/config/terminal` | existing |
| OCR | `Ocr` (`ocr.OcrConfig`, full) | `GET/PUT /api/config/ocr`, `/ocr-enabled`, `/ocr-model` | existing |
| Discovery | `Discovery` (`DiscoveryConfig`, all subfields) | `GET/PUT /api/config/ocode/discovery` | new |
| TUI | `TUI` (`TUIConfig`, all subfields) | `GET/PUT /api/config/ocode/tui` | new |
| Editor Mode | `Editor`, `EditorMode`, `IDEMode` | `GET/PUT /api/config/ocode/editor` | new |
| Image Generation | `ImageGen` (`ImageGenConfig`) | `GET/PUT /api/config/ocode/imagegen` | new |
| Paths & Uploads | `ExtraAllowedPaths`, `UploadDir` | `GET/PUT /api/config/ocode/paths` | new |
| Limits | `MaxSteps`, `MaxImageDim`, `MaxConcurrentAgents`, `UndoMaxAgeDelta` | `GET/PUT /api/config/ocode/limits` | new |
| Features | `MemoryEnabled`, `DocPromptEnabled` | `GET/PUT /api/config/ocode/features` | new |
| Plugins & Local Models | `Plugins` (`PluginsConfig`, enable/disable) | `GET/PUT /api/config/ocode/plugins-enabled` | new |
| Plugins & Local Models | `ExternalPlugins` | `GET/PUT/POST/DELETE /api/plugins...` | existing |
| Plugins & Local Models | `LocalModels` | `GET/PUT /api/config/ocode/local-models` | new |
| *(not rendered)* | `Extra` | none | opaque passthrough, preserved on every save — see out-of-scope callout |

## 4. Architecture

### Frontend

- `TopTabs.tsx`: add `{ id: 'settings', label: 'Settings' }` to `mainTabs`
  (`TopTabs.tsx:12-18`), rendered like the other five tabs.
- `App.tsx`: `activeView` (`App.tsx:108`) already supports arbitrary tab
  IDs; add the `'settings'` case to whatever switch/conditional renders
  the active tab's content, pointing at a new `SettingsView` component.
- New `web/src/components/Settings/SettingsView.tsx`: left nav of the two
  headed groups (ocode/opencode) from section 3, right pane renders the
  selected group's form component from `web/src/components/Settings/`.
- Each group form: loads its section's current value on mount via
  `api/client.ts`, edits with existing shadcn/ui form primitives (already
  used in `PluginsPanel.tsx`/`ModelDialog.tsx` for visual consistency),
  saves via PUT with toast feedback matching current dialog behavior.
- Relocated existing components (`PluginsPanel.tsx`'s external-plugin
  management, the sidebar's MCP/tools list, theme selector, paths editor)
  are refactored into `Settings/` form components rather than duplicated;
  their trigger buttons/sections are removed from `CoworkSidebar.tsx`.

### Desktop native menu

`cmd/ocode-desktop/main.go` `buildAppMenu`: add a "Settings…" menu item
(macOS: in the app menu near About; Windows/Linux: under File) that emits
a Wails event consumed by the webview to set `activeView = 'settings'`,
following the existing `confirmQuit` event-wiring pattern.

### Backend

New/extended endpoints in `internal/server/handler_config.go`, registered
in `internal/server/server.go` alongside existing config routes, per the
"new"/"extend" rows in section 3. Each new endpoint follows the existing
handler pattern: load `OcodeConfig` via the current load helper, mutate
the relevant field(s) on PUT, save via the existing save path, return the
section on GET. Each "extend" endpoint keeps its existing route/URL and
grows its request/response struct to include the additional subfields
listed in section 3, staying backward compatible with any existing caller
that only sends/reads the fields it already knows about.

`opencode.json`-backed settings (MCP servers) keep using their existing
handlers, re-surfaced under the Settings tab's "opencode" nav group. The
legacy `plugins` key and `opencode/model.json` are rendered read-only,
matching ocode's existing intentional non-persistence of those values.

### Data flow

Standard load-on-mount / edit / PUT-on-save per form, matching existing
dialog patterns already in the codebase. No new client-side state
management library needed — `activeView` reuses the existing top-level tab
mechanism.

## 5. Error handling

- PUT validation errors (e.g. malformed permission prefix, invalid path)
  surface as inline form errors, consistent with existing dialog error
  handling in `PluginsPanel.tsx`.
- GET failures (e.g. corrupt `ocodeconfig.json`) show a section-level error
  state in the right pane rather than blocking the whole tab — other
  categories remain usable.
- Extended endpoints (Advisor, Security/Redaction) must preserve any
  subfield not included in a given PUT payload rather than zeroing it, so
  older/partial clients can't silently wipe fields they don't know about.

## 6. Testing

- Backend: table-driven handler tests per new/extended endpoint (get/put
  roundtrip, validation, partial-update preservation for extended
  endpoints), matching existing test patterns for the model/theme
  handlers.
- Frontend: no existing frontend test suite was found in `web/`; skip
  automated frontend tests unless one exists at implementation time.
  Manually verify via the `run` skill (launch app, open the Settings tab,
  confirm both nav groups render, exercise a save round-trip in at least
  one new and one extended endpoint) after implementation.

## 7. Out of scope

- Moving `ocodeconfig.json`'s on-disk path out of the opencode config
  directory.
- Any change to opencode's own file formats or ownership of
  `opencode/model.json`, or persisting its legacy `plugins` key.
- The primary chat model picker (`ModelDialog.tsx`) and
  `MonacoSettingsPanel.tsx` — both unrelated to `OcodeConfig` fields.
- Persisting `Advisor.Enabled` (stays runtime-only, matching current
  behavior).
- New settings fields not already present in `OcodeConfig` or
  `opencode.json`.

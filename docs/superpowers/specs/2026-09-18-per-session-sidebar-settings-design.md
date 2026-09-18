---
type: Design
title: Per-Session Sidebar Models and Toggles
description: Design spec for making every chat-session sidebar model pick and on/off toggle per session (durable, server-side, full TUI parity), instead of one process-global config value shared by every chat.
tags:
  - design-spec
  - session
  - config
  - sidebar
  - models
  - tui
  - web
timestamp: 2026-09-18T12:34:00Z
---
# Per-Session Sidebar Models and Toggles — Design Spec

Date: 2026-09-18
Status: draft

## Problem

The chat sidebar exposes a model picker and on/off toggles for several helper
models (small, explorer, context, advisor, auto-continue judge, recap,
permission judge) plus a discovery block. Today every one of those writes a
**single process-global value** into `~/.config/opencode/ocodeconfig.json`
via `PUT /api/config/ocode/*`, so changing any of them in one chat silently
changes every other open chat, the TUI, and future chats.

Only two controls are already per chat session:

- the **main model** (`metadata["model"]` + `Handler.effectiveSessionModel`)
- the **permission mode** (`metadata["permission_mode"]` +
  `sessionPermissionModeForDir`)

Users want each chat session to own its own helper models and toggles so that,
for example, one chat can run with auto-continue + a cheap small model while
another keeps them off.

## Goals

1. Every sidebar helper-model pick and on/off toggle is scoped to the
   **chat session** that changed it; other sessions are untouched.
2. The setting is **durable**: it survives reload, resume, and server restart,
   matching the per-session main model.
3. New sessions start from **global config defaults**; an explicit
   **"Set as global default"** (promote) action copies a session's settings
   back to global config.
4. **Full TUI parity**: TUI sidebar rows and slash commands read and write the
   active session's settings, not global config.
5. Sessions that never touch a per-session control behave **exactly** as they
   do today (global config), with no regressions.

## Non-goals

- Changing how the main model or permission mode are stored (they keep their
  existing metadata keys; no migration).
- Per-project (as opposed to per-session) scoping.
- A new settings UI for global defaults; the existing Settings tab remains the
  global editor.
- Reworking the agent package's config consumption (`a.config.Ocode.*` reads
  stay as they are).

## Decisions

- **Scope:** all auxiliary models + all on/off toggles + the discovery block.
- **Persistence:** durable per session, server-side.
- **New-session defaults:** global config, with an explicit promote action.
- **Surfaces:** full TUI parity, in addition to web/desktop.
- **Architecture (Approach A):** persist overrides in session transcript
  metadata; resolve them onto a **session-scoped copy** of the config at agent
  build time; live-apply via the existing **next-turn rebuild** path, not
  per-field runtime atomics.

## Design

### Data model

New type in `internal/config/session_settings.go`:

```go
// SessionSettings holds per-chat-session overrides for the sidebar's helper
// models, on/off gates, and the discovery block. Every field is a pointer so
// "unset" (inherit global config) is distinguishable from an explicit
// false/empty value (the user turned it off / cleared the model in this chat).
type SessionSettings struct {
    SmallModel           *string `json:"small_model,omitempty"`
    SmallModelEnabled    *bool   `json:"small_model_enabled,omitempty"`
    ExplorerModel        *string `json:"explorer_model,omitempty"`
    ExplorerModelEnabled *bool   `json:"explorer_model_enabled,omitempty"`
    ContextModel         *string `json:"context_model,omitempty"`
    ContextModelEnabled  *bool   `json:"context_model_enabled,omitempty"`
    AdvisorModel         *string `json:"advisor_model,omitempty"`
    AdvisorEnabled       *bool   `json:"advisor_enabled,omitempty"`
    AutoContinueModel    *string `json:"auto_continue_model,omitempty"`
    AutoContinueEnabled  *bool   `json:"auto_continue_enabled,omitempty"`
    RecapModel           *string `json:"recap_model,omitempty"`
    RecapModelEnabled    *bool   `json:"recap_model_enabled,omitempty"`
    PermissionModel      *string `json:"permission_model,omitempty"`
    PermissionAutoAllow  *bool   `json:"permission_auto_allow,omitempty"`
    Discovery            *DiscoveryConfig `json:"discovery,omitempty"`
}

// Empty reports whether no override is set (the session follows global config).
func (s *SessionSettings) Empty() bool

// Revision returns a stable hash of the canonical JSON, used to detect a
// change and trigger exactly one rebuild.
func (s *SessionSettings) Revision() string

// Apply returns a copy of base with the non-nil overrides applied. The
// returned config is independent: shared pointer/map/slice sub-fields that are
// written (Permissions.Auto, Discovery) are copied first so the process-wide
// config is never mutated.
func (s *SessionSettings) Apply(base *Config) *Config
```

`SessionSettings` and `Apply` live in `internal/config` and depend on nothing
else, so there is no import cycle. The session-aware loader lives in
`internal/server` (which already imports `internal/session`).

### Storage

- Key: `session_settings` in the session transcript metadata map
  (`map[string]any`), written with the metadata-only
  `session.UpdateMetadataForDir(projectRoot, id, mutate)` — the same durable
  store as `model` and `permission_mode`.
- The value is the JSON object for `SessionSettings`. An empty/absent value
  means "inherit global".
- Main model and permission mode are **not** folded into this struct; they keep
  their own keys to avoid a migration.

### Resolution and application

- `sessionSettingsForDir(projectRoot, id) (*config.SessionSettings, error)` in
  `internal/server` — loads and decodes, returning an empty struct (not an
  error) when absent.
- `buildAgentSession` (`internal/server/agent_session.go:92`), after resolving
  `effCfg` from `h.cfg` or the profile config, calls `effCfg = settings.Apply(effCfg)`.
  The agent therefore receives its own config instance and every existing
  `a.config.Ocode.*` read site sees the session's values with no changes.
- **Advisor special case:** the server gates the advisor tool through the
  handler field `h.advisorEnabled`, not through the agent's config
  (`agent_session.go:54`). After construction, when
  `settings.AdvisorEnabled != nil`, call `ag.SetAdvisorEnabled(*settings.AdvisorEnabled)`.
- The returned `agentSession` records `settingsRev` (the `Revision()` used at
  build time).

### Live application

- `reconcileProfileAgent` (`internal/server/agent_session.go:257`) already
  rebuilds the resident agent at a turn boundary when the model, profile, or
  credential version changed. Add a `settingsRev` comparison to that same
  condition. A sidebar change therefore lands on the **next turn** — identical
  semantics to a per-session main-model change today, and no per-field runtime
  atomics are needed for server-owned sessions.
- This path is a no-op while a turn is active, so an in-flight turn finishes on
  the previous settings.

### Per-session status snapshot

- Add `applySessionSettingsFields(&snap, id)` and call it from the per-session
  snapshot builders (`buildSessionStatusSnapshot`, `pushSessionStatusSnapshot`).
  It sets the existing `TUIStatus` fields (`SmallModel`/`SmallModelOn`,
  `ExplorerModel`/`On`, `ContextAgentModel`/`On`, `AdvisorModel`/`AdvisorEnabled`,
  `AutoContinueModel`/`On`, `RecapModel`/`On`, `PermissionModel`,
  `PermissionAutoAllow`) from the resolved settings.
- The web sidebar already reads these from the per-session `tuiStatus`, so the
  read path needs no change for those rows.
- Discovery is not in `TUIStatus`; it is served by the settings GET below so the
  status payload does not grow a large structured block.

### API surface

- `GET /api/sessions/:id/settings` — stored overrides plus resolved effective
  values, including the discovery block.
- `PATCH /api/sessions/:id/settings` — partial update of any subset of fields.
  Validates model ids the same way `HandleSetSessionModel` does
  (`agent.NewClient` probe), persists metadata only, and pushes the
  per-session status snapshot.
- `POST /api/sessions/:id/settings/promote` — writes the session's resolved
  settings to global config using the existing `config.Save*` functions.
- Existing global `/api/config/ocode/*` endpoints remain, unchanged, for the
  Settings tab and global defaults.
- **Bridged TUI session:** the TUI owns that session's agent and persistence.
  The server forwards a settings change to the TUI (new RC settings channel),
  which applies it to its live agent, persists the metadata, and rebroadcasts
  its status. Where the in-process `rc.Agent()` is reachable, the existing
  runtime setters (`SetSmallModelRuntimeEnabled/Model`,
  `SetAutoPermissionModel`, `SetAdvisorEnabled`) are the fallback for the
  fields that have them.

### Web / desktop sidebar

- All per-session rows and pickers call the per-session endpoints with the
  active `sessionId` instead of `api.set*` globals.
- Add a "Set as global default" action in the model section (promote).
- Draft (`new-*`) tabs hold changes locally until the session exists, then
  PATCH on creation, mirroring the existing draft-model behavior.
- The global-value fallbacks remain only for the pre-session/draft case.

### TUI parity

- Sidebar rows render the active session's resolved settings.
- `/small`, `/advisor`, `/autocontinue`, `/explorer`, `/context`, `/recap`, and
  the discovery commands write the active session's overrides and apply them to
  the live TUI agent.
- Global Settings editing remains global; add a promote action/command.

## Error handling and concurrency

- Metadata-only writes to avoid the documented load→save conflict with rows the
  loader filters out.
- Session resolution and transcript loading happen outside `h.mu`; only the
  in-memory config read is under the lock, matching existing patterns.
- Invalid input (unresolvable model id, malformed body) returns 400 and changes
  no state.
- **Cron/scheduler agents must not inherit session overrides** — they have no
  session and use global config, mirroring `resolveCronPermissionMode`.
- `Apply` must never mutate the shared global config; copying
  `Ocode.Permissions.Auto` and `Discovery` before writing is load-bearing and
  must be covered by a test.

## Risks and mitigations

| Risk | Mitigation |
| --- | --- |
| Session-scoped config copy accidentally shares a pointer with global config and leaks a change | Dedicated unit test asserting global config is byte-identical after `Apply`; copy pointer/slice sub-fields first |
| Missing a consumer site that reads global config instead of the session copy | Approach A gives the agent its own config instance, so consumers need no changes; verify with a build-integration test |
| Bridged TUI session behaves differently from server-owned sessions | Explicit RC forwarding + TUI handler; dedicated test for the bridged path |
| Existing global-only behavior regresses for users who never use per-session controls | Empty `SessionSettings` must make `Apply` a no-op and status must equal today's output |
| Discovery block is large and easy to corrupt on partial update | PATCH treats `discovery` as replace-whole-block, matching the existing global PUT semantics |

## Testing

- **Go unit:** `SessionSettings.Apply` isolation (global untouched, all fields);
  metadata round-trip and `Empty`/`Revision`; reconcile rebuild on settings
  change and no-op when unchanged; per-session status snapshot values;
  endpoint validation and 4xx paths; promote writes global config; bridged
  forwarding.
- **Web:** `CoworkSidebar` tests for per-session read/write wiring and the
  promote action; update existing tests that assumed global writes.
- **TUI:** row rendering and command tests for per-session writes; global
  editing still writes global config.
- **Baseline:** run the suite against a pristine worktree per repo convention to
  distinguish pre-existing environment failures.

## Phasing

1. **Backend foundation** — `SessionSettings` type + storage + resolution +
   `buildAgentSession`/reconcile integration + per-session status + endpoints
   (models and gates). No UI behavior change yet.
2. **Web/desktop sidebar** — route rows and pickers to per-session endpoints;
   add promote.
3. **Discovery block** — extend the store and sidebar, and wire per-session
   discovery into the agent's discovery paths.
4. **TUI parity** — rows, slash commands, and bridged forwarding.

Each phase leaves the global path intact for sessions with no overrides.

# Desktop Per-Window Sparse Profiles (Base + Overriding Deltas)

- **Status:** draft — needs user review
- **Date:** 2026-08-20
- **Scope:** desktop app (`cmd/ocode-desktop` + `internal/desktop` + `web/src`) — also CLI alias parity
- **Related:** config handling in `internal/config/ocodeconfig.go` and `internal/config/config.go`, auth handling in `internal/auth/store.go` and `internal/auth/providers.go`, desktop boot in `internal/desktop/boot.go`, window lifecycle in `cmd/ocode-desktop/main.go`, web settings in `web/src/components/Settings/SettingsPanel.tsx`

## 1. Goal

Allow seamless switching of multiple `opencode` (and other provider) API keys and full config overrides on the desktop, without touching the upstream `opencode` auth contract.

- Base + sparse overlay: One base config. Each profile stores only overridden fields. Unset fields inherit base. Default means base alone, no profile.
- Per-window active profile, instant hot-swap in that window only, always visible.
- Separate auth sidecar so opencode never sees profile keys.
- Shell alias parity via environment override already used in `internal/auth/providers.go` precedence: OPENCODE_AUTH_TOKEN and provider env vars remain highest priority.
- Desktop UI must show overridden vs inherited and allow one-click reset to base, plus setup/rename/remove with confirmation.

## 2. Non-goals

- Profile inheritance chains beyond base → profile.
- Per-project auto-switching on cwd change.
- Encryption beyond 0600 perms.
- Cross-device sync of profiles via existing sync layer.

## 3. Architecture Overview

Files involved:

- `internal/config/ocodeconfig.go` for OcodeConfig and profile map
- `internal/auth/store.go` and `internal/auth/providers.go` for credential resolution
- `internal/paths` for directory resolution (AppName remains opencode for compat; new ocode-specific sidecar lives under a separate ocode data dir via new helper)
- `internal/desktop/boot.go` for server boot and config loading
- `cmd/ocode-desktop` for window creation and native menu
- `web/src` for header pill and settings UI

Effective config per window is computed at read time as deep merge of base and named sparse delta. Effective credential per window is looked up from sidecar first, falling back to shared auth file. Ephemeral environment variable OCODE_PROFILE wins for that process.

## 4. Data Model

- OcodeConfig gains a profiles map keyed by profile name, holding a sparse delta of the same top-level fields as base. The map never nests profiles inside itself.
- Auth sidecar file lives outside the opencode data dir so opencode never parses it. It is a map from profile name to map from provider id to credential. Permissions 0600, atomic write.
- Window-state file and localStorage keep per-window active profile identifier; empty string means Default (base only). Frontend paints from localStorage before file read for speed.

Files and names only, no inline code: `internal/config/ocodeconfig.go`, `internal/auth/store.go`, `internal/paths`, `internal/desktop`, `cmd/ocode-desktop/main.go`, `web/src/components/Settings`.

## 5. Merge and Reset Semantics

- Present in delta means overridden; absent means inherited. UI distinguishes the two states.
- Deep merge for map-typed top levels keyed by provider or server name; scalar fields replace only if delta has a value.
- Reset to base removes the field or map entry from the delta and rewrites files atomically, then the UI badge clears.
- Deleting all fields leaves an empty delta; the profile still exists until explicit Remove with confirmation.

## 6. Desktop UI

Header pill per window shows Base or named profile with count of overrides, opening a popover listing Default plus each profile and a New profile entry. One click switches the window via a window-scoped API.

Settings diff shows per-field overridden indicators and Reset to base links while a profile is active, plus a banner noting how many overrides are active.

Manage profiles lives in Settings profiles card with actions for create, rename, and delete. Create asks for a name, display name, and optional copy-from base or existing profile plus inline credential setup with test action. Rename validates uniqueness and migrates references in config, sidecar, and window-state atomically. Delete requires confirmation describing what will be removed and is blocked while any window is actively on that profile until switched to Base.

Additional aliases call the same window-scoped endpoint: palette action and app menu.

## 7. Backend Sync

Desktop webview calls a window-scoped endpoint to update active profile. Backend validates, file-locks and rewrites window-state, reloads effective config via the existing config load path in `internal/config` and `internal/desktop/boot` helpers, and rebuilds per-window agent client and related managers for that window only.

A file watcher in `internal/desktop` and the existing paths watch mechanism broadcasts a window profile changed event via Wails events. Webviews subscribe and re-render header and settings without reload or restart. CLI alias via OCODE_PROFILE remains ephemeral with no file write but uses the same resolver.

Mid-stream turns finish on the old profile; the next turn uses the new profile.

## 8. CLI and Alias Parity

Environment variable or command flag sets an ephemeral active profile for that process, ranking above window-state. This preserves existing alias patterns for shell and desktop launched from shell. Env still wins over file, matching the precedence already in `internal/auth/providers.go`.

## 9. Alternatives Considered

- Profile directories with full file copies. Rejected due to duplication and difficulty showing sparse diff and reset.
- Env-only bundles. Rejected because it only covers keys, not full config.
- Reusing the shared auth file. Rejected per user constraint since it breaks opencode compatibility.

## 10. Migration and Testing

Old installs have no profiles map; Default is the current behavior. First profile creation populates the maps; installs continue reading the shared auth file for base keys.

Testing covers deep merge prefers sparse, sidecar key lookup prefers profile, window-state locking, name validation, header badge counts, reset removing entries, delete blocking when active, and rename migrating refs.


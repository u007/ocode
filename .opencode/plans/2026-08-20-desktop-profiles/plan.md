# Plan: Desktop Per-Window Sparse Profiles (Base + Deltas)

## Context
Spec approved at `docs/superpowers/specs/2026-08-20-desktop-profiles-sparse-design.md`. Need to allow multiple opencode API keys and full config overrides via profiles without touching upstream `~/.local/share/opencode/auth.json`.

Base truth: `~/.config/ocode/ocodeconfig.json` and `~/.local/share/opencode/auth.json`.
Profile overlay: sparse map in `ocodeconfig.json#profiles` + new sidecar `~/.local/share/ocode/auth.profiles.json` (0600). Per-window activeProfile in `window-state.json` + localStorage. Default = base only. Env `OCODE_PROFILE` ephemeral override for `alias ocode2`.

Desktop must have header pill per-window + Settings diff badges + Reset to base + create/rename/remove with confirmation + palette/menu alias. Backend hot-reloads per window without restart.

## Goals
- Sparse overlay: missing fields inherit base.
- Per-window instant switch, visible, no restart.
- Separate auth sidecar keeps opencode compat.
- Overridden indicators + one-click Reset.
- Full manager UI on desktop.

## Non-Goals
- No profile inheritance chains.
- No per-project auto-switch.
- No cross-device sync.

## Approach
Implement slices in dependency order: config/auth storage → path helper → resolution → desktop window-state + file watcher → server window-scoped API → web header pill → web Settings diff → CLI flag. Keep implementation skill boundaries small.

## Files Touched
- `internal/config/ocodeconfig.go` for profiles map and effective getter
- `internal/auth/store.go` and `internal/auth/providers.go` for sidecar read/write and window-scoped resolution
- `internal/paths` for new ocode-sidecar dir helper (AppName collision avoided)
- `internal/desktop/boot.go` and `internal/desktop/watch.go` for per-window boot and file watcher
- `cmd/ocode-desktop/main.go` for native window menu and Wails events
- `internal/server/handler_config.go` or new `handler_profiles.go` for window-scoped profile endpoints
- `web/src/components/Settings/SettingsPanel.tsx` and new profile components for diff badges and manager
- `web/src/components/Layout` for header pill
- `internal/runcli/run.go` or `internal/cli` for --profile flag / OCODE_PROFILE handling

## Implementation Steps

### Slice 1: Config Sparse Map
- Add profiles map to OcodeConfig; add effective getter computed as deep merge of base and named delta.
- Tests: merge prefers sparse, missing inherits, nested maps merge by key.

### Slice 2: Auth Sidecar
- Add sidecar path helper not using opencode AppName.
- Add read/write with atomic tmp→rename 0600, map from profile to provider credentials.
- Add window-scoped credential lookup preferring sidecar then falling back to shared auth file.
- Tests: sidecar 0600, fallback, missing profile returns base.

### Slice 3: Window State Storage
- Add window-state file read/write with advisory lock, plus localStorage fast path in web.
- Tests: empty means Default, concurrent writes serialized.

### Slice 4: Resolution Wiring
- Wire OCODE_PROFILE env override before window-state lookup in provider resolution path.
- Wire effective config and effective key into desktop boot and per-window agent client creation.
- Tests: env wins over file, per-window keys differ.

### Slice 5: Server Window-Scoped API
- Add endpoints for listing profiles, creating, renaming, deleting, and switching active profile per window.
- Validate names with allowed charset, atomic renames of config/sidecar/window-state, broadcast change event.
- Tests: validation, atomic migrate on rename, blocked delete when active.

### Slice 6: Desktop Native Integration
- Add header pill component showing Base vs named profile with override count.
- Add Wails menu and palette entries calling same API.
- Add file watcher broadcasting window.profileChanged via Wails events.
- Tests: pill renders Default, popover lists profiles, menu triggers API.

### Slice 7: Web Settings Diff
- Annotate each settings field with inherited vs overridden state and Reset to base link.
- Add Settings profiles card with New, Rename, Delete with confirmation dialog.
- Add inline opencode key setup with Test action.
- Tests: badge appears only when overridden, Reset removes delta entry, confirmation blocks when active.

### Slice 8: CLI Flag
- Add --profile flag mapping to OCODE_PROFILE for the process.
- Tests: flag and env both resolve ephemerally.

## Testing Strategy
- Unit: merge, sidecar lookup, window-state lock, name validation, header badge counts, reset atomicity.
- Integration: two windows switch isolation, mid-stream turn stays on old profile, rename migrates refs, delete blocked when active.
- No opencode auth file mutation assertions.

## Rollout
- Phase 1 slices 1-4 behind flag, no UI.
- Phase 2 slices 5-7 behind config flag, manual QA.
- Docs update: setup instructions for alias ocode2 pattern.

## Open Questions
- Sidecar exact path: ~/.local/share/ocode/ vs ~/.config/ocode/ — confirm with path helper owner.

---
type: Design
title: System Permissions Settings Section (macOS TCC + cross-platform)
description: Design spec for System Permissions settings section covering macOS TCC grants with cross-platform support, detection/request, API, startup reconcile, and web UI.
tags:
  - design-spec
  - permissions
  - tcc
  - macos
  - settings
  - sysperm
timestamp: 2026-09-18T02:36:01Z
---
# System Permissions Settings Section (macOS TCC + cross-platform) — Design Spec

Date: 2026-09-18
Status: draft

## Problem

macOS TCC grants (Full Disk Access, Files & Folders, Accessibility, Screen Recording, Automation) are keyed to the app's code signature. Every rebuild produces a new cdhash so macOS forgets the grant and ocode's file tree/terminal/computer-use fail until re-prompted. Users also lack a single place to see which OS permission areas ocode touches and to control them.

## Goals

1. A new "System Permissions" settings section in the shared web/desktop Settings UI, listing every OS permission area ocode may need.
2. Per-entry persistent on/off toggle in `ocodeconfig.json` (intent only; live status computed).
3. Turning an entry ON triggers that OS permission request; OFF records deny and stops auto-requesting (no `tccutil` reset).
4. On ocode-desktop launch (after UI is up), reconcile: for every Enabled+Supported+not-yet-granted entry, fire the OS request once, batched.
5. Cross-platform: works/builds on darwin, windows, linux. Non-darwin entries are informational (Supported:false, "no explicit grant required") and the section still renders.

## Non-goals

- `tccutil` reset / programmatic revocation.
- Changing code-signing or `bundle-macos.sh`.
- Refactoring `internal/computer` to use the new package.
- TUI surface (settings UI is the shared SPA used by web and desktop).

## Design

**Approach A chosen.** New package `internal/sysperm` with a per-platform catalog + detect/request; new config sub-tree; three HTTP endpoints; new SPA settings group.

Rejected alternatives:
- **(B)** Generalizing `internal/computer/permissions.go` — conflates grants, forces `PermissionReport` shape.
- **(C)** UI-only toggles + deep links — no detection/request, fails goals 2-4.

## Data model

```go
type Entry struct {
    ID        string
    Label     string
    Detail    string
    Kind      string // "category" | "path"
    Platform  string // "darwin" | "windows" | "linux"
    Supported bool
    Status    string // "granted" | "denied" | "not_determined" | "unknown" | "not_required"
    Enabled   bool
    Path      string
    Source    string // "builtin" | "discovered" | "custom"
}
```

Persisted in `ocodeconfig.json` under `system_permissions.entries`, keyed by ID:

```json
{
  "system_permissions": {
    "entries": {
      "full-disk-access": { "enabled": true },
      "custom-desktop": { "enabled": true, "path": "/Users/jane/Desktop", "label": "My Desktop" }
    }
  }
}
```

Only intent + custom path/label are persisted. Live status is computed at read time.

### Darwin built-ins

- `full-disk-access`
- `files-desktop`
- `files-documents`
- `files-downloads`
- `files-icloud`
- `files-network-volumes`
- `files-removable-volumes`
- `accessibility`
- `screen-recording`
- `automation-system-events`

"Past accessed dirs" are derived automatically from saved projects (`projects.json`) and session project roots, surfaced as `path` entries pinned to their protected TCC root. Users may also add/remove custom paths.

### Cross-platform

Windows and Linux entries are informational only — `Supported: false`, labeled "no explicit grant required". The settings section still renders on these platforms.

## Detection/request (darwin)

Reuses the primitives already proven in `internal/computer/permissions_darwin.go`:

| Permission | Detection | Request |
|---|---|---|
| Accessibility | `osascript` JXA `AXIsProcessTrustedWithOptions` | Same JXA prompt |
| Screen Recording | `screencapture` probe (no scriptable grant check) | OS-level dialog |
| Automation | `osascript ... System Events` | System Events dialog |
| Files & Folders | Attempted read of protected TCC root | OS-level dialog |
| Full Disk Access | `TCC.db` readability | OS-level dialog |

**Detection caveat:** macOS cannot silently detect a denied Files & Folders grant without touching the protected path, so rows begin `unknown`/`not_determined` until probed. The UI must label this honestly.

Requests are best-effort and never fatal.

Windows/Linux: no explicit grant required, informational entries only.

## API

Registered next to the existing computer-use endpoints:

| Method | Path | Body | Response |
|---|---|---|---|
| `GET` | `/api/config/system-permissions` | — | `{platform, entries: [...]}` with live status |
| `PUT` | `/api/config/system-permissions` | `{id, enabled, path?, label?}` | updated entry |
| `POST` | `/api/config/system-permissions/request` | `{id}` (one) or `{}` (all enabled) | updated entries list |

## Startup reconcile

`cmd/ocode-desktop/main.go` hooks the window-ready event, runs one batched pass over Enabled+Supported+not-granted entries, and raises a notification if any action was needed. Never fires in CLI/server/headless/tests.

## Web UI

New `SystemPermissionsForm.tsx` as its own `SettingsPanel` group:

- **Group id:** `system-permissions`
- **Label:** "System Permissions"

Each row shows: label / detail / status badge / toggle / Request button.

Additional controls:
- Custom path add (reuses `DirectoryBrowser`) and remove.
- "Request all enabled" button.
- Explanatory note about detection limits and rebuild behavior.

## Tests

| Area | Coverage |
|---|---|
| `internal/sysperm` | Unit tests with stubbed command runner (no real OS prompt); config round-trip |
| Server handlers | Tests with stubbed requester (mirroring `requestComputerPermissions` pattern) |
| `SystemPermissionsForm.test.tsx` | Component tests with mocked `api` |

## Rollout

Additive config field, add-only routes, new settings group. No migrations required. Existing `ocodeconfig.json` files without `system_permissions` are unaffected — missing entries default to disabled/unknown.
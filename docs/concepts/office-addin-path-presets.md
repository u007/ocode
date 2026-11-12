---
type: Concept
title: Office Add-in Path Presets — Save-Time OS Access Request
description: Settings → Paths & Uploads offers per-OS Microsoft Office add-in folders as checkboxes over extra_allowed_paths, then asks the OS for access only for the folders newly granted on save; records the macOS container ("access data from other apps") gate and the Full Disk Access remedy.
resource: internal/paths/office_addins.go, internal/server/handler_office_addin_access.go, internal/server/handler_config.go, internal/sysperm/catalog_darwin.go, web/src/components/Settings/PathsForm.tsx, web/src/api/client.ts
tags:
  - settings
  - paths
  - office
  - permissions
  - macos
  - tcc
  - server
  - web
timestamp: 2026-11-09T09:41:30Z
---
# Office Add-in Path Presets — Save-Time OS Access Request

Settings → Paths & Uploads renders one checkbox per well-known Microsoft
Office add-in folder for the server's platform. Ticking one appends its
absolute path to `extra_allowed_paths`; there is still **no Office-specific
config key and no separate permission surface**. The presets are a convenience
over the existing allowlist (`OfficeAddinPath` doc comment,
`internal/paths/office_addins.go:9-16`).

What is new is the **save-time OS access request**: after the paths config is
persisted, the web asks the server to probe each folder that just became
granted, so the macOS "access data from other apps" consent dialog appears at
save time instead of surfacing later as a mysterious sideload failure.

## Presets

`paths.OfficeAddinPaths()` (`internal/paths/office_addins.go:49`) delegates to
the testable `officeAddinPathsFor(goos, home, appData)`
(`internal/paths/office_addins.go:56`):

| Platform | Presets |
|---|---|
| macOS | Five per-app `~/Library/Containers/<bundle>/Data/Documents/wef` folders: Excel (`com.microsoft.Excel`), Word (`com.microsoft.Word`), PowerPoint (`com.microsoft.Powerpoint`), Outlook (`com.microsoft.Outlook`), OneNote (`com.microsoft.onenote.mac`) — `:58-84` |
| Windows | `%APPDATA%\Microsoft\AddIns` ("Office Add-ins (classic)", app id `addins`) and `%APPDATA%\Microsoft\Word\STARTUP` (`word-startup`) — `:85-100` |
| other | none (`nil`), so the form falls back to manual entry — `:101-103` |

The Windows branch carries a note on each `AddIns` preset: web add-ins there are
sideloaded through a **trusted shared-folder catalog** (or the registry), not a
per-app `wef` cache, so the user enters that folder by hand.

`newOfficeAddinPath` (`internal/paths/office_addins.go:106`) fills two
distinct booleans:

- `Exists` (`:111`) — the folder is present *now*. Office creates a macOS
  `wef` folder on first sideload, so a missing folder is still grantable.
- `Available` (`:112`) — the **parent** directory exists. `tool.NormalizeRootPath`
  only resolves a missing leaf when its immediate parent exists, so a preset for
  an uninstalled app would look grantable but silently never reach the runtime
  allowlist.

`Allowed` and `AllowedPath` are filled by the server, not `internal/paths` (which
does not read config): `HandleGetPathsConfig` marks a preset allowed when its
normalized path matches an `extra_allowed_paths` entry, and `AllowedPath` returns
the **raw** matching entry (e.g. a `~/...` or trailing-slash form) so the form can
exclude it from the free-text field without duplicating it
(`internal/server/handler_config.go:1903-1927`).

## Endpoints

All three routes are registered behind `authMiddleware`
(`internal/server/server.go:631-633`):

| Route | Handler | Contract |
|---|---|---|
| `GET /api/config/ocode/paths` | `HandleGetPathsConfig` (`internal/server/handler_config.go:1894`) | `{extra_allowed_paths, upload_dir, office_addin_paths, platform}` (`:1929-1938`) |
| `PUT /api/config/ocode/paths` | `HandleSetPathsConfig` (`:1942`) | Body `{extra_allowed_paths, upload_dir}`; saves, syncs the live allowlist, returns both (`:1951-1967`) |
| `POST /api/config/ocode/paths/request` | `HandleRequestOfficeAddinAccess` (`internal/server/handler_office_addin_access.go:99`) | Body `{apps:[...]}`; returns `{platform, supported, results:[...]}` |

`platform` is `runtime.GOOS` — the server's OS, not the browser's, which is what
lets a remote project label the right file-manager action. `supported` is
`sysperm.PlatformSupported()` (`internal/sysperm/catalog_darwin.go:258` returns
true on macOS; `catalog_other.go` returns false).

The handler re-reads `h.officeAddinPresets()` when it runs
(`internal/server/handler_office_addin_access.go:119-121`), so `Exists` is
**live** at request time rather than the value cached when the page loaded. It
rejects invalid JSON and an empty `apps` list with 400 (`:104-116`), dedupes app
ids (`:124-130`), and rejects an unknown app id rather than ignoring it
(`:132-135`). The preset seam is a `Handler` field so tests can pin the list
without mutating a package var (`internal/server/handler.go:83`); the request
override for the probe is at `internal/server/handler.go:88`.

## The save ordering that matters

`PathsForm.save` (`web/src/components/Settings/PathsForm.tsx:114`) does four
things in this order:

1. **Compute `newlyGranted` before mutating local state** — ticked presets that
   were not already `allowed` (`:131-133`). The next step marks every ticked
   preset allowed, which would otherwise erase the distinction.
2. **`PUT /api/config/ocode/paths`** (`:134`). The config write and
   `applyExtraAllowedPathsToRuntime` (`internal/server/handler_config.go:1963`)
   complete here.
3. **Mark the ticked presets allowed locally** (`:136`).
4. **`POST …/paths/request` for `newlyGranted` only** (`:139-141`). If the list
   is empty, no request is made.

**Invariant: persist before probing.** A denied consent dialog therefore never
undoes the grant, and a probe failure is not a save failure. `requestAccess`
(`PathsForm.tsx:96-112`) stores its error in a separate `accessError` slot,
rendered as "Access request failed: …" (`:273-275`); the save's own error state is
untouched.

**Invariant: only newly granted presets are auto-probed.** Already-granted
folders are not re-prompted on every save — that would re-open the consent dialog
for a grant the OS already recorded. The per-row **Check access / Retry access**
button calls `requestAccess([preset.app])` to re-probe exactly one folder
without saving (`PathsForm.tsx:250-263`).

## The probe

`probeOfficeAddinAccess(ctx, preset, osGrants, request)`
(`internal/server/handler_office_addin_access.go:49`) is cross-platform and takes
both platform facts as parameters so each branch is testable on any host OS
without firing a real prompt. `requestOfficeAddinAccess` (`:29`) supplies
`sysperm.PlatformSupported()` and `h.syspermRequest` unless a test override is
installed.

1. **`!preset.Exists` → `missing`, and never probes** (`:56-62`). Office creates
   the `wef` folder on first sideload, and macOS raises no dialog for an absent
   path, so probing would only produce a misleading result.
2. **macOS (`osGrants == true`)** builds a `sysperm.Entry` with
   `Kind: sysperm.KindPath`, `Supported: true`, `Requested: true` and calls
   `sysperm.Request` (`:63-77`; `Request` at `internal/sysperm/sysperm.go:243`).
   **The folder read inside that request is what raises the TCC dialog.** The
   result's status/message/`OpenedSettings` are forwarded to the row.
3. **Windows/Linux (`osGrants == false`)** has no per-application consent gate,
   so the check is a write probe (`:82-93`): it calls
   `os.CreateTemp(preset.Path, ".ocode-access-*")` (`:82`), closes and removes
   the file, and reports `not_required` ("the folder is writable") on success or
   `denied` ("Cannot write to this folder: …") on an ACL/permission error.
   Nothing is opened in System Settings.

Result shape (`officeAddinAccessResult`, `:19-25`):
`{app, path, status, message, opened_settings}`.

Declared statuses and the web labels (`web/src/api/client.ts:139-146`,
`ACCESS_LABEL` in `PathsForm.tsx:22-29`):

| Status | Meaning | Label |
|---|---|---|
| `granted` | OS reports the grant present | Access granted |
| `denied` | OS refused, or the access check failed with a permission error | Access denied |
| `not_required` | No per-app gate on this platform; folder writable | Folder writable |
| `missing` | Folder does not exist yet | Folder not created yet |
| `unknown` | Platform exposed no verdict | Access not verifiable |

The macOS branch forwards `sysperm.Status` verbatim (`:73`), so a
`not_determined` can pass through in principle; the web falls back to a generic
"Access status unknown" label for any status outside the five above.

## macOS: the container gate and the Full Disk Access remedy

Two predicates in `internal/sysperm/catalog_darwin.go` keep a settings-page open
from raising a dialog:

- `isProtectedPath` (`:102`) covers Desktop, Documents, Downloads,
  `~/Library/Mobile Documents`, **`~/Library/Containers`**,
  **`~/Library/Group Containers`** and `/Volumes` (`:104-112`).
- `isAppDataPath` (`:126`) narrows that to another application's container:
  `~/Library/Containers` and `~/Library/Group Containers` (`:131-134`), which is
  where every Office preset lives. macOS protects these with the **"access data
  from other apps"** TCC service.

`detectEntry` refuses to probe a protected `KindPath` until `Requested` is
recorded (`:87-96`): a path under a protected root with `!e.Requested` returns
`StatusNotDetermined` without touching it (`:92-93`). The persisted intent is
`Entry.Requested` (`internal/sysperm/sysperm.go:78-81`). This is what makes the
System Permissions list and `GET /api/config/ocode/paths` side-effect free — and
it is why the Office handler must explicitly mark its synthetic entry
`Requested: true` before asking.

After the read, `requestEntry`'s default path branch (`:177-198`) re-detects and
chooses the remedy:

- A denial under `isAppDataPath` opens the **Full Disk Access** pane and returns a
  FDA-specific message (`:186-190`). **No Files & Folders toggle covers another
  app's container**, so FDA is the only fix after a denial.
- Any other path opens Files & Folders (`:192-193`).

TCC grants are tied to the **code signature**. A rebuilt/re-signed `ocode`
binary is a different client to TCC, so the grant is forgotten and the dialog
returns on the next save. Windows/Linux have no equivalent: there is nothing to
grant and the probe is a writability check.

## Web rendering

- `load` (`PathsForm.tsx:53-87`) only reads config; it **clears** the access
  report map (`:73`) so a stale "Access granted" never outlives a page load. No
  probe happens on GET.
- Office preset paths are filtered out of the free-text field using both `path`
  and `allowed_path` (`:63-68`), so a `~/...`-spelled grant is not shown twice.
- A checked row whose last report is not `granted` — or which has no report yet —
  shows the retry/check button (`:250-264`); the click calls
  `preventDefault`/`stopPropagation` because the row is a `<label>` that would
  otherwise toggle the checkbox.
- `requesting` renders "Requesting access…" (`:233-235`); the server message is
  rendered under the status label (`:236-249`).

## Invariants

- **The config PUT persists before the probe.** A denied dialog never undoes a
  grant; a probe error is reported separately from the save error.
- **Only newly granted presets are auto-probed on save.** Already-granted
  folders are not re-prompted; the Check/Retry button re-probes one folder
  without saving.
- **No probe on GET/load.** `detectEntry`'s `isProtectedPath`/`Requested` gate is
  what prevents merely opening Settings (or the System Permissions list) from
  raising a TCC dialog.
- **On macOS a denial to another app's container has no Files & Folders
  remedy** — Full Disk Access is the only fix. TCC grants are signature-bound and
  forgotten on rebuild.
- **Status is a report, not a persisted permission.** The probe writes no
  sysperm state; the only persisted change is `extra_allowed_paths`. There is
  still no Office config key.
- **A preset with `Exists: false` never probes** and reports `missing`.

## Verification

- `internal/server/handler_office_addin_access_test.go`:
  `TestHandleRequestOfficeAddinAccessProbesNamedApps` (`:31`),
  `…DedupesApps` (`:66`), `…RejectsBadInput` (`:89`),
  `TestProbeOfficeAddinAccessSkipsMissingFolder` (`:120`),
  `…RequestsOSGrant` (`:137`),
  `…ChecksFilesystemWithoutOSGrants` (`:155`).
- `internal/sysperm/catalog_darwin_test.go`:
  `TestAppDataPathNotProbedUntilRequested` (`:99`), `TestIsAppDataPath`
  (`:115`), and the extended `TestIsProtectedPath` (`:73`).
- `web/src/components/Settings/PathsForm.officeAddins.test.tsx` (12 cases:
  preset rendering, grant-with-other-paths, save→request, result rendering,
  denied-without-save-failure, retry-without-saving, request error, no request
  when nothing is new, revoke, uninstalled app, disappeared folder, no-preset
  platform).

## See also

- [File editor grants](file-editor-grants.md) — the *other* out-of-project
  allowlist. Editor grants are deliberately **not** `extra_allowed_paths`;
  Office presets are the opposite case, a UI that writes that very list.

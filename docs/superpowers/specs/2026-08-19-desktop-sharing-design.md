# Desktop Session and Desktop Sharing Design

**Date:** 2026-08-19
**Status:** Proposed

## Goal

Add two native application-menu actions to `ocode-desktop`:

- **Share Current Session** — expose the active web session route.
- **Share Desktop** — expose the complete desktop web application, including all session tabs and desktop panels.

Sharing follows `/rc`: detect a running Tailscale installation, try Funnel first, fall back to tailnet-only Serve, preserve per-route path isolation, include the existing server auth token, copy/show the resulting URL, and clean up only ocode-owned routes.

This is full live-control sharing, not a read-only export. The shared URL therefore carries the same auth capability as the local desktop URL.

## Architecture

### Shared Tailscale package

Create `internal/tailscale`, with no TUI, Wails, or server imports. Move the existing `/rc` implementation there:

- availability/running probe (`LookPath` + `tailscale status`)
- Funnel-first / Serve-fallback exposure
- output capture and URL parsing
- Tailscale DNS-name lookup
- safe `--set-path` sanitization
- URL path-prefix replacement
- best-effort removal of one owned path (`funnel ... off` and `serve ... off`)

The package exposes an owned exposure value containing the URL, mode (`funnel` or `serve`), path prefix, and process handle. The caller owns the exposure lifecycle; cleanup is idempotent and never performs a global `tailscale ... reset`.

Command execution is injectable through package-level command/lookup seams or an equivalent runner interface so tests never invoke a real Tailscale binary.

The TUI `/rc` code becomes a thin adapter over this package. Existing URL/path behavior and setup hints remain unchanged.

### Desktop share manager

Add a small share manager at the desktop boundary. It owns at most two independent exposures:

- desktop exposure, mounted at a process-unique path such as `/ocode-desktop-<pid>`
- session exposure, mounted at a path derived from the session ID and a desktop-instance prefix

Repeated requests for the same share reuse the existing exposure rather than spawning duplicate Tailscale routes. A mutex serializes start/replace/cleanup operations. Closing the desktop app calls cleanup for both exposures before quitting. Cleanup is best-effort and does not affect other ocode instances or unrelated Tailscale routes.

The manager receives the actual listener port from `desktop.Handle`/`server.Addr`, not the saved port file.

### Server boundary

The server receives an optional share-manager interface from the desktop boot path. The server exposes authenticated endpoints only when the manager is configured:

- `POST /api/share/desktop` → starts/reuses the complete desktop exposure.
- `POST /api/share/session` with `{ "session_id": "..." }` → starts/reuses a route for that session.
- `DELETE /api/share/desktop` and `DELETE /api/share/session` → stops the corresponding exposure.

Responses contain the share URL, mode, and setup hint when Tailscale is installed but Funnel/Serve is not enabled. Missing/unconfigured Tailscale returns a clear `503` response. Session sharing validates that the requested session exists before exposing it.

The optional interface keeps normal `ocode serve` and non-desktop server instances unchanged while allowing the React event handler to request sharing through the authenticated loopback server.

### Native menu and web event flow

Add both items to the macOS application menu and the Windows/Linux File menu. They remain enabled; the web UI reports “No active session” when appropriate.

- Native **Share Current Session** dispatches `ocode:share-session` with `window.ExecJS`.
- React reads its current `activeTabId`, POSTs `/api/share/session`, copies the returned URL using the browser clipboard API, and displays a success/error toast.
- Native **Share Desktop** dispatches `ocode:share-desktop`.
- React POSTs `/api/share/desktop`, copies the returned URL, and displays a success/error toast.

This preserves the established plain-HTTP Wails event pattern used by `ocode:open-settings`; it does not depend on `window.EmitEvent` or the unavailable full Wails runtime.

The native menu also gets accelerators for the two actions, using distinct `CmdOrCtrl+Shift` combinations that do not conflict with existing shortcuts.

## Tailscale behavior and safety

- `tailscale status` is the availability check; a missing binary or stopped daemon is reported as unavailable without logging noisy errors to the terminal.
- Funnel is attempted first to preserve `/rc` behavior. The response includes the mode so the UI can distinguish public Funnel sharing from tailnet-only sharing.
- The generated URL contains the existing desktop auth token. UI copy feedback must make clear that the link grants live access.
- No global Tailscale reset is ever used. Cleanup removes only the exact owned `--set-path` mount.
- Share requests are serialized and idempotent. A failed second exposure must not destroy a healthy existing exposure.
- If Tailscale is stopped after a URL is created, the existing URL naturally stops working; a later share request re-probes and reports the current state.

## Testing

### Go unit tests

- Shared Tailscale path sanitization and URL-prefix replacement, including malformed/base URLs.
- Probe behavior for missing binary and stopped daemon.
- Funnel success, Funnel failure with Serve success, setup-hint failure, and DNS fallback parsing.
- Per-path cleanup invokes both modes without global reset.
- Share manager reuses an exposure, isolates desktop/session paths, serializes concurrent requests, and cleans up both exposures.
- Server share handlers validate request/session input, return mode/setup details, map unavailable Tailscale to `503`, and honor optional-manager absence.
- Existing TUI Tailscale path tests continue to pass against the extracted implementation.

### Frontend tests

- Native share-session event uses the current active session ID.
- Native share-desktop event requests the desktop endpoint.
- Successful responses copy the URL and surface success feedback.
- Missing active session, API failure, and clipboard failure surface error feedback without breaking the app.

### Validation

Run `gofmt`, focused Go tests, `go test ./...`, frontend tests, `tsgo --noEmit -p web/tsconfig.json`, `git diff --check`, and the desktop build.
